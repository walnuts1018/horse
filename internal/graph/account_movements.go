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
	"github.com/walnuts1018/horse/internal/application/accountmovement"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

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
	result, err := accountmovement.New(r.DB).RecordMovement(ctx, accountmovement.MovementCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, Kind: string(input.Kind), OccurredAt: occurredAt,
		FromAccountID: input.FromAccountID, ToAccountID: input.ToAccountID, AmountYen: int64(input.AmountYen),
		Description: description, IdempotencyKey: input.IdempotencyKey, RequestFingerprint: fingerprint,
		SourceDocumentID: optionalString(input.SourceDocumentID), SourceReference: optionalString(input.SourceReference), SourceAccountID: optionalString(input.SourceAccountID),
	})
	if err != nil {
		return nil, err
	}
	return &model.AccountMovement{
		ID: result.ID, HouseholdID: result.HouseholdID, Kind: model.AccountMovementKind(result.Kind),
		OccurredAt: model.DateTime(result.OccurredAt), FromAccountID: result.FromAccountID, ToAccountID: result.ToAccountID,
		AmountYen: graphLong(result.AmountYen), Currency: currencyJPY, Description: result.Description, CreatedAt: model.DateTime(result.CreatedAt),
	}, nil
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
	sourceDocumentID, sourceReference := "", ""
	if input.SourceDocumentID != nil {
		sourceDocumentID = *input.SourceDocumentID
	}
	if input.SourceReference != nil {
		sourceReference = *input.SourceReference
	}
	if (sourceDocumentID == "") != (sourceReference == "") {
		return nil, fmt.Errorf("%w: statement source is incomplete", ErrBadInput)
	}
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	result, err := accountmovement.New(r.DB).RecordIncome(ctx, accountmovement.IncomeCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, AccountID: input.AccountID, OccurredAt: occurredAt,
		AmountYen: int64(input.AmountYen), Description: description, IdempotencyKey: input.IdempotencyKey,
		RequestFingerprint: fingerprint, SourceDocumentID: sourceDocumentID, SourceReference: sourceReference,
	})
	if err != nil {
		return nil, err
	}
	return &model.AccountIncome{
		ID: result.ID, HouseholdID: result.HouseholdID, AccountID: result.AccountID,
		AmountYen: graphLong(result.AmountYen), Currency: currencyJPY, OccurredAt: model.DateTime(result.OccurredAt),
		Description: result.Description, CreatedAt: model.DateTime(result.CreatedAt),
	}, nil
}

func accountIncomeModel(row *models.AccountIncome) *model.AccountIncome {
	return &model.AccountIncome{
		ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), AccountID: row.AccountID.String(),
		AmountYen: graphLong(row.AmountYen), Currency: currencyJPY, OccurredAt: timeString(row.OccurredAt),
		Description: row.Description, CreatedAt: timeString(row.CreatedAt),
	}
}

func accountMovementModel(row *models.AccountMovement) *model.AccountMovement {
	return &model.AccountMovement{
		ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Kind: model.AccountMovementKind(row.Kind),
		OccurredAt: timeString(row.OccurredAt), FromAccountID: row.FromAccountID.String(), ToAccountID: row.ToAccountID.String(),
		AmountYen: graphLong(row.AmountYen), Currency: currencyJPY, Description: row.Description, CreatedAt: timeString(row.CreatedAt),
	}
}

func loadAccountMovementAccount(ctx context.Context, executor bob.Executor, householdID, accountID string) (*model.Account, error) {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	accountUUID, err := gofrsuuid.FromString(accountID)
	if err != nil {
		return nil, ErrNotFound
	}
	account, err := models.Accounts.Query(sm.Where(psql.And(
		models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Accounts.Columns.ID.EQ(psql.Arg(accountUUID)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return graphAccount(account), nil
}

func loadAccountMovementAccounts(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, movements []*models.AccountMovement) (map[gofrsuuid.UUID]*model.Account, error) {
	ids := make([]any, 0, len(movements)*2)
	seen := make(map[gofrsuuid.UUID]struct{}, len(movements)*2)
	for _, movement := range movements {
		for _, id := range []gofrsuuid.UUID{movement.FromAccountID, movement.ToAccountID} {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	accountsByID := make(map[gofrsuuid.UUID]*model.Account, len(ids))
	if len(ids) == 0 {
		return accountsByID, nil
	}
	accounts, err := models.Accounts.Query(sm.Where(psql.And(
		models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Accounts.Columns.ID.In(psql.Arg(ids...)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		accountsByID[account.ID] = graphAccount(account)
	}
	return accountsByID, nil
}

func (r *queryResolver) accountMovements(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.AccountMovementConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	limit, backward, err := pageDirection(first, last)
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
	if id, err := cursorID(after, nodeTypeAccountMovement); err != nil {
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
	if id, err := cursorID(before, nodeTypeAccountMovement); err != nil {
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
	accountsByID, err := loadAccountMovementAccounts(ctx, executor, hhID, rows)
	if err != nil {
		return nil, err
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
		movement := accountMovementModel(row)
		movement.FromAccount = accountsByID[row.FromAccountID]
		movement.ToAccount = accountsByID[row.ToAccountID]
		connection.Edges = append(connection.Edges, &model.AccountMovementEdge{Cursor: encodeCursor(nodeTypeAccountMovement, row.ID.String()), Node: movement})
	}
	if len(rows) > 0 {
		start := encodeCursor(nodeTypeAccountMovement, rows[0].ID.String())
		end := encodeCursor(nodeTypeAccountMovement, rows[len(rows)-1].ID.String())
		connection.PageInfo.StartCursor = &start
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
}

func (r *queryResolver) accountIncomes(ctx context.Context, householdID string, after, before *string, first, last *int) (*model.AccountIncomeConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	limit, backward, err := pageDirection(first, last)
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
	if id, err := cursorID(after, nodeTypeAccountIncome); err != nil {
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
	if id, err := cursorID(before, nodeTypeAccountIncome); err != nil {
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
		connection.Edges = append(connection.Edges, &model.AccountIncomeEdge{Cursor: encodeCursor(nodeTypeAccountIncome, row.ID.String()), Node: accountIncomeModel(row)})
	}
	if len(rows) > 0 {
		start := encodeCursor(nodeTypeAccountIncome, rows[0].ID.String())
		end := encodeCursor(nodeTypeAccountIncome, rows[len(rows)-1].ID.String())
		connection.PageInfo.StartCursor = &start
		connection.PageInfo.EndCursor = &end
	}
	return connection, nil
}
