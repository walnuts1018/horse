package purchase

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"fmt"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type itemChangePlan struct {
	revision           int64
	fieldRevisions     []byte
	categoryID         sql.Null[gofrsuuid.UUID]
	productVariantID   sql.Null[gofrsuuid.UUID]
	allocationOverride bool
	allocationsChanged bool
	allocations        []domain.AllocationAmount
	allocationRuleID   *gofrsuuid.UUID
	allocationVersion  int64
}

type itemChangeInput struct {
	fields                   []string
	amount                   int64
	categoryID               sql.Null[gofrsuuid.UUID]
	productVariantID         sql.Null[gofrsuuid.UUID]
	allocationOverride       bool
	categoryOrVariantChanged bool
	allocationsSet           bool
	requestedWeights         []domain.Weight
	defaultAllocationVersion int64
}

func buildItemChangePlan(ctx context.Context, tx pgx.Tx, executor bob.Executor, item *models.PurchaseItem, purchase *models.Purchase, input itemChangeInput) (itemChangePlan, error) {
	fieldRevisions, err := purchaseUpdateFieldRevisions(item.FieldRevisions.Val, input.fields, item.Revision+1)
	if err != nil {
		return itemChangePlan{}, err
	}
	plan := itemChangePlan{
		revision: item.Revision + 1, fieldRevisions: fieldRevisions,
		categoryID: input.categoryID, productVariantID: input.productVariantID,
		allocationOverride: input.allocationOverride,
		allocationVersion:  input.defaultAllocationVersion,
	}
	plan.allocationsChanged = input.allocationsSet || input.amount != item.AmountYen || input.categoryOrVariantChanged && !item.AllocationOverride
	if !plan.allocationsChanged {
		return plan, nil
	}

	weights := input.requestedWeights
	if !input.allocationsSet {
		if input.categoryOrVariantChanged && !item.AllocationOverride {
			var categoryUUID, variantUUID *gofrsuuid.UUID
			if input.categoryID.Valid {
				categoryUUID = &input.categoryID.V
			}
			if input.productVariantID.Valid {
				variantUUID = &input.productVariantID.V
			}
			weights, plan.allocationRuleID, plan.allocationVersion, err = allocationRuleWeights(ctx, tx, item.HouseholdID, categoryUUID, variantUUID, normalizeMerchant(purchase.Merchant))
			if err == nil && plan.allocationRuleID == nil {
				weights, err = loadWeights(ctx, tx, item.HouseholdID, input.defaultAllocationVersion)
			}
		} else {
			allocations, queryErr := models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(item.ID))), sm.OrderBy(models.ItemAllocations.Columns.PoolID)).All(ctx, executor)
			if queryErr != nil {
				return itemChangePlan{}, queryErr
			}
			weights = make([]domain.Weight, 0, len(allocations))
			for _, allocation := range allocations {
				weights = append(weights, domain.Weight{PoolID: allocation.PoolID.String(), Value: allocation.Weight})
				if len(weights) == 1 {
					if allocation.RuleID.Valid {
						id := allocation.RuleID.V
						plan.allocationRuleID = &id
					}
					plan.allocationVersion = allocation.RuleVersion
				}
			}
			if len(weights) == 0 {
				weights, err = loadWeights(ctx, tx, item.HouseholdID, input.defaultAllocationVersion)
			}
		}
		if err != nil {
			return itemChangePlan{}, err
		}
	}
	plan.allocations, err = domain.Allocate(input.amount, weights)
	if err != nil {
		return itemChangePlan{}, fmt.Errorf("%w: %w", ErrBadInput, err)
	}
	return plan, nil
}

func applyItemAllocationPlan(ctx context.Context, executor bob.Executor, item *models.PurchaseItem, plan itemChangePlan) error {
	if !plan.allocationsChanged {
		return nil
	}
	if _, err := models.ItemAllocations.Delete(dm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(item.ID)))).Exec(ctx, executor); err != nil {
		return err
	}
	fieldRuleID := sql.Null[gofrsuuid.UUID]{}
	if plan.allocationRuleID != nil {
		fieldRuleID = sql.Null[gofrsuuid.UUID]{V: *plan.allocationRuleID, Valid: true}
	}
	for _, allocation := range plan.allocations {
		poolID, err := parseUUID(allocation.PoolID, "pool")
		if err != nil {
			return fmt.Errorf("%w: invalid allocation pool", ErrBadInput)
		}
		if _, err := models.ItemAllocations.Insert(&models.ItemAllocationSetter{
			HouseholdID: &item.HouseholdID, PurchaseID: &item.PurchaseID, ItemID: &item.ID, PoolID: &poolID,
			Weight: &allocation.Weight, AmountYen: &allocation.Amount, RuleVersion: &plan.allocationVersion, RuleID: &fieldRuleID,
		}).Exec(ctx, executor); err != nil {
			return err
		}
	}
	return nil
}

func itemChangeFieldValue(plan itemChangePlan) types.JSON[stdjson.RawMessage] {
	return types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(plan.fieldRevisions)}
}
