package graph

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *mutationResolver) createAccount(ctx context.Context, input model.CreateAccountInput) (*model.Account, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 || !input.Kind.IsValid() {
		return nil, fmt.Errorf("%w: invalid account", ErrBadInput)
	}
	openingBalance := int64(0)
	var openingAt time.Time
	if input.OpeningBalance != nil {
		openingBalance = int64(input.OpeningBalance.AmountYen)
		if openingBalance < 1 || openingBalance > maxMoney {
			return nil, fmt.Errorf("%w: invalid opening balance", ErrBadInput)
		}
		openingAt, err = parseTime(input.OpeningBalance.At)
		if err != nil {
			return nil, err
		}
	}
	id := newID()
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	idUUID := generatedUUID(id)
	householdUUID, parseErr := gofrsuuid.FromString(input.HouseholdID)
	if parseErr != nil {
		return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	var poolValue sql.Null[gofrsuuid.UUID]
	if input.PoolID != nil {
		poolUUID, parseErr := gofrsuuid.FromString(*input.PoolID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid pool id", ErrBadInput)
		}
		poolValue = sql.Null[gofrsuuid.UUID]{V: poolUUID, Valid: true}
	}
	accountKind := string(input.Kind)
	executor := bobpgx.NewTx(tx, nil)
	if openingBalance > 0 {
		if err := lockAccountingPeriods(ctx, tx, input.HouseholdID, accountingPeriod(openingAt)); err != nil {
			return nil, err
		}
	}
	if _, err := models.Accounts.Insert(&models.AccountSetter{ID: &idUUID, HouseholdID: &householdUUID, PoolID: &poolValue, Name: &name, Kind: &accountKind}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if openingBalance > 0 {
		journalID := generatedUUID(newID())
		sourceKind, idempotencyKey := sourceKindAccountOpeningBalance, "account-opening:"+idUUID.String()
		createdBy := generatedUUID(user.MemberID)
		if _, err := models.Journals.Insert(&models.JournalSetter{
			ID: &journalID, HouseholdID: &householdUUID, SourceKind: &sourceKind, SourceID: &idUUID,
			IdempotencyKey: &idempotencyKey, OccurredAt: &openingAt, CreatedBy: &createdBy,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
		accountReference := sql.Null[gofrsuuid.UUID]{V: idUUID, Valid: true}
		noPool, noMember := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
		zero := int64(0)
		accountDebit, accountCredit := openingBalance, zero
		contraDebit, contraCredit := zero, openingBalance
		if input.Kind == model.AccountKindCard {
			accountDebit, accountCredit = zero, openingBalance
			contraDebit, contraCredit = openingBalance, zero
		}
		lineOne, lineTwo := int32(1), int32(2)
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journalID, LineNo: &lineOne, AccountID: &accountReference, PoolID: &noPool, MemberID: &noMember,
			DebitYen: &accountDebit, CreditYen: &accountCredit, Memo: new("Opening balance"),
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journalID, LineNo: &lineTwo, AccountID: &noMember, PoolID: &noPool, MemberID: &noPool,
			DebitYen: &contraDebit, CreditYen: &contraCredit, Memo: new("Opening balance offset"),
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "account.created", id, 1, []string{fieldName, fieldKind, fieldPoolID}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &model.Account{ID: id, Name: name, Kind: input.Kind, PoolID: input.PoolID}, nil
}
