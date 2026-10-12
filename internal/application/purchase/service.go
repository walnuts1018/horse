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
	"time"

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
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	"uuid"
)

var (
	ErrBadInput                    = errors.New("invalid input")
	ErrConflict                    = errors.New("resource conflict")
	ErrNotFound                    = errors.New("resource not found")
	ErrUnattributedPurchasePayment = errors.New("posted purchase payment has no funding pool")
)

const maxMoney int64 = 1_000_000_000_000
const maxQuantityNumerator int64 = 1_000_000_000_000
const maxQuantityDenominator int64 = 1_000_000

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

func (s *Service) Create(ctx context.Context, command CreateCommand, fingerprint [sha256.Size]byte) (CreateResult, error) {
	return s.create(ctx, command, fingerprint)
}

func (s *Service) Post(ctx context.Context, command PostCommand, fingerprint [sha256.Size]byte) (PostResult, error) {
	return s.post(ctx, command, fingerprint)
}

func (s *Service) create(ctx context.Context, input CreateCommand, fingerprint [sha256.Size]byte) (CreateResult, error) {
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 200 || len(input.Items) == 0 || len(input.Items) > 200 {
		return CreateResult{}, fmt.Errorf("%w: invalid purchase", ErrBadInput)
	}
	if input.SourceReference != nil && (input.SourceDocumentID == nil || len(*input.SourceReference) > 256 || strings.ContainsAny(*input.SourceReference, "\r\n")) {
		return CreateResult{}, fmt.Errorf("%w: source reference requires a document and must be valid", ErrBadInput)
	}
	origin := input.Origin
	if origin == "" {
		origin = "MANUAL"
		if input.SourceDocumentID != nil {
			origin = "AI"
		}
	}
	if origin != "AI" && origin != "MANUAL" && origin != "UNKNOWN" {
		return CreateResult{}, fmt.Errorf("%w: invalid purchase origin", ErrBadInput)
	}
	merchant := strings.TrimSpace(input.Merchant)
	if len(merchant) > 240 {
		return CreateResult{}, fmt.Errorf("%w: merchant name is too long", ErrBadInput)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreateResult{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdUUID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return CreateResult{}, fmt.Errorf("%w: invalid household id", ErrBadInput)
	}
	actorUUID, err := gofrsuuid.FromString(input.ActorID)
	if err != nil {
		return CreateResult{}, err
	}
	executor := bobpgx.NewTx(tx, nil)
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
		if err := tx.Rollback(ctx); err != nil {
			return CreateResult{}, err
		}
		return CreateResult{PurchaseID: existing.ResourceID.V.String()}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CreateResult{}, err
	}
	period := accountingPeriod(input.OccurredAt)
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
	if input.SourceDocumentID != nil {
		sourceReference := ""
		if input.SourceReference != nil {
			sourceReference = *input.SourceReference
		}
		if len(sourceReference) > 256 {
			return CreateResult{}, fmt.Errorf("%w: source reference is too long", ErrBadInput)
		}
		documentUUID, parseErr := gofrsuuid.FromString(*input.SourceDocumentID)
		if parseErr != nil {
			return CreateResult{}, fmt.Errorf("%w: invalid source document id", ErrBadInput)
		}
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
			if err := tx.Rollback(ctx); err != nil {
				return CreateResult{}, err
			}
			return CreateResult{PurchaseID: confirmation.PurchaseID.String()}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return CreateResult{}, err
		}
	}
	householdRow, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID))), sm.ForShare()).One(ctx, executor)
	if err != nil {
		return CreateResult{}, err
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
	purchaseStatus := "DRAFT"
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
		categoryID := item.CategoryID
		if item.ProductVariantID != nil {
			variantUUID, parseErr := gofrsuuid.FromString(*item.ProductVariantID)
			if parseErr != nil {
				return CreateResult{}, fmt.Errorf("%w: invalid product variant id", ErrBadInput)
			}
			variant, err := models.ProductVariants.Query(sm.Where(psql.And(
				models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)),
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
			if categoryID == nil && concept.CategoryID.Valid {
				id := concept.CategoryID.V.String()
				categoryID = &id
			}
		}
		weights, appliedRuleID, appliedRuleVersion := defaults, (*string)(nil), ruleVersion
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
			ruleWeights, ruleID, revision, err := allocationRuleWeights(ctx, tx, householdUUID, categoryID, item.ProductVariantID, merchantRuleKey)
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
		if categoryID != nil {
			categoryValue = sql.Null[gofrsuuid.UUID]{V: parseUUID(*categoryID), Valid: true}
		}
		if item.ProductVariantID != nil {
			variantValue = sql.Null[gofrsuuid.UUID]{V: parseUUID(*item.ProductVariantID), Valid: true}
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
			poolUUID := parseUUID(allocation.PoolID)
			var ruleUUID sql.Null[gofrsuuid.UUID]
			if appliedRuleID != nil {
				ruleUUID = sql.Null[gofrsuuid.UUID]{V: parseUUID(*appliedRuleID), Valid: true}
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
		if err := linkSourceDocument(ctx, executor, householdUUID, purchaseUUID, createdBy, purchaseItemUUIDs, input); err != nil {
			return CreateResult{}, err
		}
	}
	if err := writeEvent(ctx, tx, householdUUID, purchaseUUID, "purchase.created", 1, []string{}, createdBy); err != nil {
		return CreateResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
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

func allocationRuleWeights(ctx context.Context, tx pgx.Tx, householdID gofrsuuid.UUID, categoryID, variantID *string, merchant string) ([]domain.Weight, *string, int64, error) {
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
		variantUUID, err := gofrsuuid.FromString(*variantID)
		if err != nil {
			return nil, nil, 0, err
		}
		variant, err := models.ProductVariants.Query(sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID))))).One(ctx, executor)
		if err == nil {
			id := variant.ConceptID
			conceptID = &id
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, 0, err
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
		id, err := gofrsuuid.FromString(*categoryID)
		if err != nil {
			return nil, nil, 0, err
		}
		if err := appendRule(models.AllocationRules.Columns.CategoryID.EQ(psql.Arg(id))); err != nil {
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
	id := rule.ID.String()
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
		accountID, err := gofrsuuid.FromString(payment.AccountID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid payment account id", ErrBadInput)
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
		accountID, err := gofrsuuid.FromString(payment.AccountID)
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

func writeEvent(ctx context.Context, tx pgx.Tx, householdID, entityID gofrsuuid.UUID, eventType string, revision int64, fields []string, actorID gofrsuuid.UUID) error {
	encoded, err := stdjson.Marshal(fields)
	if err != nil {
		return err
	}
	changedFields := types.JSON[stdjson.RawMessage]{Val: encoded}
	actor := sql.Null[gofrsuuid.UUID]{V: actorID, Valid: true}
	_, err = models.HouseholdEvents.Insert(&models.HouseholdEventSetter{HouseholdID: &householdID, Type: &eventType, EntityID: &entityID, Revision: &revision, ChangedFields: &changedFields, ActorID: &actor}).Exec(ctx, bobpgx.NewTx(tx, nil))
	return err
}

func accountingPeriod(value time.Time) string {
	return value.In(time.FixedZone("JST", 9*60*60)).Format("2006-01")
}
func normalizeMerchant(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
func parseUUID(value string) gofrsuuid.UUID { id, _ := gofrsuuid.FromString(value); return id }
func newUUID() gofrsuuid.UUID               { return parseUUID(uuid.NewV7().String()) }
func validateIdempotencyFingerprint(existing *models.IdempotencyKey, fingerprint [sha256.Size]byte) error {
	if !existing.RequestFingerprint.Valid || !slices.Equal(existing.RequestFingerprint.V, fingerprint[:]) {
		return fmt.Errorf("%w: idempotency key was used with a different request", ErrConflict)
	}
	return nil
}

func linkSourceDocument(ctx context.Context, executor bob.Executor, householdID, purchaseID, actorID gofrsuuid.UUID, itemIDs []gofrsuuid.UUID, input CreateCommand) error {
	documentID := parseUUID(*input.SourceDocumentID)
	sourceReference := ""
	if input.SourceReference != nil {
		sourceReference = *input.SourceReference
	}
	if _, err := models.DocumentConfirmations.Insert(&models.DocumentConfirmationSetter{SourceDocumentID: &documentID, SourceReference: &sourceReference, HouseholdID: &householdID, PurchaseID: &purchaseID, CreatedBy: &actorID}).Exec(ctx, executor); err != nil {
		return err
	}
	if _, err := psql.Insert(im.Into(models.EvidenceLinks.NameExpr(), "household_id", "source_document_id", "purchase_id"), im.Values(psql.Arg(householdID, documentID, purchaseID)), im.OnConflict().DoNothing()).Exec(ctx, executor); err != nil {
		return err
	}
	if sourceReference != "" {
		return nil
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
	return linkImportedReceiptItems(ctx, executor, householdID, documentID, purchaseID, itemIDs, amounts, candidateID, overrideReason)
}

func linkImportedReceiptItems(ctx context.Context, executor bob.Executor, householdID, documentID, purchaseID gofrsuuid.UUID, itemIDs []gofrsuuid.UUID, amounts []int64, candidateID, overrideReason string) error {
	job, err := models.ExtractionJobs.Query(sm.Where(psql.And(models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(documentID)), models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")))), sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(), sm.Limit(1)).One(ctx, executor)
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
	if err := stdjson.Unmarshal(job.Result.V.Val, &verified); err != nil {
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
		values := map[string]any{"jobId": job.ID.String(), "itemIndex": index, "item": item, "transform": verified.Transform, "transformPage": verified.TransformPage}
		if candidateID != "" {
			values["receiptCandidateId"] = candidateID
		}
		if overridden {
			values["manualAmountOverrideReason"] = strings.TrimSpace(overrideReason)
		}
		observation, err := stdjson.Marshal(values)
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
		if _, err := psql.Insert(im.Into(models.EvidenceLinks.NameExpr(), "household_id", "source_document_id", "purchase_id", "item_id", "evidence_region", "observation"), im.Values(psql.Arg(householdID, documentID, purchaseID, itemID, region, observationJSON)), im.OnConflict().DoNothing()).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}
