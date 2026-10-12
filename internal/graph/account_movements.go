package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
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
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{HouseholdID: &householdID, Key: &input.IdempotencyKey, Operation: &operation, ResourceID: &resourceID}).Exec(ctx, executor); err != nil {
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

func accountMovementModel(row *models.AccountMovement) *model.AccountMovement {
	return &model.AccountMovement{
		ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Kind: model.AccountMovementKind(row.Kind),
		OccurredAt: timeString(row.OccurredAt), FromAccountID: row.FromAccountID.String(), ToAccountID: row.ToAccountID.String(),
		AmountYen: graphLong(row.AmountYen), Currency: "JPY", Description: row.Description, CreatedAt: timeString(row.CreatedAt),
	}
}

func (r *queryResolver) accountMovements(ctx context.Context, householdID string, first *int) ([]*model.AccountMovement, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	limit := 50
	if first != nil {
		limit = *first
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("%w: first must be between one and one hundred", ErrBadInput)
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	rows, err := models.AccountMovements.Query(
		sm.Where(models.AccountMovements.Columns.HouseholdID.EQ(psql.Arg(hhID))),
		sm.OrderBy(models.AccountMovements.Columns.OccurredAt).Desc(),
		sm.OrderBy(models.AccountMovements.Columns.ID).Desc(),
		sm.Limit(limit),
	).All(ctx, bobpgx.NewPool(r.DB))
	if err != nil {
		return nil, err
	}
	result := make([]*model.AccountMovement, 0, len(rows))
	for _, row := range rows {
		result = append(result, accountMovementModel(row))
	}
	return result, nil
}
