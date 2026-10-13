package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func budgetActualScope(householdID uuid.UUID, categoryID sql.Null[uuid.UUID]) string {
	if categoryID.Valid {
		return householdID.String() + ":" + categoryID.V.String()
	}
	return householdID.String() + ":"
}

func (r *queryResolver) batchLoanBalances(ctx context.Context, households []*model.Household, loans models.TransferSlice) (map[uuid.UUID]*model.LoanBalance, error) {
	result := make(map[uuid.UUID]*model.LoanBalance, len(loans))
	if len(loans) == 0 {
		return result, nil
	}
	executor := queryExecutor(r.DB)
	householdArgs := make([]bob.Expression, 0, len(households))
	ledgerArgs := make([]bob.Expression, 0, len(loans))
	loanArgs := make([]bob.Expression, 0, len(loans))
	for _, household := range households {
		householdArgs = append(householdArgs, psql.Arg(mustUUID(household.ID)))
	}
	for _, loan := range loans {
		ledgerArgs = append(ledgerArgs, psql.Arg(loan.LedgerEntryID))
		loanArgs = append(loanArgs, psql.Arg(sql.Null[uuid.UUID]{V: loan.ID, Valid: true}))
	}
	ledgers, err := models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.HouseholdID.In(householdArgs...), models.LedgerEntries.Columns.ID.In(ledgerArgs...),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	ledgerByID := make(map[uuid.UUID]*models.LedgerEntry, len(ledgers))
	for _, ledger := range ledgers {
		ledgerByID[ledger.ID] = ledger
	}
	repaidByID := make(map[uuid.UUID]int64, len(loans))
	repayments, err := models.Transfers.Query(sm.Where(psql.And(
		models.Transfers.Columns.HouseholdID.In(householdArgs...), models.Transfers.Columns.LoanID.In(loanArgs...), models.Transfers.Columns.Kind.EQ(psql.Arg("REPAYMENT")),
	))).All(ctx, executor)
	if err != nil {
		return nil, err
	}
	for _, repayment := range repayments {
		if !repayment.LoanID.Valid {
			continue
		}
		total, ok := checkedAdd(repaidByID[repayment.LoanID.V], repayment.AmountYen)
		if !ok {
			return nil, fmt.Errorf("loan %s repayment total overflow", repayment.LoanID.V)
		}
		repaidByID[repayment.LoanID.V] = total
	}
	for _, loan := range loans {
		ledger := ledgerByID[loan.LedgerEntryID]
		if ledger == nil {
			return nil, fmt.Errorf("loan %s has no ledger entry", loan.ID)
		}
		globalID, err := encodeNodeID(nodeTypeLoanBalance, loan.ID.String())
		if err != nil {
			return nil, err
		}
		repaid := repaidByID[loan.ID]
		result[loan.ID] = &model.LoanBalance{ID: globalID, LenderMemberID: loan.FromMemberID.String(), BorrowerMemberID: loan.ToMemberID.String(), PrincipalYen: graphLong(loan.AmountYen), RepaidYen: graphLong(repaid), BalanceYen: graphLong(loan.AmountYen - repaid), CreatedAt: timeString(ledger.CreatedAt)}
	}
	return result, nil
}

func (b *nodeBatch) loadFinancialNodes(ctx context.Context) error {
	executor := b.executor
	households, idsByKind := b.households, b.idsByKind
	householdArgs, args, set := b.householdArgs, b.ids, b.set
	r := b.resolver
	if values := idsByKind[nodeTypeFundingSchedule]; len(values) > 0 {
		rows, err := models.FundingSchedules.Query(sm.Where(psql.And(
			models.FundingSchedules.Columns.HouseholdID.In(householdArgs...), models.FundingSchedules.Columns.ID.In(args(nodeTypeFundingSchedule)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, row := range rows {
			set(nodeTypeFundingSchedule, row.ID.String(), fundingScheduleModel(row))
		}
	}
	if values := idsByKind[nodeTypeLoanBalance]; len(values) > 0 {
		loans, err := models.Transfers.Query(sm.Where(psql.And(
			models.Transfers.Columns.HouseholdID.In(householdArgs...), models.Transfers.Columns.ID.In(args(nodeTypeLoanBalance)...),
			models.Transfers.Columns.Kind.EQ(psql.Arg(transferKindLoan)),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		ledgerIDs := make([]bob.Expression, 0, len(loans))
		loanIDs := make([]bob.Expression, 0, len(loans))
		loanByID := make(map[uuid.UUID]*models.Transfer, len(loans))
		for _, loan := range loans {
			ledgerIDs = append(ledgerIDs, psql.Arg(loan.LedgerEntryID))
			loanIDs = append(loanIDs, psql.Arg(sql.Null[uuid.UUID]{V: loan.ID, Valid: true}))
			loanByID[loan.ID] = loan
		}
		ledgerByID := make(map[uuid.UUID]*models.LedgerEntry, len(loans))
		if len(ledgerIDs) > 0 {
			ledgers, err := models.LedgerEntries.Query(sm.Where(models.LedgerEntries.Columns.ID.In(ledgerIDs...))).All(ctx, executor)
			if err != nil {
				return err
			}
			for _, ledger := range ledgers {
				ledgerByID[ledger.ID] = ledger
			}
		}
		repaidByID := make(map[uuid.UUID]int64, len(loans))
		if len(loanIDs) > 0 {
			repayments, err := models.Transfers.Query(sm.Where(psql.And(
				models.Transfers.Columns.HouseholdID.In(householdArgs...), models.Transfers.Columns.LoanID.In(loanIDs...),
				models.Transfers.Columns.Kind.EQ(psql.Arg("REPAYMENT")),
			))).All(ctx, executor)
			if err != nil {
				return err
			}
			for _, repayment := range repayments {
				if !repayment.LoanID.Valid {
					continue
				}
				total, ok := checkedAdd(repaidByID[repayment.LoanID.V], repayment.AmountYen)
				if !ok {
					return fmt.Errorf("loan %s repayment total overflow", repayment.LoanID.V)
				}
				repaidByID[repayment.LoanID.V] = total
			}
		}
		for id, loan := range loanByID {
			ledger := ledgerByID[loan.LedgerEntryID]
			if ledger == nil {
				return fmt.Errorf("loan %s has no ledger entry", id)
			}
			globalID, err := encodeNodeID(nodeTypeLoanBalance, id.String())
			if err != nil {
				return err
			}
			repaid := repaidByID[id]
			set(nodeTypeLoanBalance, id.String(), &model.LoanBalance{
				ID: globalID, LenderMemberID: loan.FromMemberID.String(), BorrowerMemberID: loan.ToMemberID.String(),
				PrincipalYen: graphLong(loan.AmountYen), RepaidYen: graphLong(repaid),
				BalanceYen: graphLong(loan.AmountYen - repaid), CreatedAt: timeString(ledger.CreatedAt),
			})
		}
	}
	if values := idsByKind[nodeTypeBudget]; len(values) > 0 {
		budgets, err := models.Budgets.Query(sm.Where(psql.And(
			models.Budgets.Columns.HouseholdID.In(householdArgs...), models.Budgets.Columns.ID.In(args(nodeTypeBudget)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		month := time.Now().UTC()
		start, end := month, month.AddDate(0, 1, 0)
		purchases, err := models.Purchases.Query(sm.Where(psql.And(
			models.Purchases.Columns.HouseholdID.In(householdArgs...), models.Purchases.Columns.OccurredAt.GTE(psql.Arg(start)), models.Purchases.Columns.OccurredAt.LT(psql.Arg(end)),
			psql.Or(models.Purchases.Columns.Status.EQ(psql.Arg(statusPosted)), models.Purchases.Columns.Status.EQ(psql.Arg(statusClosed))),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		purchaseIDs := make([]bob.Expression, 0, len(purchases))
		for _, purchase := range purchases {
			purchaseIDs = append(purchaseIDs, psql.Arg(purchase.ID))
		}
		actualByScope := make(map[string]int64)
		itemCategoryByID := make(map[uuid.UUID]sql.Null[uuid.UUID])
		if len(purchaseIDs) > 0 {
			items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.In(purchaseIDs...))).All(ctx, executor)
			if err != nil {
				return err
			}
			purchaseHouseholdByID := make(map[uuid.UUID]uuid.UUID, len(purchases))
			for _, purchase := range purchases {
				purchaseHouseholdByID[purchase.ID] = purchase.HouseholdID
			}
			for _, item := range items {
				itemCategoryByID[item.ID] = item.CategoryID
				householdID := purchaseHouseholdByID[item.PurchaseID]
				keys := []string{budgetActualScope(householdID, sql.Null[uuid.UUID]{})}
				if item.CategoryID.Valid {
					keys = append(keys, budgetActualScope(householdID, item.CategoryID))
				}
				for _, key := range keys {
					total, ok := checkedAdd(actualByScope[key], item.AmountYen)
					if !ok {
						return fmt.Errorf("%w: budget actual exceeds int64", ErrBadInput)
					}
					actualByScope[key] = total
				}
			}
			corrections, err := models.Corrections.Query(sm.Where(models.Corrections.Columns.PurchaseID.In(purchaseIDs...))).All(ctx, executor)
			if err != nil {
				return err
			}
			for _, correction := range corrections {
				var categoryID sql.Null[uuid.UUID]
				if correction.ItemID.Valid {
					categoryID = itemCategoryByID[correction.ItemID.V]
				}
				householdID := purchaseHouseholdByID[correction.PurchaseID]
				keys := []string{budgetActualScope(householdID, sql.Null[uuid.UUID]{})}
				if categoryID.Valid {
					keys = append(keys, budgetActualScope(householdID, categoryID))
				}
				for _, key := range keys {
					total, ok := checkedAdd(actualByScope[key], correction.AmountDeltaYen)
					if !ok {
						return fmt.Errorf("%w: budget actual exceeds int64", ErrBadInput)
					}
					actualByScope[key] = total
				}
			}
		}
		for _, row := range budgets {
			value := &model.Budget{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, AmountYen: graphLong(row.AmountYen), StartsOn: model.Date(row.StartsOn.Format(time.DateOnly)), Archived: row.Archived, Revision: int(row.Revision)}
			if row.CategoryID.Valid {
				value.CategoryID = new(row.CategoryID.V.String())
			}
			if row.EndsOn.Valid {
				endDate := row.EndsOn.V.Format(time.DateOnly)
				value.EndsOn = datePointer(&endDate)
			}
			actual := actualByScope[budgetActualScope(row.HouseholdID, row.CategoryID)]
			value.ActualYen = graphLong(actual)
			value.RemainingYen = graphLong(row.AmountYen - actual)
			set(nodeTypeBudget, value.ID, value)
		}
	}
	if values := idsByKind[nodeTypeAccountMovement]; len(values) > 0 {
		rows, err := models.AccountMovements.Query(sm.Where(psql.And(
			models.AccountMovements.Columns.HouseholdID.In(householdArgs...), models.AccountMovements.Columns.ID.In(args(nodeTypeAccountMovement)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, row := range rows {
			set(nodeTypeAccountMovement, row.ID.String(), accountMovementModel(row))
		}
	}
	if values := idsByKind[nodeTypeAccountIncome]; len(values) > 0 {
		rows, err := models.AccountIncomes.Query(sm.Where(psql.And(
			models.AccountIncomes.Columns.HouseholdID.In(householdArgs...), models.AccountIncomes.Columns.ID.In(args(nodeTypeAccountIncome)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		for _, row := range rows {
			set(nodeTypeAccountIncome, row.ID.String(), accountIncomeModel(row))
		}
	}
	if values := idsByKind[nodeTypeLedgerEntry]; len(values) > 0 {
		entries, err := models.LedgerEntries.Query(sm.Where(psql.And(
			models.LedgerEntries.Columns.HouseholdID.In(householdArgs...), models.LedgerEntries.Columns.ID.In(args(nodeTypeLedgerEntry)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		ledgerIDs := make([]bob.Expression, 0, len(entries))
		purchaseIDs := make([]bob.Expression, 0, len(entries))
		purchaseSet := make(map[uuid.UUID]struct{})
		for _, entry := range entries {
			ledgerIDs = append(ledgerIDs, psql.Arg(entry.ID))
			if entry.SourcePurchaseID.Valid {
				purchaseSet[entry.SourcePurchaseID.V] = struct{}{}
			}
		}
		for id := range purchaseSet {
			purchaseIDs = append(purchaseIDs, psql.Arg(id))
		}
		purchasesByID := make(map[uuid.UUID]*model.Purchase)
		if len(purchaseIDs) > 0 {
			purchaseRows, err := models.Purchases.Query(sm.Where(psql.And(models.Purchases.Columns.HouseholdID.In(householdArgs...), models.Purchases.Columns.ID.In(purchaseIDs...)))).All(ctx, executor)
			if err != nil {
				return err
			}
			purchases, err := loadPurchases(ctx, r.DB, purchaseRows)
			if err != nil {
				return err
			}
			for _, purchase := range purchases {
				id, _ := uuid.FromString(purchase.ID)
				purchasesByID[id] = purchase
			}
		}
		transfersByLedger := make(map[uuid.UUID]*models.Transfer)
		var transfers models.TransferSlice
		if len(ledgerIDs) > 0 {
			transfers, err = models.Transfers.Query(sm.Where(models.Transfers.Columns.LedgerEntryID.In(ledgerIDs...))).All(ctx, executor)
			if err != nil {
				return err
			}
			for _, transfer := range transfers {
				transfersByLedger[transfer.LedgerEntryID] = transfer
			}
		}
		membersByHousehold := make(map[string]map[string]*model.Member, len(households))
		poolsByHousehold := make(map[string]map[string]*model.Pool, len(households))
		for _, household := range households {
			membersByHousehold[household.ID] = make(map[string]*model.Member, len(household.Members))
			for _, member := range household.Members {
				membersByHousehold[household.ID][member.ID] = member
			}
			poolsByHousehold[household.ID] = make(map[string]*model.Pool, len(household.PoolVariants))
			for _, pool := range household.PoolVariants {
				if value := poolData(pool); value != nil {
					poolsByHousehold[household.ID][value.ID] = value
				}
			}
		}
		loanRows := make(models.TransferSlice, 0)
		loanIDs := make([]bob.Expression, 0)
		for _, transfer := range transfers {
			if transfer.LoanID.Valid {
				loanIDs = append(loanIDs, psql.Arg(transfer.LoanID))
			} else if transfer.Kind == transferKindLoan {
				loanIDs = append(loanIDs, psql.Arg(transfer.ID))
			}
		}
		if len(loanIDs) > 0 {
			loanRows, err = models.Transfers.Query(sm.Where(psql.And(models.Transfers.Columns.HouseholdID.In(householdArgs...), models.Transfers.Columns.ID.In(loanIDs...), models.Transfers.Columns.Kind.EQ(psql.Arg(transferKindLoan))))).All(ctx, executor)
			if err != nil {
				return err
			}
		}
		loansByID, err := r.batchLoanBalances(ctx, households, loanRows)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			id, occurredAt, description, amount, createdAt := entry.ID.String(), timeString(entry.OccurredAt), entry.Description, graphLong(entry.AmountYen), timeString(entry.CreatedAt)
			switch entry.Kind {
			case sourceKindPurchase, sourceKindCorrection:
				if !entry.SourcePurchaseID.Valid || purchasesByID[entry.SourcePurchaseID.V] == nil {
					return fmt.Errorf("ledger entry %s has no source purchase", entry.ID)
				}
				if entry.Kind == sourceKindPurchase {
					set(nodeTypeLedgerEntry, id, &model.PurchaseLedgerEntry{ID: id, OccurredAt: occurredAt, Description: description, AmountYen: amount, Currency: currencyJPY, CreatedAt: createdAt, Purchase: purchasesByID[entry.SourcePurchaseID.V]})
				} else {
					set(nodeTypeLedgerEntry, id, &model.CorrectionLedgerEntry{ID: id, OccurredAt: occurredAt, Description: description, AmountYen: amount, Currency: currencyJPY, CreatedAt: createdAt, Purchase: purchasesByID[entry.SourcePurchaseID.V]})
				}
			case sourceKindFunding:
				householdID := entry.HouseholdID.String()
				if !entry.FromMemberID.Valid || !entry.PoolID.Valid || membersByHousehold[householdID][entry.FromMemberID.V.String()] == nil || poolsByHousehold[householdID][entry.PoolID.V.String()] == nil {
					return errors.New("funding ledger entry is missing a member or pool")
				}
				set(nodeTypeLedgerEntry, id, &model.FundingLedgerEntry{ID: id, OccurredAt: occurredAt, Description: description, AmountYen: amount, Currency: currencyJPY, CreatedAt: createdAt, Member: membersByHousehold[householdID][entry.FromMemberID.V.String()], Pool: poolVariant(poolsByHousehold[householdID][entry.PoolID.V.String()])})
			case sourceKindTransfer:
				transfer := transfersByLedger[entry.ID]
				householdID := entry.HouseholdID.String()
				if transfer == nil || !entry.FromMemberID.Valid || !entry.ToMemberID.Valid || membersByHousehold[householdID][entry.FromMemberID.V.String()] == nil || membersByHousehold[householdID][entry.ToMemberID.V.String()] == nil {
					return errors.New("transfer ledger entry is incomplete")
				}
				var loan *model.LoanBalance
				switch transfer.Kind {
				case transferKindLoan:
					loan = loansByID[transfer.ID]
				case transferKindRepayment:
					if !transfer.LoanID.Valid {
						return errors.New("repayment transfer has no loan")
					}
					loan = loansByID[transfer.LoanID.V]
				case transferKindAdvance, transferKindGift:
					if transfer.LoanID.Valid {
						return fmt.Errorf("%s transfer unexpectedly references a loan", transfer.Kind)
					}
				default:
					return fmt.Errorf("unknown transfer kind %q", transfer.Kind)
				}
				variant, err := transferLedgerEntryVariant(transfer.Kind, id, occurredAt, description, amount, currencyJPY, createdAt, membersByHousehold[householdID][entry.FromMemberID.V.String()], membersByHousehold[householdID][entry.ToMemberID.V.String()], loan)
				if err != nil {
					return err
				}
				set(nodeTypeLedgerEntry, id, variant)
			default:
				return fmt.Errorf("unknown ledger entry kind %q", entry.Kind)
			}
		}
	}
	return nil
}
