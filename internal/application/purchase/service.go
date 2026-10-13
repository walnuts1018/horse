package purchase

import (
	"context"
	"crypto/sha256"
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

var (
	ErrBadInput                    = apperror.ErrBadInput
	ErrConflict                    = apperror.ErrConflict
	ErrNotFound                    = apperror.ErrNotFound
	ErrUnattributedPurchasePayment = errors.New("posted purchase payment has no funding pool")
)

const maxMoney int64 = 1_000_000_000_000
const maxQuantityNumerator int64 = 1_000_000_000_000
const maxQuantityDenominator int64 = 1_000_000
const manualOrigin = "MANUAL"

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

func postRequestFingerprint(purchaseID string, expectedRevision int64) ([sha256.Size]byte, error) {
	request := struct {
		PurchaseID       string
		ExpectedRevision int64
	}{PurchaseID: purchaseID, ExpectedRevision: expectedRevision}
	encoded, err := stdjson.Marshal(request)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func (s *Service) Create(ctx context.Context, command CreateCommand, fingerprint [sha256.Size]byte) (CreateResult, error) {
	if err := validateCreateCommand(command); err != nil {
		return CreateResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreateResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	result, err := s.create(ctx, tx, command, fingerprint)
	if err != nil {
		return CreateResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreateResult{}, err
	}
	return result, nil
}

func (s *Service) Post(ctx context.Context, command PostCommand, fingerprint [sha256.Size]byte) (PostResult, error) {
	if command.IdempotencyKey == "" || len(command.IdempotencyKey) > 200 {
		return PostResult{}, fmt.Errorf("%w: idempotency key is required", ErrBadInput)
	}
	if _, err := parseUUID(command.PurchaseID, "purchase"); err != nil {
		return PostResult{}, err
	}
	if _, err := parseUUID(command.ActorID, "actor"); err != nil {
		return PostResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return PostResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	result, err := s.post(ctx, tx, command, fingerprint, true)
	if err != nil {
		return PostResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PostResult{}, err
	}
	return result, nil
}

func (s *Service) CreateAndPost(ctx context.Context, command CreateCommand, fingerprint [sha256.Size]byte) (CreateResult, error) {
	if err := validateCreateCommand(command); err != nil {
		return CreateResult{}, err
	}
	householdID, err := parseUUID(command.HouseholdID, "household")
	if err != nil {
		return CreateResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreateResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CreateResult{}, ErrNotFound
		}
		return CreateResult{}, err
	}
	created, err := s.create(ctx, tx, command, fingerprint)
	if err != nil {
		return CreateResult{}, err
	}
	purchaseID, err := parseUUID(created.PurchaseID, "purchase")
	if err != nil {
		return CreateResult{}, err
	}
	purchaseRow, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(purchaseID)))).One(ctx, executor)
	if err != nil {
		return CreateResult{}, err
	}
	if purchaseRow.Status == draftStatus {
		postCommand := PostCommand{
			ActorID: command.ActorID, PurchaseID: created.PurchaseID, ExpectedRevision: purchaseRow.Revision,
			IdempotencyKey: command.IdempotencyKey,
		}
		postFingerprint, err := postRequestFingerprint(postCommand.PurchaseID, postCommand.ExpectedRevision)
		if err != nil {
			return CreateResult{}, err
		}
		if _, err := s.post(ctx, tx, postCommand, postFingerprint, false); err != nil {
			return CreateResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CreateResult{}, err
	}
	return created, nil
}

func validateCreateCommand(input CreateCommand) error {
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 || len(input.Items) == 0 || len(input.Items) > 200 {
		return fmt.Errorf("%w: invalid purchase", ErrBadInput)
	}
	if input.SourceReference != nil && (input.SourceDocumentID == nil || len(*input.SourceReference) > 256 || strings.ContainsAny(*input.SourceReference, "\r\n")) {
		return fmt.Errorf("%w: source reference requires a document and must be valid", ErrBadInput)
	}
	origin := input.Origin
	if origin == "" {
		origin = manualOrigin
		if input.SourceDocumentID != nil {
			origin = "AI"
		}
	}
	if origin != "AI" && origin != manualOrigin && origin != "UNKNOWN" {
		return fmt.Errorf("%w: invalid purchase origin", ErrBadInput)
	}
	if len(strings.TrimSpace(input.Merchant)) > 240 {
		return fmt.Errorf("%w: merchant name is too long", ErrBadInput)
	}
	if _, err := parseUUID(input.HouseholdID, "household"); err != nil {
		return err
	}
	if _, err := parseUUID(input.ActorID, "actor"); err != nil {
		return err
	}
	return nil
}

func (s *Service) create(ctx context.Context, tx pgx.Tx, input CreateCommand, fingerprint [sha256.Size]byte) (CreateResult, error) {
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 || len(input.Items) == 0 || len(input.Items) > 200 {
		return CreateResult{}, fmt.Errorf("%w: invalid purchase", ErrBadInput)
	}
	if input.SourceReference != nil && (input.SourceDocumentID == nil || len(*input.SourceReference) > 256 || strings.ContainsAny(*input.SourceReference, "\r\n")) {
		return CreateResult{}, fmt.Errorf("%w: source reference requires a document and must be valid", ErrBadInput)
	}
	origin := input.Origin
	if origin == "" {
		origin = manualOrigin
		if input.SourceDocumentID != nil {
			origin = "AI"
		}
	}
	if origin != "AI" && origin != manualOrigin && origin != "UNKNOWN" {
		return CreateResult{}, fmt.Errorf("%w: invalid purchase origin", ErrBadInput)
	}
	merchant := strings.TrimSpace(input.Merchant)
	if len(merchant) > 240 {
		return CreateResult{}, fmt.Errorf("%w: merchant name is too long", ErrBadInput)
	}
	householdUUID, err := parseUUID(input.HouseholdID, "household")
	if err != nil {
		return CreateResult{}, err
	}
	executor := bobpgx.NewTx(tx, nil)
	householdRow, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForShare()).One(ctx, executor)
	if err != nil {
		return CreateResult{}, err
	}
	actorUUID, err := parseUUID(input.ActorID, "actor")
	if err != nil {
		return CreateResult{}, err
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorUUID)),
	))).Exists(ctx, executor)
	if err != nil {
		return CreateResult{}, err
	}
	if !member {
		return CreateResult{}, ErrNotFound
	}
	key, operation := input.IdempotencyKey, "createPurchase"
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(key)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, executor)
	if err == nil {
		if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
			return CreateResult{}, err
		}
		return CreateResult{PurchaseID: existing.ResourceID.V.String()}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CreateResult{}, err
	}
	period := accounting.PeriodFor(input.OccurredAt)
	if err := ensureAccountingPeriod(ctx, executor, householdUUID, period); err != nil {
		return CreateResult{}, err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return CreateResult{}, err
	}
	if periodRow.Status == "CLOSED" {
		return CreateResult{}, fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}
	var sourceDocumentUUID *gofrsuuid.UUID
	if input.SourceDocumentID != nil {
		sourceReference := ""
		if input.SourceReference != nil {
			sourceReference = *input.SourceReference
		}
		if len(sourceReference) > 256 {
			return CreateResult{}, fmt.Errorf("%w: source reference is too long", ErrBadInput)
		}
		documentUUID, parseErr := parseUUID(*input.SourceDocumentID, "source document")
		if parseErr != nil {
			return CreateResult{}, parseErr
		}
		sourceDocumentUUID = &documentUUID
		document, err := models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(documentUUID))), sm.ForUpdate()).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return CreateResult{}, ErrNotFound
		}
		if err != nil {
			return CreateResult{}, err
		}
		if document.HouseholdID != householdUUID {
			return CreateResult{}, ErrNotFound
		}
		confirmation, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
			models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)),
			models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(sourceReference)),
		))).One(ctx, executor)
		if err == nil {
			return CreateResult{PurchaseID: confirmation.PurchaseID.String()}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return CreateResult{}, err
		}
	}
	ruleVersion := householdRow.DefaultAllocationVersion
	defaults, err := loadWeights(ctx, tx, householdUUID, ruleVersion)
	if err != nil {
		return CreateResult{}, err
	}
	if len(defaults) == 0 {
		return CreateResult{}, fmt.Errorf("%w: configure a default allocation before adding purchases", ErrBadInput)
	}
	merchantRuleKey := normalizeMerchant(merchant)
	purchaseUUID := newUUID()
	purchaseID := purchaseUUID.String()
	createdBy := actorUUID
	purchaseStatus := draftStatus
	if _, err := models.Purchases.Insert(&models.PurchaseSetter{
		ID: &purchaseUUID, HouseholdID: &householdUUID, OccurredAt: &input.OccurredAt, Merchant: &merchant,
		Status: &purchaseStatus, AllocationRuleVersion: &ruleVersion, CreatedBy: &createdBy, Origin: &origin,
	}).Exec(ctx, executor); err != nil {
		return CreateResult{}, err
	}
	purchaseItemUUIDs := make([]gofrsuuid.UUID, 0, len(input.Items))
	for _, item := range input.Items {
		if strings.TrimSpace(item.Name) == "" || len(item.Name) > 240 || item.AmountYen < 0 || item.AmountYen > maxMoney {
			return CreateResult{}, fmt.Errorf("%w: invalid purchase item", ErrBadInput)
		}
		measure, err := measureFromCommand(item)
		if err != nil {
			return CreateResult{}, fmt.Errorf("%w: %w", ErrBadInput, err)
		}
		var categoryUUID, variantUUID *gofrsuuid.UUID
		if item.CategoryID != nil {
			id, parseErr := parseUUID(*item.CategoryID, "category")
			if parseErr != nil {
				return CreateResult{}, parseErr
			}
			categoryUUID = &id
		}
		if item.ProductVariantID != nil {
			id, parseErr := parseUUID(*item.ProductVariantID, "product variant")
			if parseErr != nil {
				return CreateResult{}, parseErr
			}
			variantUUID = &id
			variant, err := models.ProductVariants.Query(sm.Where(psql.And(
				models.ProductVariants.Columns.ID.EQ(psql.Arg(id)),
				models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			))).One(ctx, executor)
			if errors.Is(err, sql.ErrNoRows) || err == nil && variant.Archived {
				return CreateResult{}, ErrNotFound
			}
			if err != nil {
				return CreateResult{}, err
			}
			concept, err := models.ProductConcepts.Query(sm.Where(psql.And(
				models.ProductConcepts.Columns.ID.EQ(psql.Arg(variant.ConceptID)),
				models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			))).One(ctx, executor)
			if errors.Is(err, sql.ErrNoRows) || err == nil && concept.Archived {
				return CreateResult{}, ErrNotFound
			}
			if err != nil {
				return CreateResult{}, err
			}
			if categoryUUID == nil && concept.CategoryID.Valid {
				id := concept.CategoryID.V
				categoryUUID = &id
			}
		}
		weights, appliedRuleID, appliedRuleVersion := defaults, (*gofrsuuid.UUID)(nil), ruleVersion
		allocationOverride := len(item.Allocations) > 0
		if allocationOverride {
			weights = make([]domain.Weight, 0, len(item.Allocations))
			for _, allocation := range item.Allocations {
				if allocation.PoolID == "" || allocation.Weight <= 0 {
					return CreateResult{}, fmt.Errorf("%w: allocation weights must be positive", ErrBadInput)
				}
				weights = append(weights, domain.Weight{PoolID: allocation.PoolID, Value: allocation.Weight})
			}
			if _, err := domain.Allocate(1, weights); err != nil {
				return CreateResult{}, fmt.Errorf("%w: %w", ErrBadInput, err)
			}
		} else {
			ruleWeights, ruleID, revision, err := allocationRuleWeights(ctx, tx, householdUUID, categoryUUID, variantUUID, merchantRuleKey)
			if err != nil {
				return CreateResult{}, err
			}
			if ruleID != nil {
				weights, appliedRuleID, appliedRuleVersion = ruleWeights, ruleID, revision
			}
		}
		allocated, err := domain.Allocate(item.AmountYen, weights)
		if err != nil {
			return CreateResult{}, fmt.Errorf("%w: %w", ErrBadInput, err)
		}
		itemUUID := newUUID()
		itemName, itemAmount := strings.TrimSpace(item.Name), item.AmountYen
		var categoryValue, variantValue sql.Null[gofrsuuid.UUID]
		if categoryUUID != nil {
			categoryValue = sql.Null[gofrsuuid.UUID]{V: *categoryUUID, Valid: true}
		}
		if variantUUID != nil {
			variantValue = sql.Null[gofrsuuid.UUID]{V: *variantUUID, Valid: true}
		}
		if _, err := models.PurchaseItems.Insert(&models.PurchaseItemSetter{
			ID: &itemUUID, HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, Name: &itemName,
			AmountYen: &itemAmount, CategoryID: &categoryValue, ProductVariantID: &variantValue, AllocationOverride: &allocationOverride,
			QuantityNumerator: &measure.quantityNumerator, QuantityDenominator: &measure.quantityDenominator, QuantityUnit: &measure.quantityUnit,
			PrintedUnitPriceYen: &measure.printedUnitPriceYen, PrintedUnitPriceQuantityNumerator: &measure.printedUnitPriceQuantityNumerator,
			PrintedUnitPriceQuantityDenominator: &measure.printedUnitPriceQuantityDenominator, PrintedUnitPriceUnit: &measure.printedUnitPriceUnit,
		}).Exec(ctx, executor); err != nil {
			return CreateResult{}, err
		}
		purchaseItemUUIDs = append(purchaseItemUUIDs, itemUUID)
		for _, allocation := range allocated {
			poolUUID, err := parseUUID(allocation.PoolID, "allocation pool")
			if err != nil {
				return CreateResult{}, err
			}
			var ruleUUID sql.Null[gofrsuuid.UUID]
			if appliedRuleID != nil {
				ruleUUID = sql.Null[gofrsuuid.UUID]{V: *appliedRuleID, Valid: true}
			}
			if _, err := models.ItemAllocations.Insert(&models.ItemAllocationSetter{
				HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, ItemID: &itemUUID, PoolID: &poolUUID,
				Weight: &allocation.Weight, AmountYen: &allocation.Amount, RuleVersion: &appliedRuleVersion, RuleID: &ruleUUID,
			}).Exec(ctx, executor); err != nil {
				return CreateResult{}, err
			}
		}
	}
	if len(input.PaymentAllocations) > 0 {
		payments, err := validatePayments(ctx, tx, householdUUID, input.PaymentAllocations)
		if err != nil {
			return CreateResult{}, err
		}
		if err := replacePayments(ctx, executor, householdUUID, purchaseUUID, payments); err != nil {
			return CreateResult{}, err
		}
	}
	purchaseResource := sql.Null[gofrsuuid.UUID]{V: purchaseUUID, Valid: true}
	fingerprintValue := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdUUID, Key: &key, Operation: &operation, ResourceID: &purchaseResource, RequestFingerprint: &fingerprintValue,
	}).Exec(ctx, executor); err != nil {
		return CreateResult{}, err
	}
	if input.SourceDocumentID != nil {
		if err := linkSourceDocument(ctx, executor, householdUUID, purchaseUUID, createdBy, *sourceDocumentUUID, purchaseItemUUIDs, input); err != nil {
			return CreateResult{}, err
		}
	}
	if err := accounting.RecordEvent(ctx, executor, householdUUID, purchaseUUID, "purchase.created", 1, []string{}, createdBy); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{PurchaseID: purchaseID}, nil
}

type itemMeasure struct {
	quantityNumerator, quantityDenominator                                                      sql.Null[int64]
	quantityUnit, printedUnitPriceUnit                                                          sql.Null[string]
	printedUnitPriceYen, printedUnitPriceQuantityNumerator, printedUnitPriceQuantityDenominator sql.Null[int64]
}

func measureFromCommand(item Item) (itemMeasure, error) {
	var measure itemMeasure
	if item.Quantity != nil {
		n, d, err := parseRational(item.Quantity.Numerator, item.Quantity.Denominator)
		if err != nil {
			return measure, err
		}
		measure.quantityNumerator, measure.quantityDenominator = n, d
		measure.quantityUnit = sql.Null[string]{V: item.Quantity.Unit, Valid: true}
	}
	if item.PrintedUnitPrice != nil {
		n, d, err := parseRational(item.PrintedUnitPrice.Basis.Numerator, item.PrintedUnitPrice.Basis.Denominator)
		if err != nil {
			return measure, err
		}
		measure.printedUnitPriceYen = sql.Null[int64]{V: item.PrintedUnitPrice.AmountYen, Valid: true}
		measure.printedUnitPriceQuantityNumerator, measure.printedUnitPriceQuantityDenominator = n, d
		measure.printedUnitPriceUnit = sql.Null[string]{V: item.PrintedUnitPrice.Basis.Unit, Valid: true}
	}
	if measure.quantityNumerator.Valid && (measure.quantityNumerator.V > maxQuantityNumerator || measure.quantityDenominator.V > maxQuantityDenominator || !validUnit(measure.quantityUnit.V)) {
		return measure, errors.New("invalid quantity")
	}
	if measure.printedUnitPriceYen.Valid && (measure.printedUnitPriceYen.V < 0 || measure.printedUnitPriceYen.V > maxMoney || measure.printedUnitPriceQuantityNumerator.V > maxQuantityNumerator || measure.printedUnitPriceQuantityDenominator.V > maxQuantityDenominator || !validUnit(measure.printedUnitPriceUnit.V)) {
		return measure, errors.New("invalid printed unit price")
	}
	return measure, nil
}

func parseRational(numerator, denominator string) (sql.Null[int64], sql.Null[int64], error) {
	n, err := strconv.ParseInt(numerator, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != numerator {
		return sql.Null[int64]{}, sql.Null[int64]{}, errors.New("invalid rational numerator")
	}
	d, err := strconv.ParseInt(denominator, 10, 64)
	if err != nil || d <= 0 || strconv.FormatInt(d, 10) != denominator {
		return sql.Null[int64]{}, sql.Null[int64]{}, errors.New("invalid rational denominator")
	}
	if new(big.Int).GCD(nil, nil, big.NewInt(n), big.NewInt(d)).Int64() != 1 {
		return sql.Null[int64]{}, sql.Null[int64]{}, errors.New("rational values must be reduced")
	}
	return sql.Null[int64]{V: n, Valid: true}, sql.Null[int64]{V: d, Valid: true}, nil
}

func validUnit(value string) bool {
	return slices.Contains([]string{"個", "本", "枚", "袋", "箱", "g", "kg", "ml", "L"}, value)
}

func allocationRuleWeights(ctx context.Context, tx pgx.Tx, householdID gofrsuuid.UUID, categoryID, variantID *gofrsuuid.UUID, merchant string) ([]domain.Weight, *gofrsuuid.UUID, int64, error) {
	executor := bobpgx.NewTx(tx, nil)
	var candidates []*models.AllocationRule
	appendRule := func(expression psql.Expression) error {
		rule, err := models.AllocationRules.Query(sm.Where(psql.And(models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdID)), expression))).One(ctx, executor)
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
	if variantID != nil {
		variant, err := models.ProductVariants.Query(sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.ProductVariants.Columns.ID.EQ(psql.Arg(*variantID))))).One(ctx, executor)
		if err == nil {
			id := variant.ConceptID
			conceptID = &id
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, 0, err
		}
		if err := appendRule(models.AllocationRules.Columns.ProductVariantID.EQ(psql.Arg(*variantID))); err != nil {
			return nil, nil, 0, err
		}
	}
	if conceptID != nil {
		if err := appendRule(models.AllocationRules.Columns.ProductConceptID.EQ(psql.Arg(*conceptID))); err != nil {
			return nil, nil, 0, err
		}
	}
	if categoryID != nil {
		if err := appendRule(models.AllocationRules.Columns.CategoryID.EQ(psql.Arg(*categoryID))); err != nil {
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
	rows, err := models.AllocationRuleWeights.Query(sm.Where(models.AllocationRuleWeights.Columns.RuleID.EQ(psql.Arg(rule.ID))), sm.OrderBy(models.AllocationRuleWeights.Columns.PoolID)).All(ctx, executor)
	if err != nil {
		return nil, nil, 0, err
	}
	weights := make([]domain.Weight, 0, len(rows))
	for _, row := range rows {
		weights = append(weights, domain.Weight{PoolID: row.PoolID.String(), Value: row.Weight})
	}
	if len(weights) == 0 {
		return nil, nil, 0, errors.New("allocation rule has no weights")
	}
	id := rule.ID
	return weights, &id, rule.Revision, nil
}

func loadWeights(ctx context.Context, tx pgx.Tx, householdID gofrsuuid.UUID, version int64) ([]domain.Weight, error) {
	rows, err := models.AllocationDefaults.Query(sm.Where(psql.And(models.AllocationDefaults.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.AllocationDefaults.Columns.Version.EQ(psql.Arg(version)))), sm.OrderBy(models.AllocationDefaults.Columns.PoolID)).All(ctx, bobpgx.NewTx(tx, nil))
	if err != nil {
		return nil, err
	}
	weights := make([]domain.Weight, 0, len(rows))
	for _, row := range rows {
		weights = append(weights, domain.Weight{PoolID: row.PoolID.String(), Value: row.Weight})
	}
	return weights, nil
}

func validatePayments(ctx context.Context, tx pgx.Tx, householdID gofrsuuid.UUID, payments []Payment) ([]Payment, error) {
	seen := make(map[string]struct{}, len(payments))
	result := make([]Payment, 0, len(payments))
	executor := bobpgx.NewTx(tx, nil)
	for _, payment := range payments {
		if payment.AccountID == "" || payment.AmountYen <= 0 || payment.AmountYen > maxMoney {
			return nil, fmt.Errorf("%w: payment allocations require an account and positive amount", ErrBadInput)
		}
		if _, exists := seen[payment.AccountID]; exists {
			return nil, fmt.Errorf("%w: payment account may appear only once", ErrBadInput)
		}
		seen[payment.AccountID] = struct{}{}
		accountID, err := parseUUID(payment.AccountID, "payment account")
		if err != nil {
			return nil, err
		}
		_, err = models.Accounts.Query(sm.Where(psql.And(models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.Accounts.Columns.ID.EQ(psql.Arg(accountID))))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		result = append(result, payment)
	}
	return result, nil
}

func replacePayments(ctx context.Context, executor bob.Executor, householdID, purchaseID gofrsuuid.UUID, payments []Payment) error {
	if _, err := models.PurchasePayments.Delete(dm.Where(psql.And(models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseID))))).Exec(ctx, executor); err != nil {
		return err
	}
	for i, payment := range payments {
		accountID, err := parseUUID(payment.AccountID, "payment account")
		if err != nil {
			return err
		}
		lineNo, amount := int32(i+1), payment.AmountYen
		if _, err := models.PurchasePayments.Insert(&models.PurchasePaymentSetter{HouseholdID: &householdID, PurchaseID: &purchaseID, LineNo: &lineNo, AccountID: &accountID, AmountYen: &amount}).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}

func ensureAccountingPeriod(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, period string) error {
	status, revision := "OPEN", int64(0)
	_, err := models.AccountingPeriods.Insert(&models.AccountingPeriodSetter{HouseholdID: &householdID, Period: &period, Status: &status, Revision: &revision}, im.OnConflict("household_id", "period").DoNothing()).Exec(ctx, executor)
	return err
}

func normalizeMerchant(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
func parseUUID(value, field string) (gofrsuuid.UUID, error) {
	id, err := gofrsuuid.FromString(value)
	if err != nil {
		return gofrsuuid.Nil, fmt.Errorf("%w: invalid %s id", ErrBadInput, field)
	}
	return id, nil
}

func newUUID() gofrsuuid.UUID { return gofrsuuid.Must(gofrsuuid.NewV7()) }
func validateIdempotencyFingerprint(existing *models.IdempotencyKey, fingerprint [sha256.Size]byte) error {
	if !existing.RequestFingerprint.Valid || !slices.Equal(existing.RequestFingerprint.V, fingerprint[:]) {
		return fmt.Errorf("%w: idempotency key was used with a different request", ErrConflict)
	}
	return nil
}

func linkSourceDocument(ctx context.Context, executor bob.Executor, householdID, purchaseID, actorID, documentID gofrsuuid.UUID, itemIDs []gofrsuuid.UUID, input CreateCommand) error {
	sourceReference := ""
	if input.SourceReference != nil {
		sourceReference = *input.SourceReference
	}
	if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{SourceDocumentID: &documentID, SourceReference: &sourceReference, HouseholdID: &householdID, PurchaseID: &purchaseID, CreatedBy: &actorID}).Exec(ctx, executor); err != nil {
		return err
	}
	if sourceReference != "" {
		return insertGenericEvidenceLink(ctx, executor, householdID, documentID, purchaseID)
	}
	amounts := make([]int64, len(input.Items))
	for i, item := range input.Items {
		amounts[i] = item.AmountYen
	}
	candidateID, overrideReason := "", ""
	if input.ReceiptCandidateID != nil {
		candidateID = *input.ReceiptCandidateID
	}
	if input.ManualAmountOverrideReason != nil {
		overrideReason = *input.ManualAmountOverrideReason
	}
	linkedItems, err := linkImportedReceiptItems(ctx, executor, householdID, documentID, purchaseID, itemIDs, amounts, candidateID, overrideReason)
	if err != nil {
		return err
	}
	if linkedItems {
		return nil
	}
	return insertGenericEvidenceLink(ctx, executor, householdID, documentID, purchaseID)
}

func insertGenericEvidenceLink(ctx context.Context, executor bob.Executor, householdID, documentID, purchaseID gofrsuuid.UUID) error {
	_, err := psql.Insert(im.Into(models.EvidenceLinks.NameExpr(), "household_id", "source_document_id", "purchase_id"), im.Values(psql.Arg(householdID, documentID, purchaseID)), im.OnConflict().DoNothing()).Exec(ctx, executor)
	return err
}

func linkImportedReceiptItems(ctx context.Context, executor bob.Executor, householdID, documentID, purchaseID gofrsuuid.UUID, itemIDs []gofrsuuid.UUID, amounts []int64, candidateID, overrideReason string) (bool, error) {
	job, err := models.ExtractionJobs.Query(sm.Where(psql.And(models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(documentID)), models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")))), sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(), sm.Limit(1)).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !job.Result.Valid {
		return false, nil
	}
	var verified ingestion.Verification
	if err := stdjson.Unmarshal(job.Result.V.Val, &verified); err != nil {
		return false, err
	}
	if len(verified.Receipt.Items) == 0 {
		if candidateID != "" || strings.TrimSpace(overrideReason) != "" {
			return false, fmt.Errorf("%w: receipt verification is unavailable", ErrBadInput)
		}
		return false, nil
	}
	overridden, err := ingestion.ValidateReceiptConfirmation(verified, amounts, candidateID, overrideReason)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	for index, item := range verified.Receipt.Items {
		values := map[string]any{"jobId": job.ID.String(), "itemIndex": index, "item": item, "transform": verified.Transform, "transformPage": verified.TransformPage}
		if candidateID != "" {
			values["receiptCandidateId"] = candidateID
		}
		if overridden {
			values["manualAmountOverrideReason"] = strings.TrimSpace(overrideReason)
		}
		observation, err := stdjson.Marshal(values)
		if err != nil {
			return false, err
		}
		observationJSON := types.JSON[stdjson.RawMessage]{Val: observation}
		var region sql.Null[types.JSON[stdjson.RawMessage]]
		if item.Region != nil {
			encoded, err := stdjson.Marshal(item.Region)
			if err != nil {
				return false, err
			}
			region = sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: encoded}, Valid: true}
		}
		itemID := sql.Null[gofrsuuid.UUID]{V: itemIDs[index], Valid: true}
		if _, err := psql.Insert(im.Into(models.EvidenceLinks.NameExpr(), "household_id", "source_document_id", "purchase_id", "item_id", "evidence_region", "observation"), im.Values(psql.Arg(householdID, documentID, purchaseID, itemID, region, observationJSON)), im.OnConflict().DoNothing()).Exec(ctx, executor); err != nil {
			return false, err
		}
	}
	return true, nil
}
