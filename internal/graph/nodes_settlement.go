package graph

import (
	"cmp"
	"context"
	"slices"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	settlementapp "github.com/walnuts1018/horse/internal/application/settlement"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type settlementNodeKey struct {
	householdID string
	period      string
}

func (r *queryResolver) loadSettlementNodes(ctx context.Context, nodeIDs []decodedNodeID) ([]model.Node, error) {
	result := make([]model.Node, len(nodeIDs))
	keys := make([]settlementNodeKey, len(nodeIDs))
	uniqueKeys := make(map[settlementNodeKey]struct{})
	householdUUIDs := make(map[uuid.UUID]struct{})
	periods := make(map[string]struct{})
	for index, nodeID := range nodeIDs {
		if nodeID.kind != nodeTypeSettlement {
			continue
		}
		householdID, period, err := decodeSettlementNodeID(nodeID.rawID)
		if err != nil {
			return nil, err
		}
		householdUUID, _ := uuid.FromString(householdID)
		keys[index] = settlementNodeKey{householdID: householdID, period: period}
		uniqueKeys[keys[index]] = struct{}{}
		householdUUIDs[householdUUID] = struct{}{}
		periods[period] = struct{}{}
	}
	if len(uniqueKeys) == 0 {
		return result, nil
	}

	households, err := r.householdReferences(ctx)
	if err != nil {
		return nil, err
	}
	accessible := make(map[uuid.UUID]struct{}, len(households))
	for _, household := range households {
		accessible[mustUUID(household.ID)] = struct{}{}
	}
	for householdID := range householdUUIDs {
		if _, ok := accessible[householdID]; !ok {
			return nil, ErrNotFound
		}
	}

	queryHouseholds := make([]bob.Expression, 0, len(householdUUIDs))
	for householdID := range householdUUIDs {
		queryHouseholds = append(queryHouseholds, psql.Arg(householdID))
	}
	queryPeriods := make([]bob.Expression, 0, len(periods))
	for period := range periods {
		queryPeriods = append(queryPeriods, psql.Arg(period))
	}
	closedStatus := settlementStatusClosed
	runs, err := models.SettlementRuns.Query(sm.Where(psql.And(
		models.SettlementRuns.Columns.HouseholdID.In(queryHouseholds...),
		models.SettlementRuns.Columns.Period.In(queryPeriods...),
		models.SettlementRuns.Columns.Status.EQ(psql.Arg(closedStatus)),
	))).All(ctx, queryExecutor(r.DB))
	if err != nil {
		return nil, err
	}
	runsByKey := make(map[settlementNodeKey]*models.SettlementRun, len(runs))
	for _, run := range runs {
		key := settlementNodeKey{householdID: run.HouseholdID.String(), period: run.Period}
		current := runsByKey[key]
		if current == nil || current.Revision < run.Revision {
			runsByKey[key] = run
		}
	}

	runIDs := make([]bob.Expression, 0, len(runsByKey))
	for _, run := range runsByKey {
		runIDs = append(runIDs, psql.Arg(run.ID))
	}
	linesByRun := make(map[uuid.UUID][]*models.SettlementLine, len(runIDs))
	namesByHousehold := make(map[uuid.UUID]map[uuid.UUID]string, len(householdUUIDs))
	if len(runIDs) > 0 {
		lines, err := models.SettlementLines.Query(sm.Where(models.SettlementLines.Columns.SettlementRunID.In(runIDs...))).All(ctx, queryExecutor(r.DB))
		if err != nil {
			return nil, err
		}
		for _, line := range lines {
			linesByRun[line.SettlementRunID] = append(linesByRun[line.SettlementRunID], line)
		}
		members, err := models.HouseholdMembers.Query(sm.Where(models.HouseholdMembers.Columns.HouseholdID.In(queryHouseholds...))).All(ctx, queryExecutor(r.DB))
		if err != nil {
			return nil, err
		}
		if err := members.LoadMember(ctx, queryExecutor(r.DB)); err != nil {
			return nil, err
		}
		for _, member := range members {
			if namesByHousehold[member.HouseholdID] == nil {
				namesByHousehold[member.HouseholdID] = make(map[uuid.UUID]string)
			}
			namesByHousehold[member.HouseholdID][member.MemberID] = member.R.Member.DisplayName
		}
	}

	settlements := make(map[settlementNodeKey]model.Node, len(uniqueKeys))
	for key := range uniqueKeys {
		if run := runsByKey[key]; run != nil {
			value := settlementapp.Settlement{
				HouseholdID: key.householdID, Period: key.period, Status: run.Status,
				Revision: run.Revision, CreatedAt: &run.CreatedAt,
			}
			lines := linesByRun[run.ID]
			names := namesByHousehold[run.HouseholdID]
			slices.SortFunc(lines, func(left, right *models.SettlementLine) int {
				return cmp.Compare(names[left.MemberID], names[right.MemberID])
			})
			value.Lines = make([]domain.SettlementAmount, 0, len(lines))
			for _, line := range lines {
				value.Lines = append(value.Lines, domain.SettlementAmount{
					MemberID: line.MemberID.String(), MemberName: names[line.MemberID], ScheduledYen: line.ScheduledYen,
					PaidYen: line.PaidYen, CarryYen: line.CarryYen, MonthlyAdjustmentYen: line.MonthlyAdjustmentYen,
					AdvanceCreditYen: line.AdvanceCreditYen, AmountDueYen: line.AmountDueYen,
				})
			}
			node, err := settlementModel(value)
			if err != nil {
				return nil, err
			}
			settlements[key] = node
			continue
		}
		value, err := settlementapp.New(r.DB).Get(ctx, key.householdID, key.period)
		if err != nil {
			return nil, mapSettlementError(err)
		}
		node, err := settlementModel(value)
		if err != nil {
			return nil, err
		}
		settlements[key] = node
	}
	for index, nodeID := range nodeIDs {
		if nodeID.kind == nodeTypeSettlement {
			result[index] = settlements[keys[index]]
		}
	}
	return result, nil
}
