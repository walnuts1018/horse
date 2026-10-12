package graph

import (
	"context"
	"crypto/sha256"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const maxMoney int64 = 1_000_000_000_000

func (r *mutationResolver) createHousehold(ctx context.Context, input model.CreateHouseholdInput) (*model.Household, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 || len(input.MemberNames) > 12 {
		return nil, fmt.Errorf("%w: invalid household name or member count", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdID := newID()
	householdUUID := generatedUUID(householdID)
	userUUID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, err
	}
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdUUID, Name: &name}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	ownerRole := "OWNER"
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &userUUID, Role: &ownerRole}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	for _, memberName := range input.MemberNames {
		memberName = strings.TrimSpace(memberName)
		if memberName == "" || len(memberName) > 120 {
			return nil, fmt.Errorf("%w: member names must not be empty", ErrBadInput)
		}
		placeholderID := newID()
		placeholderSubject := "invite:" + placeholderID
		placeholderUUID := generatedUUID(placeholderID)
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &placeholderUUID, OidcSubject: &placeholderSubject, DisplayName: &memberName}).Exec(ctx, executor); err != nil {
			return nil, err
		}
		if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &placeholderUUID}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	if err := writeEvent(ctx, tx, householdID, "household.created", householdID, 1, []string{}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadHousehold(ctx, r.DB, householdID)
}

func (r *mutationResolver) createPool(ctx context.Context, input model.CreatePoolInput) (*model.Pool, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 || !input.Kind.IsValid() {
		return nil, fmt.Errorf("%w: invalid pool", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdUUID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	executor := bobpgx.NewTx(tx, nil)
	householdRow, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	version := int64(householdRow.DefaultAllocationVersion)
	poolID := newID()
	poolUUID := generatedUUID(poolID)
	poolKind := string(input.Kind)
	if _, err := models.Pools.Insert(&models.PoolSetter{ID: &poolUUID, HouseholdID: &householdUUID, Name: &name, Kind: &poolKind}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	initialFundingVersion := int32(1)
	createdByUUID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, err
	}
	if _, err := models.PoolFundingRules.Insert(&models.PoolFundingRuleSetter{HouseholdID: &householdUUID, PoolID: &poolUUID, Version: &initialFundingVersion, CreatedBy: &createdByUUID}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	newVersion := version + 1
	oldDefaults, err := models.AllocationDefaults.Query(sm.Where(psql.And(
		models.AllocationDefaults.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AllocationDefaults.Columns.Version.EQ(psql.Arg(version)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	newVersionValue := newVersion
	for _, oldDefault := range oldDefaults {
		poolIDValue, weightValue := oldDefault.PoolID, oldDefault.Weight
		if _, err := models.AllocationDefaults.Insert(&models.AllocationDefaultSetter{
			HouseholdID: &householdUUID, Version: &newVersionValue, PoolID: &poolIDValue, Weight: &weightValue,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	defaultWeight := int64(1)
	if _, err := models.AllocationDefaults.Insert(&models.AllocationDefaultSetter{
		HouseholdID: &householdUUID, Version: &newVersionValue, PoolID: &poolUUID, Weight: &defaultWeight,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	newUpdatedAt := time.Now().UTC()
	newRevision := householdRow.Revision + 1
	if _, err := models.Households.Update((&models.HouseholdSetter{
		DefaultAllocationVersion: &newVersionValue, Revision: &newRevision, UpdatedAt: &newUpdatedAt,
	}).UpdateMod(), um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "pool.created", poolID, newVersion, []string{"name", "kind"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	household, err := loadHousehold(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	for _, pool := range household.Pools {
		if pool.ID == poolID {
			return pool, nil
		}
	}
	return nil, ErrNotFound
}

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
		sourceKind, idempotencyKey := "ACCOUNT_OPENING_BALANCE", "account-opening:"+idUUID.String()
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
	if err := writeEvent(ctx, tx, input.HouseholdID, "account.created", id, 1, []string{"name", "kind", "poolId"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &model.Account{ID: id, Name: name, Kind: input.Kind, PoolID: input.PoolID}, nil
}

func (r *mutationResolver) createCategory(ctx context.Context, input model.CreateCategoryInput) (*model.Category, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 {
		return nil, fmt.Errorf("%w: invalid category", ErrBadInput)
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
	var parentValue sql.Null[gofrsuuid.UUID]
	if input.ParentID != nil {
		parentUUID, parseErr := gofrsuuid.FromString(*input.ParentID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid parent category id", ErrBadInput)
		}
		parentValue = sql.Null[gofrsuuid.UUID]{V: parentUUID, Valid: true}
	}
	if _, err := models.Categories.Insert(&models.CategorySetter{ID: &idUUID, HouseholdID: &householdUUID, ParentID: &parentValue, Name: &name}).Exec(ctx, bobpgx.NewTx(tx, nil)); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "category.created", id, 1, []string{"name", "parentId"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &model.Category{ID: id, Name: name, ParentID: input.ParentID}, nil
}

func (r *mutationResolver) setCategoryArchived(ctx context.Context, categoryID string, archived bool) (*model.Category, error) {
	categoryUUID, err := gofrsuuid.FromString(categoryID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid category id", ErrBadInput)
	}
	categoryRow, err := models.Categories.Query(sm.Where(models.Categories.Columns.ID.EQ(psql.Arg(categoryUUID)))).One(ctx, bobpgx.NewPool(r.DB))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	householdID := categoryRow.HouseholdID.String()
	user, err := requireMember(ctx, r.DB, householdID)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdUUID := categoryRow.HouseholdID
	categoryRow, err = models.Categories.Query(sm.Where(psql.And(
		models.Categories.Columns.ID.EQ(psql.Arg(categoryUUID)),
		models.Categories.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
	)), sm.ForUpdate()).One(ctx, bobpgx.NewTx(tx, nil))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	name := categoryRow.Name
	var parentID *string
	if categoryRow.ParentID.Valid {
		parent := categoryRow.ParentID.V.String()
		parentID = &parent
	}
	if categoryRow.Archived != archived {
		if _, err := models.Categories.Update((&models.CategorySetter{Archived: &archived}).UpdateMod(), um.Where(models.Categories.Columns.ID.EQ(psql.Arg(categoryUUID)))).Exec(ctx, bobpgx.NewTx(tx, nil)); err != nil {
			return nil, err
		}
		eventType := "category.unarchived"
		if archived {
			eventType = "category.archived"
		}
		if err := writeEvent(ctx, tx, householdID, eventType, categoryID, 1, []string{"archived"}, user.MemberID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &model.Category{ID: categoryID, Name: name, ParentID: parentID, Archived: archived}, nil
}

func (r *mutationResolver) setDefaultAllocation(ctx context.Context, input model.SetDefaultAllocationInput) (*model.Household, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	weights, err := allocationWeights(input.Allocations)
	if err != nil {
		return nil, err
	}
	if _, err := domain.Allocate(1, weights); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdUUID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	executor := bobpgx.NewTx(tx, nil)
	householdRow, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	version := int64(householdRow.DefaultAllocationVersion)
	for _, weight := range weights {
		poolUUID, parseErr := gofrsuuid.FromString(weight.PoolID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid pool id", ErrBadInput)
		}
		_, err := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.Pools.Columns.ID.EQ(psql.Arg(poolUUID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
	}
	newVersion := version + 1
	newVersionValue := newVersion
	for _, weight := range weights {
		poolUUID, _ := gofrsuuid.FromString(weight.PoolID)
		weightValue := weight.Value
		if _, err := models.AllocationDefaults.Insert(&models.AllocationDefaultSetter{
			HouseholdID: &householdUUID, Version: &newVersionValue, PoolID: &poolUUID, Weight: &weightValue,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	newRevision, updatedAt := householdRow.Revision+1, time.Now().UTC()
	if _, err := models.Households.Update((&models.HouseholdSetter{
		DefaultAllocationVersion: &newVersionValue, Revision: &newRevision, UpdatedAt: &updatedAt,
	}).UpdateMod(), um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "allocation.default.updated", input.HouseholdID, newVersion, []string{"defaultAllocation"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadHousehold(ctx, r.DB, input.HouseholdID)
}

func (r *mutationResolver) setAllocationRule(ctx context.Context, input model.SetAllocationRuleInput) (*model.AllocationRule, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	scopeCount := 0
	for _, selected := range []*string{input.ProductVariantID, input.ProductConceptID, input.CategoryID, input.Merchant} {
		if selected != nil {
			scopeCount++
		}
	}
	if scopeCount != 1 {
		return nil, fmt.Errorf("%w: select exactly one product, category, or merchant rule key", ErrBadInput)
	}
	weights, err := allocationWeights(input.Allocations)
	if err != nil {
		return nil, err
	}
	if _, err := domain.Allocate(1, weights); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	merchantKey := ""
	if input.Merchant != nil {
		merchantKey = normalizeMerchant(*input.Merchant)
		if merchantKey == "" || len(merchantKey) > 240 {
			return nil, fmt.Errorf("%w: invalid merchant rule key", ErrBadInput)
		}
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdUUID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	for _, weight := range weights {
		poolUUID, parseErr := gofrsuuid.FromString(weight.PoolID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid pool id", ErrBadInput)
		}
		_, err := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.Pools.Columns.ID.EQ(psql.Arg(poolUUID)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
	}
	for _, referenceID := range []*string{input.CategoryID, input.ProductConceptID, input.ProductVariantID} {
		if referenceID == nil {
			continue
		}
		if _, err := gofrsuuid.FromString(*referenceID); err != nil {
			return nil, fmt.Errorf("%w: invalid allocation rule reference id", ErrBadInput)
		}
	}
	if input.CategoryID != nil {
		id := generatedUUID(*input.CategoryID)
		if _, err := models.Categories.Query(sm.Where(psql.And(models.Categories.Columns.ID.EQ(psql.Arg(id)), models.Categories.Columns.HouseholdID.EQ(psql.Arg(householdUUID))))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
	}
	if input.ProductConceptID != nil {
		id := generatedUUID(*input.ProductConceptID)
		if _, err := models.ProductConcepts.Query(sm.Where(psql.And(models.ProductConcepts.Columns.ID.EQ(psql.Arg(id)), models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdUUID))))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
	}
	if input.ProductVariantID != nil {
		id := generatedUUID(*input.ProductVariantID)
		if _, err := models.ProductVariants.Query(sm.Where(psql.And(models.ProductVariants.Columns.ID.EQ(psql.Arg(id)), models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID))))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
	}
	var ruleRow *models.AllocationRule
	var ruleErr error
	if input.CategoryID != nil {
		id := generatedUUID(*input.CategoryID)
		ruleRow, ruleErr = models.AllocationRules.Query(sm.Where(psql.And(models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.AllocationRules.Columns.CategoryID.EQ(psql.Arg(id)))), sm.ForUpdate()).One(ctx, executor)
	} else if input.Merchant != nil {
		ruleRow, ruleErr = models.AllocationRules.Query(sm.Where(psql.And(models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.AllocationRules.Columns.MerchantKey.EQ(psql.Arg(merchantKey)))), sm.ForUpdate()).One(ctx, executor)
	} else if input.ProductConceptID != nil {
		id := generatedUUID(*input.ProductConceptID)
		ruleRow, ruleErr = models.AllocationRules.Query(sm.Where(psql.And(models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.AllocationRules.Columns.ProductConceptID.EQ(psql.Arg(id)))), sm.ForUpdate()).One(ctx, executor)
	} else {
		id := generatedUUID(*input.ProductVariantID)
		ruleRow, ruleErr = models.AllocationRules.Query(sm.Where(psql.And(models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.AllocationRules.Columns.ProductVariantID.EQ(psql.Arg(id)))), sm.ForUpdate()).One(ctx, executor)
	}
	ruleID, revision := "", int64(1)
	if errors.Is(ruleErr, sql.ErrNoRows) {
		ruleID = newID()
		ruleUUID := generatedUUID(ruleID)
		categoryValue, conceptValue, variantValue := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
		merchantValue := sql.Null[string]{}
		if input.CategoryID != nil {
			categoryValue = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*input.CategoryID), Valid: true}
		}
		if input.ProductConceptID != nil {
			conceptValue = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*input.ProductConceptID), Valid: true}
		}
		if input.ProductVariantID != nil {
			variantValue = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*input.ProductVariantID), Valid: true}
		}
		if input.Merchant != nil {
			merchantValue = sql.Null[string]{V: merchantKey, Valid: true}
		}
		if _, err := models.AllocationRules.Insert(&models.AllocationRuleSetter{
			ID: &ruleUUID, HouseholdID: &householdUUID, CategoryID: &categoryValue, MerchantKey: &merchantValue,
			ProductConceptID: &conceptValue, ProductVariantID: &variantValue, Revision: &revision,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	} else {
		if ruleErr != nil {
			return nil, ruleErr
		}
		ruleUUID := ruleRow.ID
		ruleID, revision = ruleUUID.String(), ruleRow.Revision+1
		updatedAt := time.Now().UTC()
		if _, err := models.AllocationRules.Update((&models.AllocationRuleSetter{Revision: &revision, UpdatedAt: &updatedAt}).UpdateMod(), um.Where(models.AllocationRules.Columns.ID.EQ(psql.Arg(ruleUUID)))).Exec(ctx, executor); err != nil {
			return nil, err
		}
		if _, err := models.AllocationRuleWeights.Delete(dm.Where(models.AllocationRuleWeights.Columns.RuleID.EQ(psql.Arg(ruleUUID)))).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	for _, weight := range weights {
		poolUUID, _ := gofrsuuid.FromString(weight.PoolID)
		ruleUUID := generatedUUID(ruleID)
		weightValue := weight.Value
		if _, err := models.AllocationRuleWeights.Insert(&models.AllocationRuleWeightSetter{HouseholdID: &householdUUID, RuleID: &ruleUUID, PoolID: &poolUUID, Weight: &weightValue}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	changedFields := []string{"allocations"}
	if input.ProductVariantID != nil {
		changedFields = append(changedFields, "productVariantId")
	} else if input.ProductConceptID != nil {
		changedFields = append(changedFields, "productConceptId")
	} else if input.CategoryID != nil {
		changedFields = append(changedFields, "categoryId")
	} else {
		changedFields = append(changedFields, "merchant")
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "allocation.rule.updated", ruleID, revision, changedFields, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadAllocationRule(ctx, r.DB, ruleID)
}

func normalizeMerchant(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func allocationRuleWeights(ctx context.Context, tx pgx.Tx, householdID string, categoryID, productVariantID *string, merchant string) ([]domain.Weight, *string, int64, error) {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, nil, 0, err
	}
	executor := bobpgx.NewTx(tx, nil)
	var candidates []*models.AllocationRule
	appendRule := func(expression psql.Expression) error {
		rule, err := models.AllocationRules.Query(sm.Where(psql.And(models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), expression))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		candidates = append(candidates, rule)
		return nil
	}
	var conceptID *gofrsuuid.UUID
	if productVariantID != nil {
		variantUUID, parseErr := gofrsuuid.FromString(*productVariantID)
		if parseErr != nil {
			return nil, nil, 0, parseErr
		}
		variant, variantErr := models.ProductVariants.Query(sm.Where(psql.And(
			models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
		))).One(ctx, executor)
		if variantErr == nil {
			concept := variant.ConceptID
			conceptID = &concept
		} else if variantErr != nil && !errors.Is(variantErr, sql.ErrNoRows) {
			return nil, nil, 0, variantErr
		}
		if err := appendRule(models.AllocationRules.Columns.ProductVariantID.EQ(psql.Arg(variantUUID))); err != nil {
			return nil, nil, 0, err
		}
	}
	if conceptID != nil {
		if err := appendRule(models.AllocationRules.Columns.ProductConceptID.EQ(psql.Arg(*conceptID))); err != nil {
			return nil, nil, 0, err
		}
	}
	if categoryID != nil {
		categoryUUID, parseErr := gofrsuuid.FromString(*categoryID)
		if parseErr != nil {
			return nil, nil, 0, parseErr
		}
		if err := appendRule(models.AllocationRules.Columns.CategoryID.EQ(psql.Arg(categoryUUID))); err != nil {
			return nil, nil, 0, err
		}
	}
	if merchant != "" {
		if err := appendRule(models.AllocationRules.Columns.MerchantKey.EQ(psql.Arg(merchant))); err != nil {
			return nil, nil, 0, err
		}
	}
	if len(candidates) == 0 {
		return nil, nil, 0, nil
	}
	rule := candidates[0]
	weightsRows, err := models.AllocationRuleWeights.Query(sm.Where(models.AllocationRuleWeights.Columns.RuleID.EQ(psql.Arg(rule.ID))), sm.OrderBy(models.AllocationRuleWeights.Columns.PoolID)).All(ctx, executor)
	if err != nil {
		return nil, nil, 0, err
	}
	weights := make([]domain.Weight, 0, len(weightsRows))
	for _, row := range weightsRows {
		weights = append(weights, domain.Weight{PoolID: row.PoolID.String(), Value: row.Weight})
	}
	if len(weights) == 0 {
		return nil, nil, 0, errors.New("allocation rule has no weights")
	}
	ruleID := rule.ID.String()
	return weights, &ruleID, rule.Revision, nil
}

func (r *mutationResolver) createPurchase(ctx context.Context, input model.CreatePurchaseInput) (*model.Purchase, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 || len(input.Items) == 0 || len(input.Items) > 200 {
		return nil, fmt.Errorf("%w: invalid purchase", ErrBadInput)
	}
	if input.SourceReference != nil && (input.SourceDocumentID == nil || len(*input.SourceReference) > 256 || strings.ContainsAny(*input.SourceReference, "\r\n")) {
		return nil, fmt.Errorf("%w: source reference requires a document and must be valid", ErrBadInput)
	}
	origin := model.PurchaseOriginManual
	if input.SourceDocumentID != nil {
		origin = model.PurchaseOriginAi
	}
	if input.Origin != nil {
		origin = *input.Origin
	}
	if origin != model.PurchaseOriginAi && origin != model.PurchaseOriginManual && origin != model.PurchaseOriginUnknown {
		return nil, fmt.Errorf("%w: invalid purchase origin", ErrBadInput)
	}
	status := model.PurchaseStatusDraft
	if input.Status != nil {
		if *input.Status != model.PurchaseStatusDraft {
			return nil, fmt.Errorf("%w: purchases must be posted with postPurchase", ErrBadInput)
		}
		status = *input.Status
	}
	merchant := strings.TrimSpace(input.Merchant)
	if len(merchant) > 240 {
		return nil, fmt.Errorf("%w: merchant name is too long", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdUUID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	executor := bobpgx.NewTx(tx, nil)
	key, operation := input.IdempotencyKey, "createPurchase"
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(key)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, executor)
	if err == nil {
		if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
			return nil, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return loadPurchase(ctx, r.DB, existing.ResourceID.V.String())
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	period := accountingPeriod(occurredAt)
	if err := ensureAccountingPeriod(ctx, executor, householdUUID, period); err != nil {
		return nil, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if periodRow.Status == "CLOSED" {
		return nil, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}
	if input.SourceDocumentID != nil {
		sourceReference := ""
		if input.SourceReference != nil {
			sourceReference = *input.SourceReference
		}
		if len(sourceReference) > 256 {
			return nil, fmt.Errorf("%w: source reference is too long", ErrBadInput)
		}
		documentUUID, parseErr := gofrsuuid.FromString(*input.SourceDocumentID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid source document id", ErrBadInput)
		}
		document, err := models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(documentUUID))), sm.ForUpdate()).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if document.HouseholdID != householdUUID {
			return nil, ErrNotFound
		}
		confirmation, confirmationErr := models.DocumentConfirmations.Query(sm.Where(psql.And(
			models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)),
			models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(sourceReference)),
		))).One(ctx, executor)
		if confirmationErr == nil {
			if err := tx.Rollback(ctx); err != nil {
				return nil, err
			}
			return loadPurchase(ctx, r.DB, confirmation.PurchaseID.String())
		}
		if !errors.Is(confirmationErr, sql.ErrNoRows) {
			return nil, confirmationErr
		}
	}
	householdRow, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForShare()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	ruleVersion := householdRow.DefaultAllocationVersion
	defaults, err := loadWeights(ctx, tx, input.HouseholdID, ruleVersion)
	if err != nil {
		return nil, err
	}
	if len(defaults) == 0 {
		return nil, fmt.Errorf("%w: configure a default allocation before adding purchases", ErrBadInput)
	}
	merchantRuleKey := normalizeMerchant(merchant)
	purchaseID := newID()
	purchaseUUID := generatedUUID(purchaseID)
	createdBy, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, err
	}
	purchaseStatus := string(status)
	purchaseOrigin := string(origin)
	if _, err := models.Purchases.Insert(&models.PurchaseSetter{
		ID: &purchaseUUID, HouseholdID: &householdUUID, OccurredAt: &occurredAt, Merchant: &merchant,
		Status: &purchaseStatus, AllocationRuleVersion: &ruleVersion, CreatedBy: &createdBy, Origin: &purchaseOrigin,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	purchaseItemUUIDs := make([]gofrsuuid.UUID, 0, len(input.Items))
	for _, itemInput := range input.Items {
		if itemInput == nil || strings.TrimSpace(itemInput.Name) == "" || len(itemInput.Name) > 240 || itemInput.AmountYen < 0 || int64(itemInput.AmountYen) > maxMoney {
			return nil, fmt.Errorf("%w: invalid purchase item", ErrBadInput)
		}
		measure, err := measureFromInput(itemInput)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
		}
		categoryID := itemInput.CategoryID
		if itemInput.ProductVariantID != nil {
			variantUUID, parseErr := gofrsuuid.FromString(*itemInput.ProductVariantID)
			if parseErr != nil {
				return nil, fmt.Errorf("%w: invalid product variant id", ErrBadInput)
			}
			variant, err := models.ProductVariants.Query(sm.Where(psql.And(
				models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
				models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			))).One(ctx, executor)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			if err != nil {
				return nil, err
			}
			if variant.Archived {
				return nil, ErrNotFound
			}
			concept, err := models.ProductConcepts.Query(sm.Where(psql.And(
				models.ProductConcepts.Columns.ID.EQ(psql.Arg(variant.ConceptID)),
				models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			))).One(ctx, executor)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			if err != nil {
				return nil, err
			}
			if concept.Archived {
				return nil, ErrNotFound
			}
			var productCategoryID *string
			if concept.CategoryID.Valid {
				id := concept.CategoryID.V.String()
				productCategoryID = &id
			}
			if categoryID == nil {
				categoryID = productCategoryID
			}
		}
		weights := defaults
		var appliedRuleID *string
		appliedRuleVersion := ruleVersion
		allocationOverride := false
		if len(itemInput.Allocations) > 0 {
			weights, err = allocationWeights(itemInput.Allocations)
			if err != nil {
				return nil, err
			}
			allocationOverride = true
		} else {
			ruleWeights, ruleID, revision, ruleErr := allocationRuleWeights(ctx, tx, input.HouseholdID, categoryID, itemInput.ProductVariantID, merchantRuleKey)
			if ruleErr != nil {
				return nil, ruleErr
			}
			if ruleID != nil {
				weights, appliedRuleID, appliedRuleVersion = ruleWeights, ruleID, revision
			}
		}
		allocated, err := domain.Allocate(int64(itemInput.AmountYen), weights)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
		}
		itemID := newID()
		itemUUID := generatedUUID(itemID)
		itemName, itemAmount := strings.TrimSpace(itemInput.Name), int64(itemInput.AmountYen)
		var categoryValue, variantValue sql.Null[gofrsuuid.UUID]
		if categoryID != nil {
			categoryValue = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*categoryID), Valid: true}
		}
		if itemInput.ProductVariantID != nil {
			variantValue = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*itemInput.ProductVariantID), Valid: true}
		}
		if _, err := models.PurchaseItems.Insert(&models.PurchaseItemSetter{
			ID: &itemUUID, HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, Name: &itemName,
			AmountYen: &itemAmount, CategoryID: &categoryValue, ProductVariantID: &variantValue, AllocationOverride: &allocationOverride,
			QuantityNumerator: &measure.quantityNumerator, QuantityDenominator: &measure.quantityDenominator, QuantityUnit: &measure.quantityUnit,
			PrintedUnitPriceYen: &measure.printedUnitPriceYen, PrintedUnitPriceQuantityNumerator: &measure.printedUnitPriceQuantityNumerator,
			PrintedUnitPriceQuantityDenominator: &measure.printedUnitPriceQuantityDenominator, PrintedUnitPriceUnit: &measure.printedUnitPriceUnit,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
		purchaseItemUUIDs = append(purchaseItemUUIDs, itemUUID)
		for _, allocation := range allocated {
			poolUUID := generatedUUID(allocation.PoolID)
			var ruleUUID sql.Null[gofrsuuid.UUID]
			if appliedRuleID != nil {
				ruleUUID = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*appliedRuleID), Valid: true}
			}
			if _, err := models.ItemAllocations.Insert(&models.ItemAllocationSetter{
				HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, ItemID: &itemUUID, PoolID: &poolUUID,
				Weight: &allocation.Weight, AmountYen: &allocation.Amount, RuleVersion: &appliedRuleVersion, RuleID: &ruleUUID,
			}).Exec(ctx, executor); err != nil {
				return nil, err
			}
		}
	}
	if len(input.PaymentAllocations) > 0 {
		payments, err := validatePurchasePaymentInputs(ctx, tx, input.HouseholdID, input.PaymentAllocations)
		if err != nil {
			return nil, err
		}
		if err := replacePurchasePaymentRows(ctx, tx, input.HouseholdID, purchaseID, payments); err != nil {
			return nil, err
		}
	}
	purchaseResource := sql.Null[gofrsuuid.UUID]{V: purchaseUUID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdUUID, Key: &key, Operation: &operation, ResourceID: &purchaseResource,
		RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if input.SourceDocumentID != nil {
		sourceReference := ""
		if input.SourceReference != nil {
			sourceReference = *input.SourceReference
		}
		documentUUID := generatedUUID(*input.SourceDocumentID)
		if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{
			SourceDocumentID: &documentUUID, SourceReference: &sourceReference, HouseholdID: &householdUUID,
			PurchaseID: &purchaseUUID, CreatedBy: &createdBy,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
		if _, err := psql.Insert(
			im.Into(models.EvidenceLinks.NameExpr(), "household_id", "source_document_id", "purchase_id"),
			im.Values(psql.Arg(householdUUID, documentUUID, purchaseUUID)),
			im.OnConflict().DoNothing(),
		).Exec(ctx, executor); err != nil {
			return nil, err
		}
		if sourceReference == "" {
			amounts := make([]int64, len(input.Items))
			for index, item := range input.Items {
				amounts[index] = int64(item.AmountYen)
			}
			candidateID := ""
			if input.ReceiptCandidateID != nil {
				candidateID = *input.ReceiptCandidateID
			}
			overrideReason := ""
			if input.ManualAmountOverrideReason != nil {
				overrideReason = *input.ManualAmountOverrideReason
			}
			if err := linkImportedReceiptItems(ctx, executor, householdUUID, documentUUID, purchaseUUID, purchaseItemUUIDs, amounts, candidateID, overrideReason); err != nil {
				return nil, err
			}
		}
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "purchase.created", purchaseID, 1, []string{}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, purchaseID)
}

func linkImportedReceiptItems(ctx context.Context, executor bob.Executor, householdID, documentID, purchaseID gofrsuuid.UUID, itemIDs []gofrsuuid.UUID, amounts []int64, candidateID, overrideReason string) error {
	job, err := models.ExtractionJobs.Query(
		sm.Where(psql.And(
			models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
			models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")),
		)),
		sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(),
		sm.Limit(1),
	).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !job.Result.Valid {
		return nil
	}
	var verified ingestion.Verification
	if err := json.Unmarshal(job.Result.V.Val, &verified); err != nil {
		return err
	}
	if len(verified.Receipt.Items) == 0 {
		if candidateID != "" || strings.TrimSpace(overrideReason) != "" {
			return fmt.Errorf("%w: receipt verification is unavailable", ErrBadInput)
		}
		return nil
	}
	overridden, err := ingestion.ValidateReceiptConfirmation(verified, amounts, candidateID, overrideReason)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	for index, item := range verified.Receipt.Items {
		observationValues := map[string]any{
			"jobId": job.ID.String(), "itemIndex": index, "item": item,
			"transform": verified.Transform, "transformPage": verified.TransformPage,
		}
		if candidateID != "" {
			observationValues["receiptCandidateId"] = candidateID
		}
		if overridden {
			observationValues["manualAmountOverrideReason"] = strings.TrimSpace(overrideReason)
		}
		observation, err := stdjson.Marshal(observationValues)
		if err != nil {
			return err
		}
		observationJSON := types.JSON[stdjson.RawMessage]{Val: observation}
		var region sql.Null[types.JSON[stdjson.RawMessage]]
		if item.Region != nil {
			encoded, err := stdjson.Marshal(item.Region)
			if err != nil {
				return err
			}
			region = sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: encoded}, Valid: true}
		}
		itemID := sql.Null[gofrsuuid.UUID]{V: itemIDs[index], Valid: true}
		if _, err := psql.Insert(
			im.Into(models.EvidenceLinks.NameExpr(), "household_id", "source_document_id", "purchase_id", "item_id", "evidence_region", "observation"),
			im.Values(psql.Arg(householdID, documentID, purchaseID, itemID, region, observationJSON)),
			im.OnConflict().DoNothing(),
		).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}

func allocationWeights(inputs []*model.AllocationInput) ([]domain.Weight, error) {
	weights := make([]domain.Weight, 0, len(inputs))
	for _, input := range inputs {
		if input == nil || input.Weight <= 0 {
			return nil, fmt.Errorf("%w: allocation weights must be positive", ErrBadInput)
		}
		weights = append(weights, domain.Weight{PoolID: input.PoolID, Value: int64(input.Weight)})
	}
	if _, err := domain.Allocate(1, weights); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	return weights, nil
}

func validatePurchasePaymentInputs(ctx context.Context, tx pgx.Tx, householdID string, inputs []*model.PurchasePaymentInput) ([]model.PurchasePaymentInput, error) {
	result := make([]model.PurchasePaymentInput, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if input == nil || input.PaymentAccountID == "" || input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney {
			return nil, fmt.Errorf("%w: payment allocations require an account and positive amount", ErrBadInput)
		}
		if _, exists := seen[input.PaymentAccountID]; exists {
			return nil, fmt.Errorf("%w: payment account may appear only once", ErrBadInput)
		}
		seen[input.PaymentAccountID] = struct{}{}
		householdUUID, parseErr := gofrsuuid.FromString(householdID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
		}
		accountUUID, parseErr := gofrsuuid.FromString(input.PaymentAccountID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid payment account id", ErrBadInput)
		}
		_, err := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(accountUUID)),
		))).One(ctx, bobpgx.NewTx(tx, nil))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		result = append(result, *input)
	}
	return result, nil
}

func replacePurchasePaymentRows(ctx context.Context, tx pgx.Tx, householdID, purchaseID string, payments []model.PurchasePaymentInput) error {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return err
	}
	purchaseUUID, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return err
	}
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.PurchasePayments.Delete(dm.Where(psql.And(
		models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).Exec(ctx, executor); err != nil {
		return err
	}
	for index, payment := range payments {
		accountUUID, err := gofrsuuid.FromString(payment.PaymentAccountID)
		if err != nil {
			return err
		}
		lineNo, amount := int32(index+1), int64(payment.AmountYen)
		if _, err := models.PurchasePayments.Insert(&models.PurchasePaymentSetter{
			HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, LineNo: &lineNo, AccountID: &accountUUID, AmountYen: &amount,
		}).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}

func purchasePayments(ctx context.Context, tx pgx.Tx, householdID, purchaseID string, legacyAccountID *string, total int64) ([]model.PurchasePaymentInput, error) {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, err
	}
	purchaseUUID, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return nil, err
	}
	rows, err := models.PurchasePayments.Query(sm.Where(psql.And(
		models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	)), sm.OrderBy(models.PurchasePayments.Columns.LineNo)).All(ctx, bobpgx.NewTx(tx, nil))
	if err != nil {
		return nil, err
	}
	payments := make([]model.PurchasePaymentInput, 0, len(rows))
	var paid int64
	for _, row := range rows {
		payment := model.PurchasePaymentInput{PaymentAccountID: row.AccountID.String(), AmountYen: graphLong(row.AmountYen)}
		var ok bool
		paid, ok = checkedAdd(paid, row.AmountYen)
		if !ok {
			return nil, fmt.Errorf("%w: payment total is too large", ErrBadInput)
		}
		payments = append(payments, payment)
	}
	if len(payments) == 0 && legacyAccountID != nil {
		payments = append(payments, model.PurchasePaymentInput{PaymentAccountID: *legacyAccountID, AmountYen: graphLong(total)})
		paid = total
	}
	if len(payments) > 0 && paid != total {
		return nil, fmt.Errorf("%w: payment allocations must equal purchase total", ErrBadInput)
	}
	return payments, nil
}

func correctionAllocationWeights(ctx context.Context, tx pgx.Tx, purchaseID string, itemID *string) ([]domain.Weight, error) {
	purchaseUUID, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return nil, err
	}
	var rows models.ItemAllocationSlice
	executor := bobpgx.NewTx(tx, nil)
	if itemID != nil {
		itemUUID, parseErr := gofrsuuid.FromString(*itemID)
		if parseErr != nil {
			return nil, parseErr
		}
		rows, err = models.ItemAllocations.Query(sm.Where(psql.And(
			models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
			models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(itemUUID)),
		))).All(ctx, executor)
	} else {
		rows, err = models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	}
	if err != nil {
		return nil, err
	}
	poolTotals := make(map[string]int64)
	for _, row := range rows {
		value := row.AmountYen
		if itemID != nil {
			value = row.Weight
		}
		poolID := row.PoolID.String()
		var ok bool
		poolTotals[poolID], ok = checkedAdd(poolTotals[poolID], value)
		if !ok {
			return nil, fmt.Errorf("%w: allocation total is too large", ErrBadInput)
		}
	}
	weights := make([]domain.Weight, 0, len(poolTotals))
	for poolID, amount := range poolTotals {
		if amount > 0 {
			weights = append(weights, domain.Weight{PoolID: poolID, Value: amount})
		}
	}
	slices.SortFunc(weights, func(a, b domain.Weight) int { return strings.Compare(a.PoolID, b.PoolID) })
	if len(weights) == 0 {
		return nil, fmt.Errorf("%w: purchase has no saved allocation", ErrBadInput)
	}
	return weights, nil
}

func sameAllocationWeights(inputs []*model.AllocationInput, saved []domain.Weight) bool {
	if len(inputs) != len(saved) {
		return false
	}
	requested := make(map[string]int64, len(inputs))
	for _, input := range inputs {
		if input == nil || input.PoolID == "" || input.Weight <= 0 {
			return false
		}
		if _, exists := requested[input.PoolID]; exists {
			return false
		}
		requested[input.PoolID] = int64(input.Weight)
	}
	for _, weight := range saved {
		if requested[weight.PoolID] != weight.Value {
			return false
		}
	}
	return true
}

func lockAccountingPeriods(ctx context.Context, tx pgx.Tx, householdID string, periods ...string) error {
	slices.Sort(periods)
	periods = slices.Compact(periods)
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	executor := bobpgx.NewTx(tx, nil)
	for _, period := range periods {
		if err := ensureAccountingPeriod(ctx, executor, householdUUID, period); err != nil {
			return err
		}
		row, err := models.AccountingPeriods.Query(sm.Where(psql.And(
			models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
		)), sm.ForUpdate()).One(ctx, executor)
		if err != nil {
			return err
		}
		if row.Status == "CLOSED" {
			return fmt.Errorf("%w: accounting period is closed", ErrConflict)
		}
	}
	return nil
}

func ensureAccountingPeriod(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, period string) error {
	status := "OPEN"
	revision := int64(0)
	_, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdID, Period: &period, Status: &status, Revision: &revision,
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, executor)
	return err
}

func syncPostedPurchaseAccounting(ctx context.Context, tx pgx.Tx, purchaseID string) error {
	purchaseUUID, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return err
	}
	executor := bobpgx.NewTx(tx, nil)
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID)))).One(ctx, executor)
	if err != nil {
		return err
	}
	householdID := purchase.HouseholdID.String()
	occurredAt, merchant := purchase.OccurredAt, purchase.Merchant
	var paymentAccountID *string
	if purchase.PaymentAccountID.Valid {
		id := purchase.PaymentAccountID.V.String()
		paymentAccountID = &id
	}
	journal, err := models.Journals.Query(sm.Where(psql.And(
		models.Journals.Columns.HouseholdID.EQ(psql.Arg(purchase.HouseholdID)),
		models.Journals.Columns.SourceKind.EQ(psql.Arg("PURCHASE")),
		models.Journals.Columns.SourceID.EQ(psql.Arg(purchaseUUID)),
	))).One(ctx, executor)
	if err != nil {
		return err
	}
	items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	if err != nil {
		return err
	}
	var total int64
	for _, item := range items {
		var ok bool
		total, ok = checkedAdd(total, item.AmountYen)
		if !ok {
			return fmt.Errorf("%w: purchase total is too large", ErrBadInput)
		}
	}
	if total <= 0 {
		return fmt.Errorf("%w: purchase total must be greater than zero", ErrBadInput)
	}
	if _, err := models.Journals.Update((&models.JournalSetter{OccurredAt: &occurredAt}).UpdateMod(), um.Where(models.Journals.Columns.ID.EQ(psql.Arg(journal.ID)))).Exec(ctx, executor); err != nil {
		return err
	}
	if _, err := models.JournalEntries.Delete(dm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journal.ID)))).Exec(ctx, executor); err != nil {
		return err
	}
	allocations, err := models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	if err != nil {
		return err
	}
	if err := refreshPurchaseMemberBurden(ctx, executor, householdID, purchaseUUID, allocations); err != nil {
		return err
	}
	type purchaseShare struct {
		poolID gofrsuuid.UUID
		amount int64
	}
	sharesByPool := make(map[gofrsuuid.UUID]int64)
	for _, allocation := range allocations {
		var ok bool
		sharesByPool[allocation.PoolID], ok = checkedAdd(sharesByPool[allocation.PoolID], allocation.AmountYen)
		if !ok {
			return fmt.Errorf("%w: purchase allocation total is too large", ErrBadInput)
		}
	}
	shares := make([]purchaseShare, 0, len(sharesByPool))
	for poolID, amount := range sharesByPool {
		shares = append(shares, purchaseShare{poolID: poolID, amount: amount})
	}
	slices.SortFunc(shares, func(a, b purchaseShare) int { return strings.Compare(a.poolID.String(), b.poolID.String()) })
	lineNo := 1
	for _, share := range shares {
		if share.amount == 0 {
			continue
		}
		lineNoValue := int32(lineNo)
		zero := int64(0)
		poolValue := sql.Null[gofrsuuid.UUID]{V: share.poolID, Valid: true}
		memo := "Purchase allocation"
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journal.ID, LineNo: &lineNoValue, AccountID: new(sql.Null[gofrsuuid.UUID]),
			PoolID: &poolValue, MemberID: new(sql.Null[gofrsuuid.UUID]), DebitYen: &share.amount, CreditYen: &zero, Memo: &memo,
		}).Exec(ctx, executor); err != nil {
			return err
		}
		lineNo++
	}
	payments, err := purchasePayments(ctx, tx, householdID, purchaseID, paymentAccountID, total)
	if err != nil {
		return err
	}
	if len(payments) == 0 {
		lineNoValue := int32(lineNo)
		zero := int64(0)
		memo := "Payment"
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journal.ID, LineNo: &lineNoValue, AccountID: new(sql.Null[gofrsuuid.UUID]),
			PoolID: new(sql.Null[gofrsuuid.UUID]), MemberID: new(sql.Null[gofrsuuid.UUID]), DebitYen: &zero, CreditYen: &total, Memo: &memo,
		}).Exec(ctx, executor); err != nil {
			return err
		}
	} else {
		for _, payment := range payments {
			accountUUID, parseErr := gofrsuuid.FromString(payment.PaymentAccountID)
			if parseErr != nil {
				return parseErr
			}
			lineNoValue := int32(lineNo)
			amount, zero := int64(payment.AmountYen), int64(0)
			memo := "Payment"
			accountValue := sql.Null[gofrsuuid.UUID]{V: accountUUID, Valid: true}
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journal.ID, LineNo: &lineNoValue, AccountID: &accountValue,
				PoolID: new(sql.Null[gofrsuuid.UUID]), MemberID: new(sql.Null[gofrsuuid.UUID]), DebitYen: &zero, CreditYen: &amount, Memo: &memo,
			}).Exec(ctx, executor); err != nil {
				return err
			}
			lineNo++
		}
	}
	ledgers, err := models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(purchase.HouseholdID)),
		models.LedgerEntries.Columns.SourcePurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).All(ctx, executor)
	if err != nil {
		return err
	}
	for _, ledger := range ledgers {
		if _, err := models.LedgerEntries.Update((&models.LedgerEntrySetter{
			OccurredAt: &occurredAt, Description: &merchant, AmountYen: &total,
		}).UpdateMod(), um.Where(models.LedgerEntries.Columns.ID.EQ(psql.Arg(ledger.ID)))).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}

func (r *mutationResolver) updatePurchase(ctx context.Context, input model.UpdatePurchaseInput) (*model.Purchase, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	purchaseUUID, err := gofrsuuid.FromString(input.PurchaseID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := bobpgx.NewTx(tx, nil)
	observedPurchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	observedHouseholdID := observedPurchase.HouseholdID.String()
	observedOccurredAt := observedPurchase.OccurredAt
	if _, err := requireMember(ctx, r.DB, observedHouseholdID); err != nil {
		return nil, err
	}
	targetOccurredAt := observedOccurredAt
	if input.Patch != nil && input.Patch.OccurredAt != nil {
		targetOccurredAt, err = parseTime(*input.Patch.OccurredAt)
		if err != nil {
			return nil, err
		}
	}
	if err := lockAccountingPeriods(ctx, tx, observedHouseholdID, accountingPeriod(observedOccurredAt), accountingPeriod(targetOccurredAt)); err != nil {
		return nil, err
	}
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	householdID := purchase.HouseholdID.String()
	status := purchase.Status
	if householdID != observedHouseholdID || !purchase.OccurredAt.Equal(observedOccurredAt) {
		return nil, ErrConflict
	}
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(purchase.HouseholdID))), sm.ForShare()).One(ctx, executor); err != nil {
		return nil, err
	}
	if status != string(model.PurchaseStatusDraft) && status != string(model.PurchaseStatusPosted) {
		return nil, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	if input.BaseRevision > int(purchase.Revision) {
		return nil, ErrConflict
	}
	if err := validateChangedFields(input.ChangedFields, "occurredAt", "merchant", "paymentAllocations"); err != nil {
		return nil, err
	}
	for _, field := range input.ChangedFields {
		switch field {
		case "occurredAt":
			if input.Patch == nil || input.Patch.OccurredAt == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "merchant":
			if input.Patch == nil || input.Patch.Merchant == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "paymentAllocations":
			if input.Patch == nil || input.Patch.PaymentAllocations == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		}
	}
	if input.Patch != nil && (input.Patch.OccurredAt != nil && !slices.Contains(input.ChangedFields, "occurredAt") || input.Patch.Merchant != nil && !slices.Contains(input.ChangedFields, "merchant") || input.Patch.PaymentAllocations != nil && !slices.Contains(input.ChangedFields, "paymentAllocations")) {
		return nil, fmt.Errorf("%w: patch contains fields missing from changedFields", ErrBadInput)
	}
	conflict, err := valuesConflict(purchase.FieldRevisions.Val, input.BaseRevision, input.ChangedFields)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, ErrConflict
	}
	newRevision := purchase.Revision + 1
	updatedFields, err := setFieldRevisions(purchase.FieldRevisions.Val, input.ChangedFields, newRevision)
	if err != nil {
		return nil, err
	}
	updatePayments := slices.Contains(input.ChangedFields, "paymentAllocations")
	paymentAccountID := purchase.PaymentAccountID
	if updatePayments {
		paymentAccountID = sql.Null[gofrsuuid.UUID]{}
	}
	if input.Patch != nil {
		if slices.Contains(input.ChangedFields, "occurredAt") {
			occurredAt, err := parseTime(*input.Patch.OccurredAt)
			if err != nil {
				return nil, err
			}
			purchase.OccurredAt = occurredAt
		}
		if slices.Contains(input.ChangedFields, "merchant") {
			value := strings.TrimSpace(*input.Patch.Merchant)
			if len(value) > 240 {
				return nil, fmt.Errorf("%w: merchant name is too long", ErrBadInput)
			}
			purchase.Merchant = value
		}
		if updatePayments {
			payments, err := validatePurchasePaymentInputs(ctx, tx, householdID, input.Patch.PaymentAllocations)
			if err != nil {
				return nil, err
			}
			items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			var total int64
			for _, item := range items {
				var ok bool
				total, ok = checkedAdd(total, item.AmountYen)
				if !ok {
					return nil, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
				}
			}
			if status == string(model.PurchaseStatusPosted) {
				var paid int64
				for _, payment := range payments {
					var ok bool
					paid, ok = checkedAdd(paid, int64(payment.AmountYen))
					if !ok {
						return nil, fmt.Errorf("%w: payment total is too large", ErrBadInput)
					}
				}
				if paid != total {
					return nil, fmt.Errorf("%w: payment allocations must equal purchase total", ErrBadInput)
				}
			}
			if err := replacePurchasePaymentRows(ctx, tx, householdID, input.PurchaseID, payments); err != nil {
				return nil, err
			}
		}
	}
	fieldRevisions := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(updatedFields)}
	if _, err := models.Purchases.Update((&models.PurchaseSetter{
		OccurredAt: &purchase.OccurredAt, Merchant: &purchase.Merchant, PaymentAccountID: &paymentAccountID,
		Revision: &newRevision, FieldRevisions: &fieldRevisions,
	}).UpdateMod(),
		um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))),
		um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if status == string(model.PurchaseStatusPosted) {
		if err := syncPostedPurchaseAccounting(ctx, tx, input.PurchaseID); err != nil {
			return nil, err
		}
	}
	if err := writeEvent(ctx, tx, householdID, "purchase.updated", input.PurchaseID, newRevision, input.ChangedFields, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, input.PurchaseID)
}

func (r *mutationResolver) updatePurchaseItem(ctx context.Context, input model.UpdatePurchaseItemInput) (*model.PurchaseItem, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	itemUUID, err := gofrsuuid.FromString(input.ItemID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := bobpgx.NewTx(tx, nil)
	observedItem, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := observedItem.LoadPurchase(ctx, executor); err != nil {
		return nil, err
	}
	if observedItem.R.Purchase == nil {
		return nil, ErrNotFound
	}
	observedPurchase := observedItem.R.Purchase
	householdID := observedItem.HouseholdID.String()
	purchaseID := observedItem.PurchaseID.String()
	observedOccurredAt := observedPurchase.OccurredAt
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	household, err := models.Households.Query(
		sm.Where(models.Households.Columns.ID.EQ(psql.Arg(observedItem.HouseholdID))),
		sm.ForShare(),
	).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := lockAccountingPeriods(ctx, tx, householdID, accountingPeriod(observedOccurredAt)); err != nil {
		return nil, err
	}
	purchaseUUID := observedItem.PurchaseID
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item, err := models.PurchaseItems.Query(sm.Where(psql.And(
		models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)),
		models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !purchase.OccurredAt.Equal(observedOccurredAt) {
		return nil, ErrConflict
	}
	status := purchase.Status
	if status != string(model.PurchaseStatusDraft) && status != string(model.PurchaseStatusPosted) {
		return nil, fmt.Errorf("%w: closed purchases require a correction", ErrConflict)
	}
	name, amount := item.Name, item.AmountYen
	var categoryID *string
	if item.CategoryID.Valid {
		category := item.CategoryID.V.String()
		categoryID = &category
	}
	var productVariantID *string
	if item.ProductVariantID.Valid {
		variant := item.ProductVariantID.V.String()
		productVariantID = &variant
	}
	allocationOverride := item.AllocationOverride
	revision := item.Revision
	fieldRevisions := item.FieldRevisions.Val
	merchant := purchase.Merchant
	ruleVersion := household.DefaultAllocationVersion
	if input.BaseRevision > int(item.Revision) {
		return nil, ErrConflict
	}
	if err := validateChangedFields(input.ChangedFields, "name", "amountYen", "quantity", "printedUnitPrice", "categoryId", "productVariantId", "allocations"); err != nil {
		return nil, err
	}
	for _, field := range input.ChangedFields {
		switch field {
		case "name":
			if input.Name == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "amountYen":
			if input.AmountYen == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		case "allocations":
			if input.Allocations == nil {
				return nil, fmt.Errorf("%w: changed field has no value", ErrBadInput)
			}
		}
	}
	if input.Name != nil && !slices.Contains(input.ChangedFields, "name") || input.AmountYen != nil && !slices.Contains(input.ChangedFields, "amountYen") ||
		input.Quantity.IsSet() && !slices.Contains(input.ChangedFields, "quantity") ||
		input.PrintedUnitPrice.IsSet() && !slices.Contains(input.ChangedFields, "printedUnitPrice") ||
		input.CategoryID != nil && !slices.Contains(input.ChangedFields, "categoryId") || input.ProductVariantID != nil && !slices.Contains(input.ChangedFields, "productVariantId") || input.Allocations != nil && !slices.Contains(input.ChangedFields, "allocations") {
		return nil, fmt.Errorf("%w: patch contains fields missing from changedFields", ErrBadInput)
	}
	if input.ProductLearning != nil && !slices.Contains(input.ChangedFields, "productVariantId") {
		return nil, fmt.Errorf("%w: product learning requires a product variant change", ErrBadInput)
	}
	conflict, err := valuesConflict(item.FieldRevisions.Val, input.BaseRevision, input.ChangedFields)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, ErrConflict
	}
	currentMeasure := purchaseItemMeasure{
		quantityNumerator: item.QuantityNumerator, quantityDenominator: item.QuantityDenominator, quantityUnit: item.QuantityUnit,
		printedUnitPriceYen: item.PrintedUnitPriceYen, printedUnitPriceQuantityNumerator: item.PrintedUnitPriceQuantityNumerator,
		printedUnitPriceQuantityDenominator: item.PrintedUnitPriceQuantityDenominator, printedUnitPriceUnit: item.PrintedUnitPriceUnit,
	}
	newMeasure, err := measureFromUpdate(input, currentMeasure)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	newName, newAmount, newCategory := name, amount, categoryID
	if input.Name != nil {
		newName = strings.TrimSpace(*input.Name)
		if newName == "" || len(newName) > 240 {
			return nil, fmt.Errorf("%w: invalid item name", ErrBadInput)
		}
	}
	if input.AmountYen != nil {
		newAmount = int64(*input.AmountYen)
		if newAmount < 0 || newAmount > maxMoney {
			return nil, fmt.Errorf("%w: invalid item amount", ErrBadInput)
		}
	}
	updateCategoryID := slices.Contains(input.ChangedFields, "categoryId")
	if updateCategoryID {
		newCategory = input.CategoryID
	}
	updateProductVariantID := slices.Contains(input.ChangedFields, "productVariantId")
	newProductVariantID := productVariantID
	if updateProductVariantID {
		newProductVariantID = input.ProductVariantID
	}
	learningScope := model.ProductLearningScopeItemOnly
	if input.ProductLearning != nil {
		learningScope = *input.ProductLearning
	}
	if !learningScope.IsValid() || input.VariantName != nil && learningScope != model.ProductLearningScopeVariantName {
		return nil, fmt.Errorf("%w: invalid product learning scope", ErrBadInput)
	}
	if updateProductVariantID && newProductVariantID != nil {
		variantUUID, err := gofrsuuid.FromString(*newProductVariantID)
		if err != nil {
			return nil, ErrNotFound
		}
		variant, err := models.ProductVariants.Query(sm.Where(psql.And(
			models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
			models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(observedItem.HouseholdID)),
			models.ProductVariants.Columns.Archived.EQ(psql.Arg(false)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if err := variant.LoadProductConcept(ctx, executor); err != nil {
			return nil, err
		}
		if variant.R.ProductConcept == nil || variant.R.ProductConcept.Archived {
			return nil, ErrNotFound
		}
	}
	if learningScope != model.ProductLearningScopeItemOnly && newProductVariantID == nil {
		return nil, fmt.Errorf("%w: product learning requires a linked variant", ErrBadInput)
	}
	var weights []domain.Weight
	var appliedRuleID *string
	var appliedRuleVersion = ruleVersion
	newAllocationOverride := allocationOverride
	if input.Allocations != nil {
		weights, err = allocationWeights(input.Allocations)
		newAllocationOverride = true
	} else if (updateCategoryID || updateProductVariantID) && !allocationOverride {
		weights, appliedRuleID, appliedRuleVersion, err = allocationRuleWeights(ctx, tx, householdID, newCategory, newProductVariantID, normalizeMerchant(merchant))
		if err == nil && appliedRuleID == nil {
			weights, err = loadWeights(ctx, tx, householdID, ruleVersion)
		}
	} else {
		allocations, queryErr := models.ItemAllocations.Query(
			sm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(itemUUID))),
			sm.OrderBy(models.ItemAllocations.Columns.PoolID),
		).All(ctx, executor)
		if queryErr != nil {
			return nil, queryErr
		}
		for _, allocation := range allocations {
			weight := domain.Weight{PoolID: allocation.PoolID.String(), Value: allocation.Weight}
			if len(weights) == 0 {
				if allocation.RuleID.Valid {
					ruleID := allocation.RuleID.V.String()
					appliedRuleID = &ruleID
				}
				appliedRuleVersion = allocation.RuleVersion
			}
			weights = append(weights, weight)
		}
		if len(weights) == 0 {
			weights, err = loadWeights(ctx, tx, householdID, ruleVersion)
		}
	}
	if err != nil {
		return nil, err
	}
	allocated, err := domain.Allocate(newAmount, weights)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	newRevision := revision + 1
	updatedFields, err := setFieldRevisions(fieldRevisions, input.ChangedFields, newRevision)
	if err != nil {
		return nil, err
	}
	if learningScope == model.ProductLearningScopeVariantName {
		variantName := strings.TrimSpace(trimOptional(input.VariantName))
		variantKey := normalizeProductText(variantName)
		if variantKey == "" || len(variantName) > 240 {
			return nil, fmt.Errorf("%w: invalid normalized product name", ErrBadInput)
		}
		variantUUID, err := gofrsuuid.FromString(*newProductVariantID)
		if err != nil {
			return nil, ErrNotFound
		}
		householdUUID := observedItem.HouseholdID
		if _, err := models.ProductVariants.Update((&models.ProductVariantSetter{
			Name: &variantName, NormalizedName: &variantKey,
		}).UpdateMod(),
			um.Where(psql.And(
				models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
				models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			)),
			um.SetCol("revision").To(models.ProductVariants.Columns.Revision.Plus(psql.Arg(int64(1)))),
			um.SetCol("updated_at").To(psql.F("now")),
		).Exec(ctx, executor); err != nil {
			return nil, err
		}
		variant, err := models.ProductVariants.Query(sm.Where(models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)))).One(ctx, executor)
		if err != nil {
			return nil, err
		}
		if err := writeEvent(ctx, tx, householdID, "product.variant.renamed", *newProductVariantID, variant.Revision, []string{"name"}, user.MemberID); err != nil {
			return nil, err
		}
	}
	itemName, itemAmount := newName, newAmount
	categoryValue := sql.Null[gofrsuuid.UUID]{}
	if newCategory != nil {
		categoryUUID, err := gofrsuuid.FromString(*newCategory)
		if err != nil {
			return nil, ErrNotFound
		}
		categoryValue = sql.Null[gofrsuuid.UUID]{V: categoryUUID, Valid: true}
	}
	variantValue := sql.Null[gofrsuuid.UUID]{}
	if newProductVariantID != nil {
		variantUUID, err := gofrsuuid.FromString(*newProductVariantID)
		if err != nil {
			return nil, ErrNotFound
		}
		variantValue = sql.Null[gofrsuuid.UUID]{V: variantUUID, Valid: true}
	}
	fieldRevisionValue := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(updatedFields)}
	if _, err := models.PurchaseItems.Update((&models.PurchaseItemSetter{
		Name: &itemName, AmountYen: &itemAmount, CategoryID: &categoryValue,
		ProductVariantID: &variantValue, AllocationOverride: &newAllocationOverride,
		Revision: &newRevision, FieldRevisions: &fieldRevisionValue,
		QuantityNumerator: &newMeasure.quantityNumerator, QuantityDenominator: &newMeasure.quantityDenominator, QuantityUnit: &newMeasure.quantityUnit,
		PrintedUnitPriceYen: &newMeasure.printedUnitPriceYen, PrintedUnitPriceQuantityNumerator: &newMeasure.printedUnitPriceQuantityNumerator,
		PrintedUnitPriceQuantityDenominator: &newMeasure.printedUnitPriceQuantityDenominator, PrintedUnitPriceUnit: &newMeasure.printedUnitPriceUnit,
	}).UpdateMod(),
		um.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID))),
		um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if updateProductVariantID && newProductVariantID != nil {
		merchantKey := normalizeMerchant(merchant)
		if learningScope == model.ProductLearningScopeMerchantAlias {
			if merchantKey == "" {
				return nil, fmt.Errorf("%w: merchant-specific learning requires a merchant", ErrBadInput)
			}
			var aliasID string
			if err := upsertProductAlias(ctx, tx, householdID, *newProductVariantID, merchantKey, name, normalizeProductText(name), &aliasID); err != nil {
				return nil, err
			}
			aliasUUID, err := gofrsuuid.FromString(aliasID)
			if err != nil {
				return nil, ErrNotFound
			}
			alias, err := models.ProductAliases.Query(sm.Where(models.ProductAliases.Columns.ID.EQ(psql.Arg(aliasUUID)))).One(ctx, executor)
			if err != nil {
				return nil, err
			}
			if err := writeEvent(ctx, tx, householdID, "product.alias.learned", aliasID, alias.Revision, []string{"alias", "variantId", "merchant"}, user.MemberID); err != nil {
				return nil, err
			}
		}
		correctionUUID := generatedUUID(newID())
		variantUUID, err := gofrsuuid.FromString(*newProductVariantID)
		if err != nil {
			return nil, ErrNotFound
		}
		actorUUID, err := gofrsuuid.FromString(user.MemberID)
		if err != nil {
			return nil, err
		}
		var previousVariant sql.Null[gofrsuuid.UUID]
		if productVariantID != nil {
			previousUUID, err := gofrsuuid.FromString(*productVariantID)
			if err != nil {
				return nil, ErrNotFound
			}
			previousVariant = sql.Null[gofrsuuid.UUID]{V: previousUUID, Valid: true}
		}
		correctionScope := string(learningScope)
		if _, err := models.ProductCorrections.Insert(&models.ProductCorrectionSetter{
			ID: &correctionUUID, HouseholdID: &observedItem.HouseholdID, PurchaseID: &purchaseUUID,
			ItemID: &itemUUID, RawName: &name, PreviousProductVariantID: &previousVariant,
			ProductVariantID: &variantUUID, LearningScope: &correctionScope, MerchantKey: &merchantKey, ActorID: &actorUUID,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	if _, err := models.ItemAllocations.Delete(dm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(itemUUID)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	for _, allocation := range allocated {
		poolUUID := generatedUUID(allocation.PoolID)
		var ruleUUID sql.Null[gofrsuuid.UUID]
		if appliedRuleID != nil {
			ruleUUID = sql.Null[gofrsuuid.UUID]{V: generatedUUID(*appliedRuleID), Valid: true}
		}
		if _, err := models.ItemAllocations.Insert(&models.ItemAllocationSetter{
			HouseholdID: &observedItem.HouseholdID, PurchaseID: &purchaseUUID, ItemID: &itemUUID,
			PoolID: &poolUUID, Weight: &allocation.Weight, AmountYen: &allocation.Amount,
			RuleVersion: &appliedRuleVersion, RuleID: &ruleUUID,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	if _, err := models.Purchases.Update(um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))),
		um.SetCol("revision").To(models.Purchases.Columns.Revision.Plus(psql.Arg(int64(1)))),
		um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, householdID, "purchase.item.updated", input.ItemID, newRevision, input.ChangedFields, user.MemberID); err != nil {
		return nil, err
	}
	if status == string(model.PurchaseStatusPosted) {
		if err := syncPostedPurchaseAccounting(ctx, tx, purchaseID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	loadedPurchase, err := loadPurchase(ctx, r.DB, purchaseID)
	if err != nil {
		return nil, err
	}
	for _, item := range loadedPurchase.Items {
		if item.ID == input.ItemID {
			return item, nil
		}
	}
	return nil, ErrNotFound
}

func (r *mutationResolver) postPurchase(ctx context.Context, purchaseID string, expectedRevision int, idempotencyKey string) (*model.Purchase, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return nil, fmt.Errorf("%w: idempotency key is required", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	purchaseUUID, err := gofrsuuid.FromString(purchaseID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid purchase id", ErrBadInput)
	}
	executor := bobpgx.NewTx(tx, nil)
	purchase, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	householdUUID, householdID := purchase.HouseholdID, purchase.HouseholdID.String()
	occurredAt := purchase.OccurredAt
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	operation := "postPurchase"
	fingerprint, err := mutationRequestFingerprint(struct {
		PurchaseID       string
		ExpectedRevision int
	}{PurchaseID: purchaseID, ExpectedRevision: expectedRevision})
	if err != nil {
		return nil, err
	}
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(idempotencyKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, executor)
	if err == nil {
		if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
			return nil, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return loadPurchase(ctx, r.DB, existing.ResourceID.V.String())
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	period := accountingPeriod(occurredAt)
	if err := ensureAccountingPeriod(ctx, executor, householdUUID, period); err != nil {
		return nil, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if periodRow.Status == "CLOSED" {
		return nil, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}
	purchase, err = models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	status, revision := purchase.Status, purchase.Revision
	occurredAt = purchase.OccurredAt
	var paymentAccountID *string
	if purchase.PaymentAccountID.Valid {
		id := purchase.PaymentAccountID.V.String()
		paymentAccountID = &id
	}
	if accountingPeriod(occurredAt) != period {
		return nil, ErrConflict
	}
	if revision != int64(expectedRevision) {
		return nil, ErrConflict
	}
	if status != string(model.PurchaseStatusDraft) {
		return nil, fmt.Errorf("%w: purchase is already posted", ErrConflict)
	}
	items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	var total int64
	for _, item := range items {
		var ok bool
		total, ok = checkedAdd(total, item.AmountYen)
		if !ok {
			return nil, fmt.Errorf("%w: purchase total is too large", ErrBadInput)
		}
	}
	if total <= 0 {
		return nil, fmt.Errorf("%w: purchase total must be greater than zero", ErrBadInput)
	}
	journalID, ledgerID := newID(), newID()
	journalUUID, ledgerUUID := generatedUUID(journalID), generatedUUID(ledgerID)
	actorUUID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, err
	}
	purchaseSource := "PURCHASE"
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalUUID, HouseholdID: &householdUUID, SourceKind: &purchaseSource, SourceID: &purchaseUUID,
		IdempotencyKey: &idempotencyKey, OccurredAt: &occurredAt, CreatedBy: &actorUUID,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	allocations, err := models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := snapshotPurchaseMemberBurden(ctx, executor, householdID, purchaseID, allocations); err != nil {
		return nil, err
	}
	if err := snapshotPurchasePaymentFunding(ctx, executor, householdID, purchase); err != nil {
		return nil, err
	}
	type purchaseShare struct {
		poolID gofrsuuid.UUID
		amount int64
	}
	shareAmounts := make(map[gofrsuuid.UUID]int64)
	for _, allocation := range allocations {
		var ok bool
		shareAmounts[allocation.PoolID], ok = checkedAdd(shareAmounts[allocation.PoolID], allocation.AmountYen)
		if !ok {
			return nil, fmt.Errorf("%w: purchase allocation total is too large", ErrBadInput)
		}
	}
	shares := make([]purchaseShare, 0, len(shareAmounts))
	for poolID, amount := range shareAmounts {
		shares = append(shares, purchaseShare{poolID: poolID, amount: amount})
	}
	slices.SortFunc(shares, func(a, b purchaseShare) int { return strings.Compare(a.poolID.String(), b.poolID.String()) })
	lineNo := 1
	for _, share := range shares {
		if share.amount == 0 {
			continue
		}
		lineNoValue, zero := int32(lineNo), int64(0)
		poolValue := sql.Null[gofrsuuid.UUID]{V: share.poolID, Valid: true}
		memo := "Purchase allocation"
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journalUUID, LineNo: &lineNoValue, AccountID: new(sql.Null[gofrsuuid.UUID]),
			PoolID: &poolValue, MemberID: new(sql.Null[gofrsuuid.UUID]), DebitYen: &share.amount, CreditYen: &zero, Memo: &memo,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
		lineNo++
	}
	payments, err := purchasePayments(ctx, tx, householdID, purchaseID, paymentAccountID, total)
	if err != nil {
		return nil, err
	}
	if len(payments) == 0 {
		lineNoValue, zero := int32(lineNo), int64(0)
		memo := "Payment"
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journalUUID, LineNo: &lineNoValue, AccountID: new(sql.Null[gofrsuuid.UUID]),
			PoolID: new(sql.Null[gofrsuuid.UUID]), MemberID: new(sql.Null[gofrsuuid.UUID]), DebitYen: &zero, CreditYen: &total, Memo: &memo,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	} else {
		for _, payment := range payments {
			accountUUID, parseErr := gofrsuuid.FromString(payment.PaymentAccountID)
			if parseErr != nil {
				return nil, parseErr
			}
			lineNoValue, zero, amount := int32(lineNo), int64(0), int64(payment.AmountYen)
			memo := "Payment"
			accountValue := sql.Null[gofrsuuid.UUID]{V: accountUUID, Valid: true}
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &lineNoValue, AccountID: &accountValue,
				PoolID: new(sql.Null[gofrsuuid.UUID]), MemberID: new(sql.Null[gofrsuuid.UUID]), DebitYen: &zero, CreditYen: &amount, Memo: &memo,
			}).Exec(ctx, executor); err != nil {
				return nil, err
			}
			lineNo++
		}
	}
	merchant := purchase.Merchant
	purchaseLedgerKind := "PURCHASE"
	noUUID := sql.Null[gofrsuuid.UUID]{}
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{
		ID: &ledgerUUID, HouseholdID: &householdUUID, JournalID: &journalUUID, Kind: &purchaseLedgerKind,
		OccurredAt: &occurredAt, Description: &merchant, AmountYen: &total, PoolID: &noUUID,
		SourcePurchaseID: &sql.Null[gofrsuuid.UUID]{V: purchaseUUID, Valid: true},
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	newRevision := revision + 1
	posted := string(model.PurchaseStatusPosted)
	updatedAt := time.Now().UTC()
	if _, err := models.Purchases.Update((&models.PurchaseSetter{Status: &posted, Revision: &newRevision, UpdatedAt: &updatedAt}).UpdateMod(), um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	purchaseResource := sql.Null[gofrsuuid.UUID]{V: purchaseUUID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdUUID, Key: &idempotencyKey, Operation: &operation, ResourceID: &purchaseResource,
		RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, householdID, "purchase.posted", purchaseID, newRevision, []string{"status"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, purchaseID)
}

func (r *mutationResolver) recordFunding(ctx context.Context, input model.RecordFundingInput) (*model.LedgerEntryData, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	if input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney || input.IdempotencyKey == "" {
		return nil, fmt.Errorf("%w: invalid funding amount or idempotency key", ErrBadInput)
	}
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.MemberID); err != nil {
		return nil, err
	}
	householdID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	memberID, err := gofrsuuid.FromString(input.MemberID)
	if err != nil {
		return nil, fmt.Errorf("parse member id: %w", err)
	}
	poolID, err := gofrsuuid.FromString(input.PoolID)
	if err != nil {
		return nil, fmt.Errorf("parse pool id: %w", err)
	}
	executor := bobpgx.NewPool(r.DB)
	poolExists, err := models.Pools.Query(sm.Where(psql.And(
		models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Pools.Columns.ID.EQ(psql.Arg(poolID)),
	))).Exists(ctx, executor)
	if err != nil {
		return nil, err
	}
	if !poolExists {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	transactionExecutor := bobpgx.NewTx(tx, nil)
	operation := "recordFunding"
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(input.IdempotencyKey)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, transactionExecutor)
	if err == nil && existing.ResourceID.Valid {
		if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
			return nil, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return loadLedgerEntry(ctx, r.DB, existing.ResourceID.V.String())
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	period := accountingPeriod(occurredAt)
	if _, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdID, Period: &period, Status: new("OPEN"), Revision: new(int64(0)),
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, transactionExecutor)
	if err != nil {
		return nil, err
	}
	if periodRow.Status == "CLOSED" {
		return nil, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}
	var sourceAccountID, destinationAccountID *gofrsuuid.UUID
	if input.SourceAccountID != nil {
		id, parseErr := gofrsuuid.FromString(*input.SourceAccountID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid source account id", ErrBadInput)
		}
		account, accountErr := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(id)),
		))).One(ctx, transactionExecutor)
		if errors.Is(accountErr, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if accountErr != nil {
			return nil, accountErr
		}
		if account.Kind == string(model.AccountKindCard) {
			return nil, fmt.Errorf("%w: funding source must be a non-card account", ErrBadInput)
		}
		if account.PoolID.Valid {
			pool, poolErr := models.Pools.Query(sm.Where(psql.And(
				models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
				models.Pools.Columns.ID.EQ(psql.Arg(account.PoolID.V)),
			))).One(ctx, transactionExecutor)
			if poolErr != nil {
				return nil, poolErr
			}
			if pool.Kind == string(model.PoolKindPersonal) {
				fundingRule, ruleErr := models.PoolFundingRules.Query(sm.Where(psql.And(
					models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
					models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(pool.ID)),
					models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
				))).One(ctx, transactionExecutor)
				if ruleErr != nil {
					return nil, ruleErr
				}
				if !fundingRule.OwnerMemberID.Valid || fundingRule.OwnerMemberID.V != memberID {
					return nil, fmt.Errorf("%w: personal source account must belong to the funding member", ErrBadInput)
				}
			}
		}
		sourceAccountID = &id
	}
	if input.DestinationAccountID != nil {
		id, parseErr := gofrsuuid.FromString(*input.DestinationAccountID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid destination account id", ErrBadInput)
		}
		account, accountErr := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(id)),
		))).One(ctx, transactionExecutor)
		if errors.Is(accountErr, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if accountErr != nil {
			return nil, accountErr
		}
		if !account.PoolID.Valid || account.PoolID.V != poolID || account.Kind == string(model.AccountKindCard) {
			return nil, fmt.Errorf("%w: funding destination must be a non-card account in the funded pool", ErrBadInput)
		}
		destinationAccountID = &id
	}
	if sourceAccountID != nil && destinationAccountID != nil && *sourceAccountID == *destinationAccountID {
		return nil, fmt.Errorf("%w: funding source and destination accounts must differ", ErrBadInput)
	}
	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.FundingSchedules.Columns.MemberID.EQ(psql.Arg(memberID)),
		models.FundingSchedules.Columns.PoolID.EQ(psql.Arg(poolID)),
		models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
	)), sm.OrderBy(models.FundingSchedules.Columns.CreatedAt).Desc(), sm.Limit(1)).All(ctx, transactionExecutor)
	if err != nil {
		return nil, err
	}
	var scheduleID sql.Null[gofrsuuid.UUID]
	if len(schedules) > 0 {
		scheduleID = sql.Null[gofrsuuid.UUID]{V: schedules[0].ID, Valid: true}
	}
	obligationUUID := generatedUUID(newID())
	amount := int64(input.AmountYen)
	if _, err := models.FundingObligations.Insert(&models.FundingObligationSetter{
		ID: &obligationUUID, HouseholdID: &householdID, ScheduleID: &scheduleID, MemberID: &memberID,
		PoolID: &poolID, Period: &period, AmountYen: new(int64(0)), PaidYen: &amount,
	}, im.OnConflict("household_id", "member_id", "pool_id", "period").DoUpdate(
		im.SetCol("paid_yen").To(psql.Quote("funding_obligations", "paid_yen").Plus(im.Excluded("paid_yen"))),
	)).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	journalUUID, ledgerUUID := generatedUUID(newID()), generatedUUID(newID())
	sourceKind := string("FUNDING")
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalUUID, HouseholdID: &householdID, SourceKind: &sourceKind, SourceID: &ledgerUUID,
		IdempotencyKey: &input.IdempotencyKey, OccurredAt: &occurredAt, CreatedBy: &memberID,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	poolReference := sql.Null[gofrsuuid.UUID]{V: poolID, Valid: true}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalUUID, LineNo: new(int32(1)), PoolID: &poolReference, DebitYen: &amount, Memo: new("Pool funding"),
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalUUID, LineNo: new(int32(2)), PoolID: &poolReference, DebitYen: new(int64(0)), CreditYen: &amount, Memo: new("Member contribution"),
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if sourceAccountID != nil || destinationAccountID != nil {
		noAccount, noPool, noMember := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
		lineThree, lineFour := int32(3), int32(4)
		sourceDebit, sourceCredit := int64(0), amount
		destinationDebit, destinationCredit := amount, int64(0)
		sourceAccount := noAccount
		destinationAccount := noAccount
		if sourceAccountID == nil {
			sourceCredit = 0
		} else {
			sourceAccount = sql.Null[gofrsuuid.UUID]{V: *sourceAccountID, Valid: true}
		}
		if destinationAccountID == nil {
			destinationDebit = 0
		} else {
			destinationAccount = sql.Null[gofrsuuid.UUID]{V: *destinationAccountID, Valid: true}
		}
		if sourceAccountID == nil && destinationAccountID != nil {
			sourceDebit = 0
			sourceCredit = amount
		}
		if sourceAccountID != nil && destinationAccountID == nil {
			destinationDebit = 0
			destinationCredit = amount
		}
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journalUUID, LineNo: &lineThree, AccountID: &destinationAccount, PoolID: &noPool, MemberID: &noMember,
			DebitYen: &destinationDebit, CreditYen: &destinationCredit, Memo: new("Funding destination"),
		}).Exec(ctx, transactionExecutor); err != nil {
			return nil, err
		}
		if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
			JournalID: &journalUUID, LineNo: &lineFour, AccountID: &sourceAccount, PoolID: &noPool, MemberID: &noMember,
			DebitYen: &sourceDebit, CreditYen: &sourceCredit, Memo: new("Funding source"),
		}).Exec(ctx, transactionExecutor); err != nil {
			return nil, err
		}
	}
	memberReference := sql.Null[gofrsuuid.UUID]{V: memberID, Valid: true}
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{
		ID: &ledgerUUID, HouseholdID: &householdID, JournalID: &journalUUID, Kind: new(string("FUNDING")),
		OccurredAt: &occurredAt, Description: new("Pool funding"), AmountYen: &amount,
		FromMemberID: &memberReference, PoolID: &poolReference,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	resourceID := sql.Null[gofrsuuid.UUID]{V: ledgerUUID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdID, Key: &input.IdempotencyKey, Operation: &operation, ResourceID: &resourceID,
		RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "funding.recorded", ledgerUUID.String(), 1, []string{"amountYen"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadLedgerEntry(ctx, r.DB, ledgerUUID.String())
}

func verifyHouseholdMember(ctx context.Context, db *pgxpool.Pool, householdID, memberID string) error {
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	memberUUID, err := gofrsuuid.FromString(memberID)
	if err != nil {
		return fmt.Errorf("%w: invalid member id", ErrBadInput)
	}
	_, err = models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberUUID)),
	))).One(ctx, bobpgx.NewPool(db))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return nil
}

func loadLedgerEntry(ctx context.Context, db *pgxpool.Pool, entryID string) (*model.LedgerEntryData, error) {
	entryUUID, err := gofrsuuid.FromString(entryID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid ledger entry id", ErrBadInput)
	}
	row, err := models.LedgerEntries.Query(sm.Where(models.LedgerEntries.Columns.ID.EQ(psql.Arg(entryUUID)))).One(ctx, bobpgx.NewPool(db))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	result := &model.LedgerEntryData{
		ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Kind: row.Kind, OccurredAt: timeString(row.OccurredAt),
		Description: row.Description, AmountYen: graphLong(row.AmountYen), Currency: "JPY", CreatedAt: timeString(row.CreatedAt),
	}
	if row.FromMemberID.Valid {
		value := row.FromMemberID.V.String()
		result.FromMemberID = &value
	}
	if row.ToMemberID.Valid {
		value := row.ToMemberID.V.String()
		result.ToMemberID = &value
	}
	if row.PoolID.Valid {
		value := row.PoolID.V.String()
		result.PoolID = &value
	}
	if row.SourcePurchaseID.Valid {
		value := row.SourcePurchaseID.V.String()
		result.SourcePurchaseID = &value
	}
	transfer, transferErr := models.Transfers.Query(sm.Where(models.Transfers.Columns.LedgerEntryID.EQ(psql.Arg(row.ID)))).One(ctx, bobpgx.NewPool(db))
	if transferErr == nil {
		transferID := transfer.ID.String()
		result.TransferID = &transferID
		if transfer.LoanID.Valid {
			loanID := transfer.LoanID.V.String()
			result.LoanID = &loanID
		}
	} else if !errors.Is(transferErr, sql.ErrNoRows) {
		return nil, transferErr
	}
	if result.LoanID == nil && result.Kind == "TRANSFER" && result.TransferID != nil {
		if transferErr != nil {
			return nil, transferErr
		}
		if transfer.Kind == string(model.TransferKindLoan) {
			result.LoanID = result.TransferID
		}
	}
	return result, nil
}

type transferCommand struct {
	HouseholdID          string             `json:"householdId"`
	FromMemberID         string             `json:"fromMemberId"`
	ToMemberID           string             `json:"toMemberId"`
	Kind                 model.TransferKind `json:"kind"`
	LoanID               *string            `json:"loanId,omitempty"`
	PaymentAccountID     *string            `json:"paymentAccountId,omitempty"`
	DestinationAccountID *string            `json:"destinationAccountId,omitempty"`
	AmountYen            model.Long         `json:"amountYen"`
	OccurredAt           string             `json:"occurredAt"`
	Description          string             `json:"description"`
	IdempotencyKey       string             `json:"idempotencyKey"`
}

func (r *mutationResolver) recordTransfer(ctx context.Context, input model.RecordTransferInput) (*model.LedgerEntryData, error) {
	return r.recordTransferCommand(ctx, transferCommand{
		HouseholdID: input.HouseholdID, FromMemberID: input.FromMemberID, ToMemberID: input.ToMemberID,
		Kind: model.TransferKind(input.Kind), PaymentAccountID: input.PaymentAccountID,
		DestinationAccountID: input.DestinationAccountID, AmountYen: input.AmountYen,
		OccurredAt: input.OccurredAt, Description: input.Description, IdempotencyKey: input.IdempotencyKey,
	})
}

func (r *mutationResolver) recordRepayment(ctx context.Context, input model.RecordRepaymentInput) (*model.LedgerEntryData, error) {
	return r.recordTransferCommand(ctx, transferCommand{
		HouseholdID: input.HouseholdID, FromMemberID: input.FromMemberID, ToMemberID: input.ToMemberID,
		Kind: model.TransferKindRepayment, LoanID: &input.LoanID, PaymentAccountID: input.PaymentAccountID,
		DestinationAccountID: input.DestinationAccountID, AmountYen: input.AmountYen,
		OccurredAt: input.OccurredAt, Description: input.Description, IdempotencyKey: input.IdempotencyKey,
	})
}

func (r *mutationResolver) recordTransferCommand(ctx context.Context, input transferCommand) (*model.LedgerEntryData, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	if input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney || input.IdempotencyKey == "" || !input.Kind.IsValid() {
		return nil, fmt.Errorf("%w: invalid transfer", ErrBadInput)
	}
	if input.FromMemberID == input.ToMemberID {
		return nil, fmt.Errorf("%w: transfer participants must differ", ErrBadInput)
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.FromMemberID); err != nil {
		return nil, err
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.ToMemberID); err != nil {
		return nil, err
	}
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	if input.Kind == model.TransferKindRepayment && input.LoanID == nil {
		return nil, fmt.Errorf("%w: repayment requires a loan", ErrBadInput)
	}
	if input.Kind != model.TransferKindRepayment && input.LoanID != nil {
		return nil, fmt.Errorf("%w: loanId is only valid for a repayment", ErrBadInput)
	}
	var paymentPoolID *gofrsuuid.UUID
	var paymentPoolKind string
	var paymentAccountUUID *gofrsuuid.UUID
	var destinationAccountUUID *gofrsuuid.UUID
	if input.PaymentAccountID != nil {
		accountID, err := gofrsuuid.FromString(*input.PaymentAccountID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid payment account id", ErrBadInput)
		}
		householdID, err := gofrsuuid.FromString(input.HouseholdID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
		}
		poolExecutor := bobpgx.NewPool(r.DB)
		paymentAccountUUID = &accountID
		account, err := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(accountID)),
		))).One(ctx, poolExecutor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if account.Kind == string(model.AccountKindCard) {
			return nil, fmt.Errorf("%w: transfer source must be a non-card account", ErrBadInput)
		}
		if !account.PoolID.Valid {
			return nil, fmt.Errorf("%w: payment account is not assigned to a pool", ErrBadInput)
		}
		pool, err := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Pools.Columns.ID.EQ(psql.Arg(account.PoolID.V)),
		))).One(ctx, poolExecutor)
		if err != nil {
			return nil, err
		}
		paymentPoolID = &account.PoolID.V
		paymentPoolKind = pool.Kind
		if pool.Kind == string(model.PoolKindPersonal) {
			fundingRule, err := models.PoolFundingRules.Query(sm.Where(psql.And(
				models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
				models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(pool.ID)),
				models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
			))).One(ctx, poolExecutor)
			if err != nil {
				return nil, err
			}
			fromMemberID, err := gofrsuuid.FromString(input.FromMemberID)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid source member id", ErrBadInput)
			}
			if !fundingRule.OwnerMemberID.Valid || fundingRule.OwnerMemberID.V != fromMemberID {
				return nil, fmt.Errorf("%w: personal payment account must belong to the source member", ErrBadInput)
			}
		}
	}
	if input.DestinationAccountID != nil {
		destinationID, parseErr := gofrsuuid.FromString(*input.DestinationAccountID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid destination account id", ErrBadInput)
		}
		householdID, parseErr := gofrsuuid.FromString(input.HouseholdID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
		}
		executor := bobpgx.NewPool(r.DB)
		account, accountErr := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.EQ(psql.Arg(destinationID)),
		))).One(ctx, executor)
		if errors.Is(accountErr, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if accountErr != nil {
			return nil, accountErr
		}
		if !account.PoolID.Valid || account.Kind == string(model.AccountKindCard) {
			return nil, fmt.Errorf("%w: transfer destination must be a non-card account assigned to a pool", ErrBadInput)
		}
		pool, poolErr := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Pools.Columns.ID.EQ(psql.Arg(account.PoolID.V)),
		))).One(ctx, executor)
		if poolErr != nil {
			return nil, poolErr
		}
		if pool.Kind == string(model.PoolKindPersonal) {
			fundingRule, ruleErr := models.PoolFundingRules.Query(sm.Where(psql.And(
				models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
				models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(pool.ID)),
				models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
			))).One(ctx, executor)
			if ruleErr != nil {
				return nil, ruleErr
			}
			toMemberID, parseErr := gofrsuuid.FromString(input.ToMemberID)
			if parseErr != nil || !fundingRule.OwnerMemberID.Valid || fundingRule.OwnerMemberID.V != toMemberID {
				return nil, fmt.Errorf("%w: personal destination account must belong to the receiving member", ErrBadInput)
			}
		}
		destinationAccountUUID = &destinationID
	}
	if paymentAccountUUID != nil && destinationAccountUUID != nil && *paymentAccountUUID == *destinationAccountUUID {
		return nil, fmt.Errorf("%w: transfer source and destination accounts must differ", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	transactionExecutor := bobpgx.NewTx(tx, nil)
	householdID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	key := input.IdempotencyKey
	operation := "recordTransfer"
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(key)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, transactionExecutor)
	if err == nil {
		if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
			return nil, err
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		return loadLedgerEntry(ctx, r.DB, existing.ResourceID.V.String())
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	period := accountingPeriod(occurredAt)
	periodStatusValue := "OPEN"
	periodRevision := int64(0)
	if _, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdID, Period: &period, Status: &periodStatusValue, Revision: &periodRevision,
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, transactionExecutor)
	if err != nil {
		return nil, err
	}
	if periodRow.Status == "CLOSED" {
		return nil, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}
	var loanID *gofrsuuid.UUID
	if input.Kind == model.TransferKindRepayment {
		loanUUID, parseErr := gofrsuuid.FromString(*input.LoanID)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid loan id", ErrBadInput)
		}
		loan, err := models.Transfers.Query(sm.Where(psql.And(
			models.Transfers.Columns.ID.EQ(psql.Arg(loanUUID)),
			models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Transfers.Columns.Kind.EQ(psql.Arg("LOAN")),
		)), sm.ForUpdate()).One(ctx, transactionExecutor)
		if err != nil {
			return nil, ErrNotFound
		}
		fromMemberID, fromErr := gofrsuuid.FromString(input.FromMemberID)
		toMemberID, toErr := gofrsuuid.FromString(input.ToMemberID)
		if fromErr != nil || toErr != nil {
			return nil, fmt.Errorf("%w: invalid transfer member id", ErrBadInput)
		}
		if fromMemberID != loan.ToMemberID || toMemberID != loan.FromMemberID {
			return nil, fmt.Errorf("%w: repayment direction does not match the loan", ErrBadInput)
		}
		repayments, err := models.Transfers.Query(sm.Where(psql.And(
			models.Transfers.Columns.LoanID.EQ(psql.Arg(loanUUID)),
			models.Transfers.Columns.Kind.EQ(psql.Arg("REPAYMENT")),
		))).All(ctx, transactionExecutor)
		if err != nil {
			return nil, err
		}
		var repaid int64
		for _, repayment := range repayments {
			var ok bool
			repaid, ok = checkedAdd(repaid, repayment.AmountYen)
			if !ok {
				return nil, fmt.Errorf("%w: repayment total is too large", ErrBadInput)
			}
		}
		if int64(input.AmountYen) > loan.AmountYen-repaid {
			return nil, fmt.Errorf("%w: repayment exceeds the outstanding loan", ErrBadInput)
		}
		loanID = &loanUUID
	}
	transferID, journalID, ledgerID := newID(), newID(), newID()
	transferUUID, _ := gofrsuuid.FromString(transferID)
	journalUUID, _ := gofrsuuid.FromString(journalID)
	ledgerUUID, _ := gofrsuuid.FromString(ledgerID)
	actorUUID, err := gofrsuuid.FromString(user.MemberID)
	if err != nil {
		return nil, err
	}
	sourceKind := "TRANSFER"
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalUUID, HouseholdID: &householdID, SourceKind: &sourceKind, SourceID: &transferUUID,
		IdempotencyKey: &key, OccurredAt: &occurredAt, CreatedBy: &actorUUID,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	amount := int64(input.AmountYen)
	fromMemberUUID, err := gofrsuuid.FromString(input.FromMemberID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid source member id", ErrBadInput)
	}
	toMemberUUID, err := gofrsuuid.FromString(input.ToMemberID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid destination member id", ErrBadInput)
	}
	lineOne, lineTwo := int32(1), int32(2)
	kindMemo := string(input.Kind)
	noAccount, noPool := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
	fromMember, toMember := sql.Null[gofrsuuid.UUID]{V: fromMemberUUID, Valid: true}, sql.Null[gofrsuuid.UUID]{V: toMemberUUID, Valid: true}
	zero := int64(0)
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalUUID, LineNo: &lineOne, AccountID: &noAccount, PoolID: &noPool, MemberID: &toMember,
		DebitYen: &amount, CreditYen: &zero, Memo: &kindMemo,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalUUID, LineNo: &lineTwo, AccountID: &noAccount, PoolID: &noPool, MemberID: &fromMember,
		DebitYen: &zero, CreditYen: &amount, Memo: &kindMemo,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if input.PaymentAccountID != nil || input.DestinationAccountID != nil {
		if input.PaymentAccountID != nil && paymentPoolID == nil {
			return nil, fmt.Errorf("%w: invalid payment account", ErrBadInput)
		}
		line := int32(3)
		if input.PaymentAccountID != nil {
			poolValue := sql.Null[gofrsuuid.UUID]{V: *paymentPoolID, Valid: true}
			debitMemo := "Transfer settlement offset"
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &line, AccountID: &noAccount, PoolID: &poolValue, MemberID: &noPool,
				DebitYen: &zero, CreditYen: &amount, Memo: new("Transfer settlement"),
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
			line++
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &line, AccountID: &noAccount, PoolID: &noPool, MemberID: &noPool,
				DebitYen: &amount, CreditYen: &zero, Memo: &debitMemo,
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
			line++
		}
		if paymentAccountUUID != nil {
			accountValue := sql.Null[gofrsuuid.UUID]{V: *paymentAccountUUID, Valid: true}
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &line, AccountID: &accountValue, PoolID: &noPool, MemberID: &noPool,
				DebitYen: &zero, CreditYen: &amount, Memo: new("Transfer source account"),
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
			line++
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &line, AccountID: &noAccount, PoolID: &noPool, MemberID: &noPool,
				DebitYen: &amount, CreditYen: &zero, Memo: new("Transfer source account offset"),
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
			line++
		}
		if destinationAccountUUID != nil {
			accountValue := sql.Null[gofrsuuid.UUID]{V: *destinationAccountUUID, Valid: true}
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &line, AccountID: &accountValue, PoolID: &noPool, MemberID: &noPool,
				DebitYen: &amount, CreditYen: &zero, Memo: new("Transfer destination account"),
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
			line++
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &line, AccountID: &noAccount, PoolID: &noPool, MemberID: &noPool,
				DebitYen: &zero, CreditYen: &amount, Memo: new("Transfer destination account offset"),
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
		}
	}
	description := strings.TrimSpace(input.Description)
	if description == "" {
		description = string(input.Kind)
	}
	transferLedgerKind := "TRANSFER"
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{
		ID: &ledgerUUID, HouseholdID: &householdID, JournalID: &journalUUID, Kind: &transferLedgerKind,
		OccurredAt: &occurredAt, Description: &description, AmountYen: &amount,
		FromMemberID: &fromMember, ToMemberID: &toMember,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	transferKind := string(input.Kind)
	loanValue := sql.Null[gofrsuuid.UUID]{}
	if loanID != nil {
		loanValue = sql.Null[gofrsuuid.UUID]{V: *loanID, Valid: true}
	}
	paymentAccountValue := sql.Null[gofrsuuid.UUID]{}
	if input.PaymentAccountID != nil {
		paymentAccountValue = sql.Null[gofrsuuid.UUID]{V: *paymentAccountUUID, Valid: true}
	}
	if _, err := models.Transfers.Insert(&models.TransferSetter{
		ID: &transferUUID, HouseholdID: &householdID, LedgerEntryID: &ledgerUUID,
		FromMemberID: &fromMemberUUID, ToMemberID: &toMemberUUID, Kind: &transferKind,
		AmountYen: &amount, Description: &description, LoanID: &loanValue, PaymentAccountID: &paymentAccountValue,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if input.Kind == model.TransferKindAdvance || input.Kind == model.TransferKindLoan || input.Kind == model.TransferKindRepayment {
		adjustments := []struct {
			memberID string
			amount   int64
		}{{memberID: input.FromMemberID, amount: -amount}, {memberID: input.ToMemberID, amount: amount}}
		if input.Kind == model.TransferKindRepayment && paymentPoolKind == string(model.PoolKindShared) {
			adjustments = []struct {
				memberID string
				amount   int64
			}{{memberID: input.ToMemberID, amount: amount}}
		}
		for _, adjustment := range adjustments {
			adjustmentID, _ := gofrsuuid.FromString(newID())
			memberUUID, parseErr := gofrsuuid.FromString(adjustment.memberID)
			if parseErr != nil {
				return nil, parseErr
			}
			if _, err := models.SettlementAdjustments.Insert(&models.SettlementAdjustmentSetter{
				ID: &adjustmentID, HouseholdID: &householdID, Period: &period, MemberID: &memberUUID,
				SourceKind: &transferKind, SourceID: &transferUUID, AmountYen: &adjustment.amount,
			}).Exec(ctx, transactionExecutor); err != nil {
				return nil, err
			}
		}
	}
	ledgerResource := sql.Null[gofrsuuid.UUID]{V: ledgerUUID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdID, Key: &key, Operation: &operation, ResourceID: &ledgerResource,
		RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, transactionExecutor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "transfer.recorded", ledgerID, 1, []string{"amountYen", "kind"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadLedgerEntry(ctx, r.DB, ledgerID)
}

func (r *mutationResolver) setFundingSchedule(ctx context.Context, input model.SetFundingScheduleInput) (bool, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return false, err
	}
	if input.AmountYen < 0 || int64(input.AmountYen) > maxMoney || input.Cadence != "MONTHLY" {
		return false, fmt.Errorf("%w: funding schedules currently require MONTHLY cadence", ErrBadInput)
	}
	start, err := parseDate(input.StartsOn)
	if err != nil {
		return false, err
	}
	var end sql.Null[time.Time]
	if input.EndsOn != nil {
		parsed, err := parseDate(*input.EndsOn)
		if err != nil {
			return false, err
		}
		if parsed.Before(start) {
			return false, fmt.Errorf("%w: schedule end precedes start", ErrBadInput)
		}
		end = sql.Null[time.Time]{V: parsed, Valid: true}
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.MemberID); err != nil {
		return false, err
	}
	householdUUID, poolUUID := generatedUUID(input.HouseholdID), generatedUUID(input.PoolID)
	if _, err := models.Pools.Query(sm.Where(psql.And(
		models.Pools.Columns.ID.EQ(psql.Arg(poolUUID)),
		models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
	))).One(ctx, bobpgx.NewPool(r.DB)); errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	} else if err != nil {
		return false, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	} else if err != nil {
		return false, err
	}
	memberUUID := generatedUUID(input.MemberID)
	priorDay := start.AddDate(0, 0, -1)
	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.FundingSchedules.Columns.MemberID.EQ(psql.Arg(memberUUID)),
		models.FundingSchedules.Columns.PoolID.EQ(psql.Arg(poolUUID)),
		models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
		models.FundingSchedules.Columns.StartsOn.LT(psql.Arg(start)),
	))).All(ctx, executor)
	if err != nil {
		return false, err
	}
	for _, schedule := range schedules {
		endDate := priorDay
		if schedule.EndsOn.Valid && schedule.EndsOn.V.Before(endDate) {
			endDate = schedule.EndsOn.V
		}
		endValue := sql.Null[time.Time]{V: endDate, Valid: true}
		active := false
		if _, err := models.FundingSchedules.Update((&models.FundingScheduleSetter{EndsOn: &endValue, Active: &active}).UpdateMod(), um.Where(models.FundingSchedules.Columns.ID.EQ(psql.Arg(schedule.ID)))).Exec(ctx, executor); err != nil {
			return false, err
		}
	}
	activeSchedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.FundingSchedules.Columns.MemberID.EQ(psql.Arg(memberUUID)),
		models.FundingSchedules.Columns.PoolID.EQ(psql.Arg(poolUUID)),
		models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
	))).All(ctx, executor)
	if err != nil {
		return false, err
	}
	active := false
	for _, schedule := range activeSchedules {
		if _, err := models.FundingSchedules.Update((&models.FundingScheduleSetter{Active: &active}).UpdateMod(), um.Where(models.FundingSchedules.Columns.ID.EQ(psql.Arg(schedule.ID)))).Exec(ctx, executor); err != nil {
			return false, err
		}
	}
	scheduleID := newID()
	scheduleUUID := generatedUUID(scheduleID)
	amountYen := int64(input.AmountYen)
	cadence := input.Cadence
	if _, err := models.FundingSchedules.Insert(&models.FundingScheduleSetter{
		ID: &scheduleUUID, HouseholdID: &householdUUID, MemberID: &memberUUID, PoolID: &poolUUID,
		AmountYen: &amountYen, Cadence: &cadence, StartsOn: &start, EndsOn: &end,
	}).Exec(ctx, executor); err != nil {
		return false, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "funding.schedule.updated", scheduleID, 1, []string{"amountYen", "startsOn", "endsOn"}, user.MemberID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *mutationResolver) closePeriod(ctx context.Context, input model.ClosePeriodInput) (*model.Settlement, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	periodStart, err := parseAccountingPeriod(input.Period)
	if err != nil {
		return nil, fmt.Errorf("%w: period must use YYYY-MM", ErrBadInput)
	}
	periodEnd := periodStart.AddDate(0, 1, 0)
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdUUID := generatedUUID(input.HouseholdID)
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	openStatus, zeroRevision := "OPEN", int64(0)
	if _, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{
		HouseholdID: &householdUUID, Period: &input.Period, Status: &openStatus, Revision: &zeroRevision,
	}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, executor); err != nil {
		return nil, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(input.Period)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if periodRow.Status == "CLOSED" {
		return nil, ErrConflict
	}
	if periodRow.Revision != int64(input.ExpectedRevision) {
		return nil, ErrConflict
	}
	purchases, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Purchases.Columns.Status.EQ(psql.Arg(string(model.PurchaseStatusDraft))),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(periodStart)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(periodEnd)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	if len(purchases) != 0 {
		return nil, fmt.Errorf("%w: post or remove draft purchases before closing the period", ErrConflict)
	}
	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		psql.Or(
			models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
			models.FundingSchedules.Columns.EndsOn.IsNotNull(),
		),
		models.FundingSchedules.Columns.Cadence.EQ(psql.Arg("MONTHLY")),
		models.FundingSchedules.Columns.StartsOn.LT(psql.Arg(periodEnd)),
		psql.Or(
			models.FundingSchedules.Columns.EndsOn.IsNull(),
			models.FundingSchedules.Columns.EndsOn.GTE(psql.Arg(periodStart)),
		),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, item := range schedules {
		scheduleID := sql.Null[gofrsuuid.UUID]{V: item.ID, Valid: true}
		if _, err := models.FundingObligations.Insert(&models.FundingObligationSetter{
			ID: func() *gofrsuuid.UUID { id := generatedUUID(newID()); return &id }(), HouseholdID: &householdUUID, ScheduleID: &scheduleID,
			MemberID: &item.MemberID, PoolID: &item.PoolID, Period: &input.Period, AmountYen: &item.AmountYen,
		}, im.OnConflict("household_id", "member_id", "pool_id", "period").DoUpdate(
			im.SetCol("schedule_id").To(im.Excluded("schedule_id")),
			im.SetCol("amount_yen").To(im.Excluded("amount_yen")),
		)).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	rules, err := poolFundingRules(ctx, executor, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	lines, err := calculateSettlementPreviewWithRules(ctx, executor, input.HouseholdID, input.Period)
	if err != nil {
		return nil, err
	}
	settlementID := newID()
	newRevision := periodRow.Revision + 1
	fundingSnapshot, err := json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	settlementUUID, userUUID := generatedUUID(settlementID), generatedUUID(user.MemberID)
	closedStatus := "CLOSED"
	snapshot := types.JSON[stdjson.RawMessage]{Val: fundingSnapshot}
	if _, err := models.SettlementRuns.Insert(&models.SettlementRunSetter{
		ID: &settlementUUID, HouseholdID: &householdUUID, Period: &input.Period, Revision: &newRevision,
		Status: &closedStatus, CreatedBy: &userUUID, FundingSnapshot: &snapshot,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	for _, line := range lines {
		memberUUID := generatedUUID(line.MemberID)
		scheduled, paid := int64(line.ScheduledYen), int64(line.PaidYen)
		carry, monthlyAdjustment := int64(line.CarryYen), int64(line.MonthlyAdjustmentYen)
		advanceCredit, amountDue := int64(line.AdvanceCreditYen), int64(line.AmountDueYen)
		if _, err := models.SettlementLines.Insert(&models.SettlementLineSetter{
			SettlementRunID: &settlementUUID, MemberID: &memberUUID, ScheduledYen: &scheduled, PaidYen: &paid,
			CarryYen: &carry, MonthlyAdjustmentYen: &monthlyAdjustment, AdvanceCreditYen: &advanceCredit, AmountDueYen: &amountDue,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	postedStatus := string(model.PurchaseStatusPosted)
	closedPurchaseStatus := string(model.PurchaseStatusClosed)
	updatedAt := time.Now()
	postedPurchases, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Purchases.Columns.Status.EQ(psql.Arg(postedStatus)),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(periodStart)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(periodEnd)),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, purchase := range postedPurchases {
		revision := purchase.Revision + 1
		if _, err := models.Purchases.Update((&models.PurchaseSetter{Status: &closedPurchaseStatus, Revision: &revision, UpdatedAt: &updatedAt}).UpdateMod(), um.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchase.ID)))).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	closedBy := sql.Null[gofrsuuid.UUID]{V: userUUID, Valid: true}
	closedAt := sql.Null[time.Time]{V: updatedAt, Valid: true}
	if _, err := models.AccountingPeriods.Update((&models.AccountingPeriodSetter{
		Status: &closedStatus, Revision: &newRevision, ClosedBy: &closedBy, ClosedAt: &closedAt,
	}).UpdateMod(), um.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(input.Period)),
	))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "period.closed", settlementID, newRevision, []string{"status"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return (&queryResolver{r.Resolver}).settlement(ctx, input.HouseholdID, input.Period)
}

func (r *mutationResolver) correctPurchase(ctx context.Context, input model.CorrectPurchaseInput) (*model.Purchase, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	delta := int64(input.AmountDeltaYen)
	if delta == 0 || delta > maxMoney || delta < -maxMoney || strings.TrimSpace(input.Reason) == "" || input.IdempotencyKey == "" {
		return nil, fmt.Errorf("%w: invalid correction", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	householdUUID, purchaseUUID := generatedUUID(input.HouseholdID), generatedUUID(input.PurchaseID)
	purchaseRow, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.ID.EQ(psql.Arg(purchaseUUID)),
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	operation := "correctPurchase"
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
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
		return loadPurchase(ctx, r.DB, input.PurchaseID)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if purchaseRow.Status == string(model.PurchaseStatusDraft) {
		return nil, fmt.Errorf("%w: corrections require a posted purchase", ErrBadInput)
	}
	originalPeriod := accountingPeriod(purchaseRow.OccurredAt)
	if err := ensureAccountingPeriod(ctx, executor, householdUUID, originalPeriod); err != nil {
		return nil, err
	}
	originalPeriodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(originalPeriod)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	effectivePeriod := originalPeriod
	correctionAt := time.Now()
	if originalPeriodRow.Status == "CLOSED" {
		effectivePeriod = accountingPeriod(correctionAt)
		if err := lockAccountingPeriods(ctx, tx, input.HouseholdID, effectivePeriod); err != nil {
			return nil, err
		}
	}
	if originalPeriodRow.Status != "OPEN" && originalPeriodRow.Status != "CLOSED" {
		return nil, fmt.Errorf("%w: invalid accounting period state", ErrConflict)
	}
	effectivePeriodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(effectivePeriod)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if effectivePeriodRow.Status == "CLOSED" {
		return nil, fmt.Errorf("%w: correction period is closed", ErrConflict)
	}
	var itemUUIDValue sql.Null[gofrsuuid.UUID]
	if input.ItemID != nil {
		itemUUID := generatedUUID(*input.ItemID)
		if _, err := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)),
			models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
		))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
		itemUUIDValue = sql.Null[gofrsuuid.UUID]{V: itemUUID, Valid: true}
	}
	weights, err := correctionAllocationWeights(ctx, tx, input.PurchaseID, input.ItemID)
	if err != nil {
		return nil, err
	}
	if len(input.Allocations) > 0 && !sameAllocationWeights(input.Allocations, weights) {
		return nil, fmt.Errorf("%w: corrections must use the saved purchase allocation", ErrBadInput)
	}
	if delta < 0 {
		var refundable int64
		if input.ItemID != nil {
			itemUUID := generatedUUID(*input.ItemID)
			item, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)))).One(ctx, executor)
			if err != nil {
				return nil, err
			}
			refundable = item.AmountYen
			corrections, err := models.Corrections.Query(sm.Where(psql.And(
				models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
				models.Corrections.Columns.ItemID.EQ(psql.Arg(sql.Null[gofrsuuid.UUID]{V: itemUUID, Valid: true})),
			))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, correction := range corrections {
				refundable += correction.AmountDeltaYen
			}
		} else {
			items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				refundable += item.AmountYen
			}
			corrections, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
			if err != nil {
				return nil, err
			}
			for _, correction := range corrections {
				refundable += correction.AmountDeltaYen
			}
		}
		if -delta > refundable {
			return nil, fmt.Errorf("%w: refund exceeds the remaining purchase amount", ErrBadInput)
		}
	}
	shares, err := domain.Allocate(abs(delta), weights)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	correctionID, journalID, ledgerID := newID(), newID(), newID()
	correctionUUID, journalUUID, ledgerUUID := generatedUUID(correctionID), generatedUUID(journalID), generatedUUID(ledgerID)
	userUUID := generatedUUID(user.MemberID)
	sourceKind := "CORRECTION"
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalUUID, HouseholdID: &householdUUID, SourceKind: &sourceKind, SourceID: &correctionUUID,
		IdempotencyKey: &input.IdempotencyKey, OccurredAt: &correctionAt, CreatedBy: &userUUID,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	lineNo := int32(1)
	noAccount, noPool := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
	for _, share := range shares {
		if share.Amount == 0 {
			continue
		}
		if delta > 0 {
			poolUUID := generatedUUID(share.PoolID)
			zero := int64(0)
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &lineNo, AccountID: &noAccount,
				PoolID: &sql.Null[gofrsuuid.UUID]{V: poolUUID, Valid: true}, MemberID: &noPool,
				DebitYen: &share.Amount, CreditYen: &zero, Memo: &input.Reason,
			}).Exec(ctx, executor); err != nil {
				return nil, err
			}
		} else {
			poolUUID := generatedUUID(share.PoolID)
			zero := int64(0)
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &lineNo, AccountID: &noAccount,
				PoolID: &sql.Null[gofrsuuid.UUID]{V: poolUUID, Valid: true}, MemberID: &noPool,
				DebitYen: &zero, CreditYen: &share.Amount, Memo: &input.Reason,
			}).Exec(ctx, executor); err != nil {
				return nil, err
			}
		}
		lineNo++
	}
	var payments []model.PurchasePaymentInput
	if len(input.PaymentAllocations) > 0 {
		payments, err = validatePurchasePaymentInputs(ctx, tx, input.HouseholdID, input.PaymentAllocations)
		if err != nil {
			return nil, err
		}
		var paid int64
		for _, payment := range payments {
			var ok bool
			paid, ok = checkedAdd(paid, int64(payment.AmountYen))
			if !ok {
				return nil, fmt.Errorf("%w: payment total is too large", ErrBadInput)
			}
		}
		if paid != abs(delta) {
			return nil, fmt.Errorf("%w: correction payments must equal the correction amount", ErrBadInput)
		}
	} else {
		items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, executor)
		if err != nil {
			return nil, err
		}
		var originalTotal int64
		for _, item := range items {
			originalTotal += item.AmountYen
		}
		var originalAccountID *string
		if purchaseRow.PaymentAccountID.Valid {
			accountID := purchaseRow.PaymentAccountID.V.String()
			originalAccountID = &accountID
		}
		payments, err = purchasePayments(ctx, tx, input.HouseholdID, input.PurchaseID, originalAccountID, originalTotal)
		if err != nil {
			return nil, err
		}
		if len(payments) > 0 {
			weights := make([]domain.Weight, 0, len(payments))
			for _, payment := range payments {
				weights = append(weights, domain.Weight{PoolID: payment.PaymentAccountID, Value: int64(payment.AmountYen)})
			}
			allocatedPayments, err := domain.Allocate(abs(delta), weights)
			if err != nil {
				return nil, err
			}
			payments = payments[:0]
			for _, payment := range allocatedPayments {
				if payment.Amount > 0 {
					payments = append(payments, model.PurchasePaymentInput{PaymentAccountID: payment.PoolID, AmountYen: graphLong(payment.Amount)})
				}
			}
		}
	}
	if len(payments) == 0 {
		memo := "Correction offset"
		zero := int64(0)
		noMember := sql.Null[gofrsuuid.UUID]{}
		if delta > 0 {
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &lineNo, AccountID: &noAccount, PoolID: &noPool, MemberID: &noMember,
				DebitYen: &zero, CreditYen: &delta, Memo: &memo,
			}).Exec(ctx, executor); err != nil {
				return nil, err
			}
		} else {
			debit := -delta
			if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
				JournalID: &journalUUID, LineNo: &lineNo, AccountID: &noAccount, PoolID: &noPool, MemberID: &noMember,
				DebitYen: &debit, CreditYen: &zero, Memo: &memo,
			}).Exec(ctx, executor); err != nil {
				return nil, err
			}
		}
	} else {
		for _, payment := range payments {
			accountUUID := generatedUUID(payment.PaymentAccountID)
			noPool, noMember := sql.Null[gofrsuuid.UUID]{}, sql.Null[gofrsuuid.UUID]{}
			zero := int64(0)
			memo := "Correction payment"
			paymentAmount := int64(payment.AmountYen)
			setter := &models.JournalEntrySetter{JournalID: &journalUUID, LineNo: &lineNo,
				AccountID: &sql.Null[gofrsuuid.UUID]{V: accountUUID, Valid: true}, PoolID: &noPool, MemberID: &noMember,
				DebitYen: &zero, CreditYen: &zero, Memo: &memo}
			if delta > 0 {
				setter.CreditYen = &paymentAmount
			} else {
				setter.DebitYen = &paymentAmount
			}
			if _, err := models.JournalEntries.Insert(setter).Exec(ctx, executor); err != nil {
				return nil, err
			}
			lineNo++
		}
	}
	correctionLedgerKind := "CORRECTION"
	noMemberValue := sql.Null[gofrsuuid.UUID]{}
	purchaseValue := sql.Null[gofrsuuid.UUID]{V: purchaseUUID, Valid: true}
	amount := abs(delta)
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{
		ID: &ledgerUUID, HouseholdID: &householdUUID, JournalID: &journalUUID, Kind: &correctionLedgerKind,
		OccurredAt: &correctionAt, Description: &input.Reason, AmountYen: &amount,
		FromMemberID: &noMemberValue, ToMemberID: &noMemberValue, PoolID: &noMemberValue, SourcePurchaseID: &purchaseValue,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if _, err := models.Corrections.Insert(&models.CorrectionSetter{
		ID: &correctionUUID, HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, ItemID: &itemUUIDValue,
		AmountDeltaYen: &delta, Reason: &input.Reason, JournalID: &journalUUID, CreatedBy: &userUUID,
		EffectivePeriod: &sql.Null[string]{V: effectivePeriod, Valid: true},
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := snapshotCorrectionMemberBurden(ctx, executor, input.HouseholdID, correctionUUID, purchaseUUID, itemUUIDValue, shares, delta); err != nil {
		return nil, err
	}
	for index, payment := range payments {
		accountUUID := generatedUUID(payment.PaymentAccountID)
		line := int32(index + 1)
		paymentAmount := int64(payment.AmountYen)
		if _, err := models.CorrectionPayments.Insert(&models.CorrectionPaymentSetter{
			CorrectionID: &correctionUUID, LineNo: &line, AccountID: &accountUUID, AmountYen: &paymentAmount,
		}).Exec(ctx, executor); err != nil {
			return nil, err
		}
	}
	if len(payments) > 0 {
		correctionItems := []domain.FundingExpenseItem{{Amount: abs(delta)}}
		for _, share := range shares {
			correctionItems[0].Allocations = append(correctionItems[0].Allocations, domain.PoolAmount{PoolID: share.PoolID, Amount: share.Amount})
		}
		fundingPayments := make([]domain.FundingPayment, 0, len(payments))
		for _, payment := range payments {
			accountUUID := generatedUUID(payment.PaymentAccountID)
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
			if account.PoolID.Valid {
				fundingPayments = append(fundingPayments, domain.FundingPayment{ID: payment.PaymentAccountID, PoolID: account.PoolID.V.String(), Amount: int64(payment.AmountYen)})
			}
		}
		if len(fundingPayments) == len(payments) {
			fundingExpenses, err := domain.SplitFundingExpense(correctionItems, fundingPayments)
			if err != nil {
				return nil, err
			}
			rules, err := poolFundingRules(ctx, bobpgx.NewTx(tx, nil), input.HouseholdID)
			if err != nil {
				return nil, err
			}
			changes, err := domain.CalculateExpenseAdjustments(fundingExpenses, rules)
			if err != nil {
				return nil, err
			}
			for _, change := range changes {
				amount := change.MonthlyAdjustmentYen - change.AdvanceCreditYen
				if delta < 0 {
					amount = -amount
				}
				if amount == 0 {
					continue
				}
				adjustmentID, memberUUID := generatedUUID(newID()), generatedUUID(change.MemberID)
				if _, err := models.SettlementAdjustments.Insert(&models.SettlementAdjustmentSetter{
					ID: &adjustmentID, HouseholdID: &householdUUID, Period: &effectivePeriod, MemberID: &memberUUID,
					SourceKind: &sourceKind, SourceID: &correctionUUID, AmountYen: &amount,
				}).Exec(ctx, executor); err != nil {
					return nil, err
				}
			}
		}
	}
	ledgerResource := sql.Null[gofrsuuid.UUID]{V: ledgerUUID, Valid: true}
	requestFingerprint := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdUUID, Key: &input.IdempotencyKey, Operation: &operation, ResourceID: &ledgerResource, RequestFingerprint: &requestFingerprint,
	}).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "purchase.corrected", input.PurchaseID, 1, []string{"correction"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, input.PurchaseID)
}

func abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func mutationRequestFingerprint(input any) ([sha256.Size]byte, error) {
	encoded, err := stdjson.Marshal(input)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func validateIdempotencyFingerprint(existing *models.IdempotencyKey, fingerprint [sha256.Size]byte) error {
	if !existing.RequestFingerprint.Valid {
		return fmt.Errorf("%w: idempotency key has no request fingerprint", ErrConflict)
	}
	if !slices.Equal(existing.RequestFingerprint.V, fingerprint[:]) {
		return fmt.Errorf("%w: idempotency key was used with a different request", ErrConflict)
	}
	return nil
}

func writeEvent(ctx context.Context, tx pgx.Tx, householdID, eventType, entityID string, revision int64, fields []string, actorID string) error {
	encoded, err := stdjson.Marshal(fields)
	if err != nil {
		return err
	}
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return fmt.Errorf("parse event household id: %w", err)
	}
	entityUUID, err := gofrsuuid.FromString(entityID)
	if err != nil {
		return fmt.Errorf("parse event entity id: %w", err)
	}
	actorUUID, err := gofrsuuid.FromString(actorID)
	if err != nil {
		return fmt.Errorf("parse event actor id: %w", err)
	}
	changedFields := types.JSON[stdjson.RawMessage]{Val: encoded}
	actor := sql.Null[gofrsuuid.UUID]{V: actorUUID, Valid: true}
	_, err = models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
		HouseholdID: &householdUUID, Type: &eventType, EntityID: &entityUUID, Revision: &revision,
		ChangedFields: &changedFields, ActorID: &actor,
	}).Exec(ctx, bobpgx.NewTx(tx, nil))
	return err
}

func generatedUUID(value string) gofrsuuid.UUID {
	id, _ := gofrsuuid.FromString(value)
	return id
}

func setFieldRevisions(current []byte, fields []string, revision int64) ([]byte, error) {
	var revisions map[string]int64
	if len(current) == 0 {
		revisions = make(map[string]int64)
	} else if err := json.Unmarshal(current, &revisions); err != nil {
		return nil, err
	}
	for _, field := range fields {
		revisions[field] = revision
	}
	return json.Marshal(revisions)
}

func validateChangedFields(fields []string, allowed ...string) error {
	if len(fields) == 0 {
		return fmt.Errorf("%w: changedFields is required", ErrBadInput)
	}
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !slices.Contains(allowed, field) {
			return fmt.Errorf("%w: invalid changed field", ErrBadInput)
		}
		if _, exists := seen[field]; exists {
			return fmt.Errorf("%w: duplicate changed field", ErrBadInput)
		}
		seen[field] = struct{}{}
	}
	return nil
}

func valuesConflict(fieldRevisions []byte, base int, fields []string) (bool, error) {
	if base < 1 {
		return true, nil
	}
	var revisions map[string]int64
	if len(fieldRevisions) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(fieldRevisions, &revisions); err != nil {
		return false, err
	}
	for _, field := range fields {
		if revisions[field] > int64(base) {
			return true, nil
		}
	}
	return false, nil
}

func parseDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: date must use YYYY-MM-DD", ErrBadInput)
	}
	return parsed, nil
}
