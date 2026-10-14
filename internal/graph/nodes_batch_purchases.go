package graph

import (
	"context"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (b *nodeBatch) loadPurchaseNodes(ctx context.Context) error {
	executor := b.executor
	idsByKind := b.idsByKind
	householdArgs, args, set := b.householdArgs, b.ids, b.set
	r := b.resolver
	if values := idsByKind[nodeTypePurchase]; len(values) > 0 {
		rows, err := models.Purchases.Query(sm.Where(psql.And(
			models.Purchases.Columns.HouseholdID.In(householdArgs...), models.Purchases.Columns.ID.In(args(nodeTypePurchase)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		purchases, err := loadPurchases(ctx, r.DB, rows)
		if err != nil {
			return err
		}
		for _, purchase := range purchases {
			set(nodeTypePurchase, purchase.ID, purchase)
		}
	}
	childPurchaseIDs := make(map[uuid.UUID]struct{})
	correctionsByPurchase := make(map[uuid.UUID]models.CorrectionSlice)
	if values := idsByKind[nodeTypePurchaseItem]; len(values) > 0 {
		rows, err := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.HouseholdID.In(householdArgs...), models.PurchaseItems.Columns.ID.In(args(nodeTypePurchaseItem)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, row := range rows {
			childPurchaseIDs[row.PurchaseID] = struct{}{}
		}
	}
	if values := idsByKind[nodeTypePurchaseCorrection]; len(values) > 0 {
		rows, err := models.Corrections.Query(sm.Where(psql.And(
			models.Corrections.Columns.HouseholdID.In(householdArgs...), models.Corrections.Columns.ID.In(args(nodeTypePurchaseCorrection)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, row := range rows {
			childPurchaseIDs[row.PurchaseID] = struct{}{}
			correctionsByPurchase[row.PurchaseID] = append(correctionsByPurchase[row.PurchaseID], row)
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
				set(nodeTypePurchaseItem, item.ID, item)
			}
			edges, err := purchaseCorrectionEdges(ctx, r.DB, mustUUID(purchase.ID), correctionsByPurchase[mustUUID(purchase.ID)])
			if err != nil {
				return err
			}
			for _, edge := range edges {
				set(nodeTypePurchaseCorrection, edge.Node.GetID(), edge.Node)
			}
		}
	}
	return nil
}
