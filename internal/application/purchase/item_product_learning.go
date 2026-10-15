package purchase

import (
	"context"
	"fmt"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const (
	itemLearningItemOnly      = "ITEM_ONLY"
	itemLearningVariantName   = "VARIANT_NAME"
	itemLearningMerchantAlias = "MERCHANT_ALIAS"
)

type itemProductLearningPlan struct {
	scope       string
	variantName *string
}

func prepareItemProductLearning(command UpdateItemCommand, variantID *string) (itemProductLearningPlan, error) {
	if command.ProductLearningSet && command.ProductLearning == nil {
		return itemProductLearningPlan{}, fmt.Errorf("%w: product learning configuration must select a scope", ErrBadInput)
	}
	scope, variantName, err := updateItemLearning(command.ProductLearning)
	if err != nil {
		return itemProductLearningPlan{}, err
	}
	if command.ProductLearning != nil && !slices.Contains(command.ChangedFields, itemFieldProductVariantID) {
		return itemProductLearningPlan{}, fmt.Errorf("%w: product learning requires a product variant change", ErrBadInput)
	}
	if scope != itemLearningItemOnly && variantID == nil {
		return itemProductLearningPlan{}, fmt.Errorf("%w: product learning requires a linked variant", ErrBadInput)
	}
	return itemProductLearningPlan{scope: scope, variantName: variantName}, nil
}

func applyItemVariantNameLearning(ctx context.Context, executor bob.Executor, householdID, actorID gofrsuuid.UUID, variantID *string, plan itemProductLearningPlan) error {
	if plan.scope != itemLearningVariantName {
		return nil
	}
	variantName := strings.TrimSpace(*plan.variantName)
	variantKey := normalizeProductName(variantName)
	if variantKey == "" || len(variantName) > 240 {
		return fmt.Errorf("%w: invalid normalized product name", ErrBadInput)
	}
	variantUUID, err := parseUUID(*variantID, "product variant")
	if err != nil {
		return ErrNotFound
	}
	if _, err := models.ProductVariants.Update((&models.ProductVariantSetter{Name: &variantName, NormalizedName: &variantKey}).UpdateMod(),
		um.Where(psql.And(models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)), models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)))),
		um.SetCol("revision").To(models.ProductVariants.Columns.Revision.Plus(psql.Arg(int64(1)))),
		um.SetCol("updated_at").To(psql.F("now")),
	).Exec(ctx, executor); err != nil {
		return err
	}
	variant, err := models.ProductVariants.Query(sm.Where(models.ProductVariants.Columns.ID.EQ(psql.Arg(variantUUID)))).One(ctx, executor)
	if err != nil {
		return err
	}
	return accounting.RecordEvent(ctx, executor, householdID, variant.ID, "product.variant.renamed", variant.Revision, []string{itemFieldName}, actorID)
}

func recordItemProductVariantChange(ctx context.Context, executor bob.Executor, householdID, actorID gofrsuuid.UUID, purchase *models.Purchase, item *models.PurchaseItem, itemID gofrsuuid.UUID, command UpdateItemCommand, variantID *string, plan itemProductLearningPlan) error {
	if !command.ProductVariantIDSet || variantID == nil {
		return nil
	}
	merchantKey := normalizeMerchant(purchase.Merchant)
	if plan.scope == itemLearningMerchantAlias {
		if merchantKey == "" {
			return fmt.Errorf("%w: merchant-specific learning requires a merchant", ErrBadInput)
		}
		aliasID, err := upsertProductAlias(ctx, executor, householdID, variantID, merchantKey, item.Name, normalizeProductName(item.Name))
		if err != nil {
			return err
		}
		aliasUUID, err := parseUUID(aliasID, "product alias")
		if err != nil {
			return err
		}
		alias, err := models.ProductAliases.Query(sm.Where(models.ProductAliases.Columns.ID.EQ(psql.Arg(aliasUUID)))).One(ctx, executor)
		if err != nil {
			return err
		}
		if err := accounting.RecordEvent(ctx, executor, householdID, alias.ID, "product.alias.learned", alias.Revision, []string{"alias", "variantId", "merchant"}, actorID); err != nil {
			return err
		}
	}
	variantUUID, err := parseUUID(*variantID, "product variant")
	if err != nil {
		return ErrNotFound
	}
	previousVariant, err := nullableUUID(variantIDValue(item.ProductVariantID))
	if err != nil {
		return ErrNotFound
	}
	correctionUUID := newUUID()
	_, err = models.ProductCorrections.Insert(&models.ProductCorrectionSetter{
		ID: &correctionUUID, HouseholdID: &householdID, PurchaseID: &purchase.ID, ItemID: &itemID,
		RawName: &item.Name, PreviousProductVariantID: &previousVariant, ProductVariantID: &variantUUID,
		LearningScope: &plan.scope, MerchantKey: &merchantKey, ActorID: &actorID,
	}).Exec(ctx, executor)
	return err
}

func updateItemLearning(value *ProductLearning) (string, *string, error) {
	if value == nil {
		return itemLearningItemOnly, nil, nil
	}
	selected := 0
	if value.ItemOnly != nil {
		selected++
	}
	if value.VariantName != nil {
		selected++
	}
	if value.MerchantAlias != nil {
		selected++
	}
	if selected != 1 {
		return "", nil, fmt.Errorf("%w: product learning configuration must select exactly one scope", ErrBadInput)
	}
	if value.ItemOnly != nil && *value.ItemOnly {
		return itemLearningItemOnly, nil, nil
	}
	if value.VariantName != nil {
		return itemLearningVariantName, value.VariantName, nil
	}
	if value.MerchantAlias != nil && *value.MerchantAlias {
		return itemLearningMerchantAlias, nil, nil
	}
	return "", nil, fmt.Errorf("%w: product learning marker must be true", ErrBadInput)
}
