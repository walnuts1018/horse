package graph

import (
	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (b *nodeBatch) loadPurchaseNodes() error {
	ctx, executor := b.ctx, b.executor
	idsByKind := b.idsByKind
	householdArgs, args, set := b.householdArgs, b.ids, b.set
	r := b.resolver
	if values := idsByKind["Purchase"]; len(values) > 0 {
		rows, err := models.Purchases.Query(sm.Where(psql.And(
			models.Purchases.Columns.HouseholdID.In(householdArgs...), models.Purchases.Columns.ID.In(args("Purchase")...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		purchases, err := loadPurchases(ctx, r.DB, rows)
		if err != nil {
			return err
		}
		for _, purchase := range purchases {
			set("Purchase", purchase.ID, purchase)
		}
	}
	childPurchaseIDs := make(map[uuid.UUID]struct{})
	if values := idsByKind["PurchaseItem"]; len(values) > 0 {
		rows, err := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.HouseholdID.In(householdArgs...), models.PurchaseItems.Columns.ID.In(args("PurchaseItem")...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, row := range rows {
			childPurchaseIDs[row.PurchaseID] = struct{}{}
		}
	}
	if values := idsByKind["PurchaseCorrection"]; len(values) > 0 {
		rows, err := models.Corrections.Query(sm.Where(psql.And(
			models.Corrections.Columns.HouseholdID.In(householdArgs...), models.Corrections.Columns.ID.In(args("PurchaseCorrection")...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, row := range rows {
			childPurchaseIDs[row.PurchaseID] = struct{}{}
		}
	}
	if len(childPurchaseIDs) > 0 {
		parentIDs := make([]bob.Expression, 0, len(childPurchaseIDs))
		for id := range childPurchaseIDs {
			parentIDs = append(parentIDs, psql.Arg(id))
		}
		rows, err := models.Purchases.Query(sm.Where(psql.And(
			models.Purchases.Columns.HouseholdID.In(householdArgs...), models.Purchases.Columns.ID.In(parentIDs...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		purchases, err := loadPurchases(ctx, r.DB, rows)
		if err != nil {
			return err
		}
		for _, purchase := range purchases {
			for _, item := range purchase.Items {
				set("PurchaseItem", item.ID, item)
			}
			for _, correction := range purchase.Corrections {
				set("PurchaseCorrection", correction.ID, correction)
			}
		}
	}
	return nil
}
