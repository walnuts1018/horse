package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *mutationResolver) recordAccountMovement(ctx context.Context, input model.RecordAccountMovementInput) (*model.AccountMovement, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	if !input.Kind.IsValid() || input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney || strings.TrimSpace(input.IdempotencyKey) == "" || len(input.IdempotencyKey) > 200 {
		return nil, fmt.Errorf("%w: invalid account movement", ErrBadInput)
	}
	if input.FromAccountID == input.ToAccountID {
		return nil, fmt.Errorf("%w: source and destination accounts must differ", ErrBadInput)
	}
	description := strings.TrimSpace(input.Description)
	if len(description) > 240 {
		return nil, fmt.Errorf("%w: description is too long", ErrBadInput)
	}
	if description == "" {
		description = string(input.Kind)
	}
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	householdID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, ErrNotFound
	}
	fromID, err := gofrsuuid.FromString(input.FromAccountID)
	if err != nil {
		return nil, ErrNotFound
	}
	toID, err := gofrsuuid.FromString(input.ToAccountID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	operation := "recordAccountMovement"
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(input.IdempotencyKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, executor)
	if err == nil && existing.ResourceID.Valid {
		if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
			return nil, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		row, err := models.AccountMovements.Query(sm.Where(models.AccountMovements.Columns.ID.EQ(psql.Arg(existing.ResourceID.V)))).One(ctx, bobpgx.NewPool(r.DB))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		return accountMovementModel(row), nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err := lockAccountingPeriods(ctx, tx, input.HouseholdID, accountingPeriod(occurredAt)); err != nil {
		return nil, err
	}
	fromAccount, err := models.Accounts.Query(sm.Where(psql.And(models.Accounts.Columns.ID.EQ(psql.Arg(fromID)), models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID))))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	toAccount, err := models.Accounts.Query(sm.Where(psql.And(models.Accounts.Columns.ID.EQ(psql.Arg(toID)), models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID))))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	switch input.Kind {
	case model.AccountMovementKindTransfer:
		if fromAccount.Kind == string(model.AccountKindCard) || toAccount.Kind == string(model.AccountKindCard) {
			return nil, fmt.Errorf("%w: card accounts require a bill payment movement", ErrBadInput)
		}
	case model.AccountMovementKindCardBillPayment:
		if fromAccount.Kind != string(model.AccountKindBank) || toAccount.Kind != string(model.AccountKindCard) {
			return nil, fmt.Errorf("%w: card bill payments must move from a bank account to a card account", ErrBadInput)
		}
	}
	amount := int64(input.AmountYen)
	movementID, journalID := generatedUUID(newID()), generatedUUID(newID())
	memberID := generatedUUID(user.MemberID)
	kind, sourceKind := string(input.Kind), "ACCOUNT_MOVEMENT"
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalID, HouseholdID: &householdID, SourceKind: &sourceKind, SourceID: &movementID,
		IdempotencyKey: &input.IdempotencyKey, OccurredAt: &occurredAt, CreatedBy: &memberID,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	zero := int64(0)
	noPool, noMember := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
	destination, source := sql.Null[gofrsuuid.UUID]{V: toID, Valid: true}, sql.Null[gofrsuuid.UUID]{V: fromID, Valid: true}
	debitMemo, creditMemo := "Account movement destination", "Account movement source"
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{JournalID: &journalID, LineNo: new(int32(1)), AccountID: &destination, PoolID: &noPool, MemberID: &noMember, DebitYen: &amount, CreditYen: &zero, Memo: &debitMemo}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{JournalID: &journalID, LineNo: new(int32(2)), AccountID: &source, PoolID: &noPool, MemberID: &noMember, DebitYen: &zero, CreditYen: &amount, Memo: &creditMemo}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	row, err := models.AccountMovements.Insert(&models.AccountMovementSetter{
		ID: &movementID, HouseholdID: &householdID, JournalID: &journalID, Kind: &kind, OccurredAt: &occurredAt,
		FromAccountID: &fromID, ToAccountID: &toID, AmountYen: &amount, Description: &description,
	}).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	resourceID := sql.Null[gofrsuuid.UUID]{V: movementID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdID, Key: &input.IdempotencyKey, Operation: &operation, ResourceID: &resourceID,
		RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "account.movement.recorded", movementID.String(), 1, []string{"kind", "fromAccountId", "toAccountId", "amountYen", "occurredAt"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return accountMovementModel(row), nil
}

func (r *mutationResolver) recordExternalIncome(ctx context.Context, input model.RecordExternalIncomeInput) (*model.AccountIncome, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	if input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney || strings.TrimSpace(input.IdempotencyKey) == "" || len(input.IdempotencyKey) > 200 {
		return nil, fmt.Errorf("%w: invalid external income", ErrBadInput)
	}
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	description := strings.TrimSpace(input.Description)
	if len(description) > 240 {
		return nil, fmt.Errorf("%w: description is too long", ErrBadInput)
	}
	if description == "" {
		description = "External income"
	}
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	householdID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, ErrNotFound
	}
	accountID, err := gofrsuuid.FromString(input.AccountID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	operation := "recordExternalIncome"
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(input.IdempotencyKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, executor)
	if err == nil && existing.ResourceID.Valid {
		if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
			return nil, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		row, err := models.AccountIncomes.Query(sm.Where(models.AccountIncomes.Columns.ID.EQ(psql.Arg(existing.ResourceID.V)))).One(ctx, bobpgx.NewPool(r.DB))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		return accountIncomeModel(row), nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	account, err := models.Accounts.Query(sm.Where(psql.And(
		models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Accounts.Columns.ID.EQ(psql.Arg(accountID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if account.Kind == string(model.AccountKindCard) {
		return nil, fmt.Errorf("%w: external income requires a bank, cash, or other account", ErrBadInput)
	}
	if err := lockAccountingPeriods(ctx, tx, input.HouseholdID, accountingPeriod(occurredAt)); err != nil {
		return nil, err
	}
	incomeID, journalID := generatedUUID(newID()), generatedUUID(newID())
	actorID := generatedUUID(user.MemberID)
	sourceKind := "ACCOUNT_INCOME"
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalID, HouseholdID: &householdID, SourceKind: &sourceKind, SourceID: &incomeID,
		IdempotencyKey: &input.IdempotencyKey, OccurredAt: &occurredAt, CreatedBy: &actorID,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	amount := int64(input.AmountYen)
	zero := int64(0)
	accountReference := sql.Null[gofrsuuid.UUID]{V: accountID, Valid: true}
	noAccount, noPool, noMember := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
	lineOne, lineTwo := int32(1), int32(2)
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalID, LineNo: &lineOne, AccountID: &accountReference, PoolID: &noPool, MemberID: &noMember,
		DebitYen: &amount, CreditYen: &zero, Memo: new("External income"),
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalID, LineNo: &lineTwo, AccountID: &noAccount, PoolID: &noPool, MemberID: &noMember,
		DebitYen: &zero, CreditYen: &amount, Memo: new("External income offset"),
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	row, err := models.AccountIncomes.Insert(&models.AccountIncomeSetter{
		ID: &incomeID, HouseholdID: &householdID, JournalID: &journalID, AccountID: &accountID,
		OccurredAt: &occurredAt, AmountYen: &amount, Description: &description,
	}).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	resourceID := sql.Null[gofrsuuid.UUID]{V: incomeID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdID, Key: &input.IdempotencyKey, Operation: &operation, ResourceID: &resourceID,
		RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "account.income.recorded", incomeID.String(), 1, []string{"amountYen", "accountId", "occurredAt"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return accountIncomeModel(row), nil
}

func accountIncomeModel(row *models.AccountIncome) *model.AccountIncome {
	return &model.AccountIncome{
		ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), AccountID: row.AccountID.String(),
		AmountYen: graphLong(row.AmountYen), Currency: "JPY", OccurredAt: timeString(row.OccurredAt),
		Description: row.Description, CreatedAt: timeString(row.CreatedAt),
	}
}

func accountMovementModel(row *models.AccountMovement) *model.AccountMovement {
	return &model.AccountMovement{
		ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Kind: model.AccountMovementKind(row.Kind),
		OccurredAt: timeString(row.OccurredAt), FromAccountID: row.FromAccountID.String(), ToAccountID: row.ToAccountID.String(),
		AmountYen: graphLong(row.AmountYen), Currency: "JPY", Description: row.Description, CreatedAt: timeString(row.CreatedAt),
	}
}

func (r *queryResolver) accountMovements(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.AccountMovementConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	limit, backward, err := pageDirection(after, before, first, last)
	if err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := bobpgx.NewPool(r.DB)
	baseFilter := models.AccountMovements.Columns.HouseholdID.EQ(psql.Arg(hhID))
	totalCount, err := models.AccountMovements.Query(sm.Where(baseFilter)).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	filters := []bob.Expression{baseFilter}
	if id, err := cursorID(after, "AccountMovement"); err != nil {
		return nil, err
	} else if id != nil {
		parsedID, err := gofrsuuid.FromString(*id)
		if err != nil {
			return nil, fmt.Errorf("parse account movement cursor: %w", err)
		}
		cursor, err := models.AccountMovements.Query(sm.Where(psql.And(models.AccountMovements.Columns.ID.EQ(psql.Arg(parsedID)), baseFilter))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, psql.Or(models.AccountMovements.Columns.OccurredAt.LT(psql.Arg(cursor.OccurredAt)), psql.And(models.AccountMovements.Columns.OccurredAt.EQ(psql.Arg(cursor.OccurredAt)), models.AccountMovements.Columns.ID.LT(psql.Arg(cursor.ID)))))
	}
	if id, err := cursorID(before, "AccountMovement"); err != nil {
		return nil, err
	} else if id != nil {
		parsedID, err := gofrsuuid.FromString(*id)
		if err != nil {
			return nil, fmt.Errorf("parse account movement cursor: %w", err)
		}
		cursor, err := models.AccountMovements.Query(sm.Where(psql.And(models.AccountMovements.Columns.ID.EQ(psql.Arg(parsedID)), baseFilter))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, psql.Or(models.AccountMovements.Columns.OccurredAt.GT(psql.Arg(cursor.OccurredAt)), psql.And(models.AccountMovements.Columns.OccurredAt.EQ(psql.Arg(cursor.OccurredAt)), models.AccountMovements.Columns.ID.GT(psql.Arg(cursor.ID)))))
	}
	occurredAtOrder := sm.OrderBy(models.AccountMovements.Columns.OccurredAt).Desc()
	idOrder := sm.OrderBy(models.AccountMovements.Columns.ID).Desc()
	if backward {
		occurredAtOrder = sm.OrderBy(models.AccountMovements.Columns.OccurredAt)
		idOrder = sm.OrderBy(models.AccountMovements.Columns.ID)
	}
	rows, err := models.AccountMovements.Query(
		sm.Where(psql.And(filters...)),
		occurredAtOrder,
		idOrder,
		sm.Limit(limit+1),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	hasExtra := len(rows) > limit
	if hasExtra {
		rows = rows[:limit]
	}
	if backward {
		slices.Reverse(rows)
	}
	pageInfo := &model.PageInfo{HasPreviousPage: backward && hasExtra, HasNextPage: !backward && hasExtra}
	if !backward && after != nil && *after != "" {
		pageInfo.HasPreviousPage = true
	}
	if backward && before != nil && *before != "" {
		pageInfo.HasNextPage = true
	}
	connection := &model.AccountMovementConnection{
		Edges:      make([]*model.AccountMovementEdge, 0, len(rows)),
		PageInfo:   pageInfo,
		TotalCount: int(totalCount),
	}
	for _, row := range rows {
		connection.Edges = append(connection.Edges, &model.AccountMovementEdge{Cursor: encodeCursor("AccountMovement", row.ID.String()), Node: accountMovementModel(row)})
	}
	if len(rows) > 0 {
		start := encodeCursor("AccountMovement", rows[0].ID.String())
		end := encodeCursor("AccountMovement", rows[len(rows)-1].ID.String())
		connection.PageInfo.StartCursor = &start
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
}

func (r *queryResolver) accountIncomes(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.AccountIncomeConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	limit, backward, err := pageDirection(after, before, first, last)
	if err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := bobpgx.NewPool(r.DB)
	baseFilter := models.AccountIncomes.Columns.HouseholdID.EQ(psql.Arg(hhID))
	totalCount, err := models.AccountIncomes.Query(sm.Where(baseFilter)).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	filters := []bob.Expression{baseFilter}
	if id, err := cursorID(after, "AccountIncome"); err != nil {
		return nil, err
	} else if id != nil {
		parsedID, err := gofrsuuid.FromString(*id)
		if err != nil {
			return nil, fmt.Errorf("parse account income cursor: %w", err)
		}
		cursor, err := models.AccountIncomes.Query(sm.Where(psql.And(models.AccountIncomes.Columns.ID.EQ(psql.Arg(parsedID)), baseFilter))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, psql.Or(models.AccountIncomes.Columns.OccurredAt.LT(psql.Arg(cursor.OccurredAt)), psql.And(models.AccountIncomes.Columns.OccurredAt.EQ(psql.Arg(cursor.OccurredAt)), models.AccountIncomes.Columns.ID.LT(psql.Arg(cursor.ID)))))
	}
	if id, err := cursorID(before, "AccountIncome"); err != nil {
		return nil, err
	} else if id != nil {
		parsedID, err := gofrsuuid.FromString(*id)
		if err != nil {
			return nil, fmt.Errorf("parse account income cursor: %w", err)
		}
		cursor, err := models.AccountIncomes.Query(sm.Where(psql.And(models.AccountIncomes.Columns.ID.EQ(psql.Arg(parsedID)), baseFilter))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		filters = append(filters, psql.Or(models.AccountIncomes.Columns.OccurredAt.GT(psql.Arg(cursor.OccurredAt)), psql.And(models.AccountIncomes.Columns.OccurredAt.EQ(psql.Arg(cursor.OccurredAt)), models.AccountIncomes.Columns.ID.GT(psql.Arg(cursor.ID)))))
	}
	occurredAtOrder := sm.OrderBy(models.AccountIncomes.Columns.OccurredAt).Desc()
	idOrder := sm.OrderBy(models.AccountIncomes.Columns.ID).Desc()
	if backward {
		occurredAtOrder = sm.OrderBy(models.AccountIncomes.Columns.OccurredAt)
		idOrder = sm.OrderBy(models.AccountIncomes.Columns.ID)
	}
	rows, err := models.AccountIncomes.Query(
		sm.Where(psql.And(filters...)),
		occurredAtOrder,
		idOrder,
		sm.Limit(limit+1),
	).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	hasExtra := len(rows) > limit
	if hasExtra {
		rows = rows[:limit]
	}
	if backward {
		slices.Reverse(rows)
	}
	pageInfo := &model.PageInfo{HasPreviousPage: backward && hasExtra, HasNextPage: !backward && hasExtra}
	if !backward && after != nil && *after != "" {
		pageInfo.HasPreviousPage = true
	}
	if backward && before != nil && *before != "" {
		pageInfo.HasNextPage = true
	}
	connection := &model.AccountIncomeConnection{
		Edges:      make([]*model.AccountIncomeEdge, 0, len(rows)),
		PageInfo:   pageInfo,
		TotalCount: int(totalCount),
	}
	for _, row := range rows {
		connection.Edges = append(connection.Edges, &model.AccountIncomeEdge{Cursor: encodeCursor("AccountIncome", row.ID.String()), Node: accountIncomeModel(row)})
	}
	if len(rows) > 0 {
		start := encodeCursor("AccountIncome", rows[0].ID.String())
		end := encodeCursor("AccountIncome", rows[len(rows)-1].ID.String())
		connection.PageInfo.StartCursor = &start
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
}
