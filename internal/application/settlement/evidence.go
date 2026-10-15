package settlement

import (
	"cmp"
	"context"
	"slices"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type Evidence struct {
	MemberID   string
	Kind       string
	Label      string
	AmountYen  int64
	SourceID   string
	PurchaseID string
	OccurredAt *time.Time
}

type evidenceKey struct {
	householdID gofrsuuid.UUID
	period      string
}

func settlementEvidenceMany(ctx context.Context, db bob.Executor, requests []GetRequest) (map[evidenceKey][]Evidence, error) {
	keys := make([]evidenceKey, 0, len(requests))
	households := make(map[gofrsuuid.UUID]struct{}, len(requests))
	periods := make(map[string]struct{}, len(requests))
	startByKey, endByKey := make(map[evidenceKey]time.Time, len(requests)), make(map[evidenceKey]time.Time, len(requests))
	var minStart, maxEnd time.Time
	for _, request := range requests {
		householdID, err := gofrsuuid.FromString(request.HouseholdID)
		if err != nil {
			return nil, err
		}
		start, err := parsePeriod(request.Period)
		if err != nil {
			return nil, err
		}
		key := evidenceKey{householdID: householdID, period: request.Period}
		if _, found := startByKey[key]; !found {
			keys = append(keys, key)
		}
		end := start.AddDate(0, 1, 0)
		startByKey[key], endByKey[key] = start, end
		households[householdID] = struct{}{}
		periods[request.Period] = struct{}{}
		if minStart.IsZero() || start.Before(minStart) {
			minStart = start
		}
		if end.After(maxEnd) {
			maxEnd = end
		}
	}
	result := make(map[evidenceKey][]Evidence, len(keys))
	if len(keys) == 0 {
		return result, nil
	}
	householdIDs := make([]gofrsuuid.UUID, 0, len(households))
	for id := range households {
		householdIDs = append(householdIDs, id)
	}
	slices.SortFunc(householdIDs, func(left, right gofrsuuid.UUID) int { return cmp.Compare(left.String(), right.String()) })
	householdArgs := make([]bob.Expression, len(householdIDs))
	for index, id := range householdIDs {
		householdArgs[index] = psql.Arg(id)
	}
	periodValues := make([]string, 0, len(periods))
	for period := range periods {
		periodValues = append(periodValues, period)
	}
	slices.Sort(periodValues)
	periodArgs := make([]bob.Expression, len(periodValues))
	for index, period := range periodValues {
		periodArgs[index] = psql.Arg(period)
	}

	obligations, err := models.FundingObligations.Query(sm.Where(psql.And(
		models.FundingObligations.Columns.HouseholdID.In(householdArgs...),
		models.FundingObligations.Columns.Period.In(periodArgs...),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	accountingPeriods, err := models.AccountingPeriods.Query(sm.Where(psql.And(
		models.AccountingPeriods.Columns.HouseholdID.In(householdArgs...),
		models.AccountingPeriods.Columns.Period.In(periodArgs...),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	closedPeriods := make(map[evidenceKey]bool, len(accountingPeriods))
	for _, accountingPeriod := range accountingPeriods {
		closedPeriods[evidenceKey{householdID: accountingPeriod.HouseholdID, period: accountingPeriod.Period}] = accountingPeriod.Status == closedStatus
	}
	schedules, err := models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.In(householdArgs...),
		psql.Or(models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)), models.FundingSchedules.Columns.EndsOn.IsNotNull()),
		models.FundingSchedules.Columns.StartsOn.LT(psql.Arg(maxEnd)),
		psql.Or(models.FundingSchedules.Columns.EndsOn.IsNull(), models.FundingSchedules.Columns.EndsOn.GTE(psql.Arg(minStart))),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	type fundingGroup struct {
		householdID gofrsuuid.UUID
		period      string
		memberID    string
		poolID      gofrsuuid.UUID
	}
	scheduleTotals := make(map[fundingGroup]int64)
	for _, key := range keys {
		if closedPeriods[key] {
			continue
		}
		start, end := startByKey[key], endByKey[key]
		for _, schedule := range schedules {
			if schedule.HouseholdID != key.householdID || !schedule.StartsOn.Before(end) || schedule.EndsOn.Valid && schedule.EndsOn.V.Before(start) || !schedule.Active && !schedule.EndsOn.Valid {
				continue
			}
			amount, err := fundingScheduleAmount(start, end, schedule)
			if err != nil {
				return nil, err
			}
			if amount == 0 {
				continue
			}
			group := fundingGroup{householdID: key.householdID, period: key.period, memberID: schedule.MemberID.String(), poolID: schedule.PoolID}
			total, ok := checkedAdd(scheduleTotals[group], amount)
			if !ok {
				return nil, domain.ErrInvalidSettlement
			}
			scheduleTotals[group] = total
			result[key] = append(result[key], Evidence{MemberID: group.memberID, Kind: "SCHEDULED", Label: "入金予定", AmountYen: amount, SourceID: schedule.ID.String()})
		}
	}
	for _, obligation := range obligations {
		key := evidenceKey{householdID: obligation.HouseholdID, period: obligation.Period}
		group := fundingGroup{householdID: key.householdID, period: key.period, memberID: obligation.MemberID.String(), poolID: obligation.PoolID}
		if closedPeriods[key] {
			result[key] = append(result[key], Evidence{MemberID: group.memberID, Kind: "SCHEDULED", Label: "確定済み入金予定", AmountYen: obligation.AmountYen, SourceID: obligation.ID.String()})
			continue
		}
		if obligation.AmountYen > scheduleTotals[group] {
			result[key] = append(result[key], Evidence{MemberID: group.memberID, Kind: "SCHEDULED", Label: "確定済み入金予定", AmountYen: obligation.AmountYen - scheduleTotals[group], SourceID: obligation.ID.String()})
		}
	}
	ledger, err := models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.HouseholdID.In(householdArgs...),
		models.LedgerEntries.Columns.Kind.EQ(psql.Arg("FUNDING")),
		models.LedgerEntries.Columns.OccurredAt.GTE(psql.Arg(minStart)),
		models.LedgerEntries.Columns.OccurredAt.LT(psql.Arg(maxEnd)),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, entry := range ledger {
		if !entry.FromMemberID.Valid {
			continue
		}
		for _, key := range keys {
			if entry.HouseholdID != key.householdID || entry.OccurredAt.Before(startByKey[key]) || !entry.OccurredAt.Before(endByKey[key]) {
				continue
			}
			occurredAt := entry.OccurredAt
			result[key] = append(result[key], Evidence{MemberID: entry.FromMemberID.V.String(), Kind: "PAID", Label: entry.Description, AmountYen: entry.AmountYen, SourceID: entry.ID.String(), OccurredAt: &occurredAt})
		}
	}
	purchases, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.In(householdArgs...),
		models.Purchases.Columns.Status.In(psql.Arg("POSTED"), psql.Arg("CLOSED")),
		models.Purchases.Columns.OccurredAt.GTE(psql.Arg(minStart)),
		models.Purchases.Columns.OccurredAt.LT(psql.Arg(maxEnd)),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	purchaseIDs := make([]bob.Expression, 0, len(purchases))
	purchaseByID := make(map[gofrsuuid.UUID]*models.Purchase, len(purchases))
	for _, purchase := range purchases {
		purchaseIDs = append(purchaseIDs, psql.Arg(purchase.ID))
		purchaseByID[purchase.ID] = purchase
	}
	if len(purchaseIDs) > 0 {
		burdens, err := models.PurchaseMemberBurdens.Query(sm.Where(psql.And(
			models.PurchaseMemberBurdens.Columns.HouseholdID.In(householdArgs...),
			models.PurchaseMemberBurdens.Columns.PurchaseID.In(purchaseIDs...),
		))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		items, err := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.HouseholdID.In(householdArgs...),
			models.PurchaseItems.Columns.PurchaseID.In(purchaseIDs...),
		))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		itemByID := make(map[gofrsuuid.UUID]*models.PurchaseItem, len(items))
		for _, item := range items {
			itemByID[item.ID] = item
		}
		type itemBurden struct {
			purchaseID gofrsuuid.UUID
			itemID     gofrsuuid.UUID
			memberID   string
			amount     int64
		}
		burdenByItem := make(map[string]itemBurden)
		for _, burden := range burdens {
			groupID := burden.PurchaseID.String() + ":" + burden.ItemID.String() + ":" + burden.MemberID.String()
			group := burdenByItem[groupID]
			group.purchaseID, group.itemID, group.memberID = burden.PurchaseID, burden.ItemID, burden.MemberID.String()
			amount, ok := checkedAdd(group.amount, burden.AmountYen)
			if !ok {
				return nil, domain.ErrInvalidSettlement
			}
			group.amount = amount
			burdenByItem[groupID] = group
		}
		for _, burden := range burdenByItem {
			purchase, item := purchaseByID[burden.purchaseID], itemByID[burden.itemID]
			if purchase == nil || item == nil {
				continue
			}
			for _, key := range keys {
				if purchase.HouseholdID != key.householdID || purchase.OccurredAt.Before(startByKey[key]) || !purchase.OccurredAt.Before(endByKey[key]) {
					continue
				}
				occurredAt := purchase.OccurredAt
				result[key] = append(result[key], Evidence{MemberID: burden.memberID, Kind: "PURCHASE", Label: purchase.Merchant + "・" + item.Name, AmountYen: burden.amount, SourceID: item.ID.String(), PurchaseID: purchase.ID.String(), OccurredAt: &occurredAt})
			}
		}
	}
	adjustments, err := models.SettlementAdjustments.Query(sm.Where(psql.And(
		models.SettlementAdjustments.Columns.HouseholdID.In(householdArgs...),
		models.SettlementAdjustments.Columns.Period.In(periodArgs...),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, adjustment := range adjustments {
		key := evidenceKey{householdID: adjustment.HouseholdID, period: adjustment.Period}
		occurredAt := adjustment.CreatedAt
		result[key] = append(result[key], Evidence{MemberID: adjustment.MemberID.String(), Kind: "ADJUSTMENT", Label: adjustment.SourceKind, AmountYen: adjustment.AmountYen, SourceID: adjustment.SourceID.String(), OccurredAt: &occurredAt})
	}
	transferEntries, err := models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.HouseholdID.In(householdArgs...),
		models.LedgerEntries.Columns.Kind.EQ(psql.Arg("TRANSFER")),
		models.LedgerEntries.Columns.OccurredAt.GTE(psql.Arg(minStart)),
		models.LedgerEntries.Columns.OccurredAt.LT(psql.Arg(maxEnd)),
	))).All(ctx, db)
	if err != nil {
		return nil, err
	}
	transferIDs := make([]bob.Expression, 0, len(transferEntries))
	entryByTransferID := make(map[gofrsuuid.UUID]*models.LedgerEntry, len(transferEntries))
	for _, entry := range transferEntries {
		transferIDs = append(transferIDs, psql.Arg(entry.ID))
		entryByTransferID[entry.ID] = entry
	}
	var transfers models.TransferSlice
	if len(transferIDs) > 0 {
		transfers, err = models.Transfers.Query(sm.Where(psql.And(
			models.Transfers.Columns.HouseholdID.In(householdArgs...),
			models.Transfers.Columns.LedgerEntryID.In(transferIDs...),
			models.Transfers.Columns.Kind.EQ(psql.Arg("ADVANCE")),
		))).All(ctx, db)
		if err != nil {
			return nil, err
		}
	}
	for _, transfer := range transfers {
		entry := entryByTransferID[transfer.LedgerEntryID]
		for _, key := range keys {
			if entry == nil || transfer.HouseholdID != key.householdID || entry.OccurredAt.Before(startByKey[key]) || !entry.OccurredAt.Before(endByKey[key]) {
				continue
			}
			occurredAt := entry.OccurredAt
			result[key] = append(result[key], Evidence{MemberID: transfer.ToMemberID.String(), Kind: "ADVANCE", Label: transfer.Description, AmountYen: transfer.AmountYen, SourceID: transfer.ID.String(), OccurredAt: &occurredAt})
		}
	}
	for key := range result {
		slices.SortFunc(result[key], func(left, right Evidence) int {
			if comparison := cmp.Compare(left.MemberID, right.MemberID); comparison != 0 {
				return comparison
			}
			if comparison := cmp.Compare(left.Kind, right.Kind); comparison != 0 {
				return comparison
			}
			if left.OccurredAt != nil && right.OccurredAt != nil {
				if comparison := left.OccurredAt.Compare(*right.OccurredAt); comparison != 0 {
					return comparison
				}
			}
			return cmp.Compare(left.SourceID, right.SourceID)
		})
	}
	return result, nil
}
