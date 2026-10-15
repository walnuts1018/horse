package purchase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type createAuthorization struct {
	actorID               gofrsuuid.UUID
	allocationRuleVersion int64
}

type createSourceResolution struct {
	documentID         *gofrsuuid.UUID
	existingPurchaseID string
}

func authorizeCreate(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, actorID string) (createAuthorization, error) {
	household, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForShare()).One(ctx, executor)
	if err != nil {
		return createAuthorization{}, err
	}
	actorUUID, err := parseUUID(actorID, "actor")
	if err != nil {
		return createAuthorization{}, err
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorUUID)),
		models.HouseholdMembers.Columns.LeftAt.IsNull(),
	))).Exists(ctx, executor)
	if err != nil {
		return createAuthorization{}, err
	}
	if !member {
		return createAuthorization{}, ErrNotFound
	}
	return createAuthorization{actorID: actorUUID, allocationRuleVersion: household.DefaultAllocationVersion}, nil
}

func findCreateIdempotentResult(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, key string, fingerprint [32]byte) (CreateResult, bool, error) {
	operation := "createPurchase"
	existing, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(key)),
		models.IdempotencyKeys.Columns.Operation.EQ(psql.Arg(operation)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return CreateResult{}, false, nil
	}
	if err != nil {
		return CreateResult{}, false, err
	}
	if err := validateIdempotencyFingerprint(existing, fingerprint); err != nil {
		return CreateResult{}, false, err
	}
	return CreateResult{PurchaseID: existing.ResourceID.V.String()}, true, nil
}

func lockCreateAccountingPeriod(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, occurredAt time.Time) error {
	period := accounting.PeriodFor(occurredAt)
	if err := ensureAccountingPeriod(ctx, executor, householdID, period); err != nil {
		return err
	}
	periodRow, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.AccountingPeriods.Columns.Period.EQ(psql.Arg(period)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return err
	}
	if periodRow.Status == "CLOSED" {
		return fmt.Errorf("%w: accounting period is closed", ErrConflict)
	}
	return nil
}

func resolveCreateSourceDocument(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, input CreateCommand) (createSourceResolution, error) {
	if input.SourceDocumentID == nil {
		return createSourceResolution{}, nil
	}
	sourceReference := ""
	if input.SourceReference != nil {
		sourceReference = *input.SourceReference
	}
	if len(sourceReference) > 256 {
		return createSourceResolution{}, fmt.Errorf("%w: source reference is too long", ErrBadInput)
	}
	documentID, err := parseUUID(*input.SourceDocumentID, "source document")
	if err != nil {
		return createSourceResolution{}, err
	}
	document, err := models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(documentID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return createSourceResolution{}, ErrNotFound
	}
	if err != nil {
		return createSourceResolution{}, err
	}
	if document.HouseholdID != householdID {
		return createSourceResolution{}, ErrNotFound
	}
	confirmation, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(sourceReference)),
	))).One(ctx, executor)
	if err == nil {
		if confirmation.CorrectionID.Valid || !confirmation.PurchaseID.Valid {
			return createSourceResolution{}, fmt.Errorf("%w: source row is already confirmed as a correction", ErrConflict)
		}
		return createSourceResolution{existingPurchaseID: confirmation.PurchaseID.V.String()}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return createSourceResolution{}, err
	}
	return createSourceResolution{documentID: &documentID}, nil
}

func loadCreateDefaultWeights(ctx context.Context, tx pgx.Tx, householdID gofrsuuid.UUID, ruleVersion int64) ([]domain.Weight, error) {
	defaults, err := loadWeights(ctx, tx, householdID, ruleVersion)
	if err != nil {
		return nil, err
	}
	if len(defaults) == 0 {
		return nil, fmt.Errorf("%w: configure a default allocation before adding purchases", ErrBadInput)
	}
	return defaults, nil
}

func persistCreate(ctx context.Context, tx pgx.Tx, executor bob.Executor, input CreateCommand, origin string, fingerprint [32]byte, householdID gofrsuuid.UUID, authorization createAuthorization, sourceDocumentID *gofrsuuid.UUID, defaults []domain.Weight) (CreateResult, error) {
	purchaseID := newUUID()
	purchaseStatus := draftStatus
	merchant := input.Merchant
	merchantRuleKey := normalizeMerchant(merchant)
	createdBy := authorization.actorID
	ruleVersion := authorization.allocationRuleVersion
	if _, err := models.Purchases.Insert(&models.PurchaseSetter{
		ID: &purchaseID, HouseholdID: &householdID, OccurredAt: &input.OccurredAt, DatePrecision: &input.DatePrecision, Merchant: &merchant,
		Status: &purchaseStatus, AllocationRuleVersion: &ruleVersion, CreatedBy: &createdBy, Origin: &origin,
	}).Exec(ctx, executor); err != nil {
		return CreateResult{}, err
	}
	purchaseItemIDs, err := createPurchaseItems(ctx, tx, executor, householdID, purchaseID, merchantRuleKey, ruleVersion, defaults, input.Items)
	if err != nil {
		return CreateResult{}, err
	}
	if len(input.PaymentAllocations) > 0 {
		payments, err := validatePayments(ctx, tx, householdID, input.PaymentAllocations)
		if err != nil {
			return CreateResult{}, err
		}
		if err := replacePayments(ctx, executor, householdID, purchaseID, payments); err != nil {
			return CreateResult{}, err
		}
	}
	purchaseResource := sql.Null[gofrsuuid.UUID]{V: purchaseID, Valid: true}
	fingerprintValue := sql.Null[[]byte]{V: fingerprint[:], Valid: true}
	key, operation := input.IdempotencyKey, "createPurchase"
	if _, err := models.IdempotencyKeys.Insert(&models.IdempotencyKeySetter{
		HouseholdID: &householdID, Key: &key, Operation: &operation, ResourceID: &purchaseResource, RequestFingerprint: &fingerprintValue,
	}).Exec(ctx, executor); err != nil {
		return CreateResult{}, err
	}
	if sourceDocumentID != nil {
		if err := linkSourceDocument(ctx, executor, householdID, purchaseID, createdBy, *sourceDocumentID, purchaseItemIDs, input); err != nil {
			return CreateResult{}, err
		}
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, purchaseID, "purchase.created", 1, []string{}, createdBy); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{PurchaseID: purchaseID.String()}, nil
}
