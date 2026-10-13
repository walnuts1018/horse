package graph

import (
	"context"
	"math"
	"time"

	"github.com/stephenafamo/bob"
	poolapp "github.com/walnuts1018/horse/internal/application/pool"
	purchaseapp "github.com/walnuts1018/horse/internal/application/purchase"
	settlementapp "github.com/walnuts1018/horse/internal/application/settlement"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
)

type accountingDB interface {
	bob.Executor
}

var ErrUnattributedPurchasePayment = purchaseapp.ErrUnattributedPurchasePayment
var ErrMissingPurchaseFundingSnapshot = settlementapp.ErrMissingPurchaseFundingSnapshot

var accountingLocation = time.FixedZone("JST", 9*60*60)

func accountingPeriod(value time.Time) string { return value.In(accountingLocation).Format("2006-01") }

func parseAccountingPeriod[T ~string](value T) (time.Time, error) {
	return time.ParseInLocation("2006-01", string(value), accountingLocation)
}

func (r *mutationResolver) setPoolFundingRule(ctx context.Context, input model.SetPoolFundingRuleInput) (*model.Pool, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	command := poolapp.SetFundingRuleCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, PoolID: input.PoolID,
		ExpectedVersion: input.ExpectedVersion,
	}
	if configuration := input.Configuration; configuration != nil {
		if configuration.Personal != nil {
			command.Personal = true
			command.PersonalOwnerID = configuration.Personal.OwnerMemberID
		}
		if configuration.Shared != nil {
			command.Shared = true
			command.SettlementPolicy = string(configuration.Shared.SettlementPolicy)
			for _, share := range configuration.Shared.FundingShares {
				if share == nil {
					command.FundingShares = append(command.FundingShares, poolapp.FundingShare{})
					continue
				}
				command.FundingShares = append(command.FundingShares, poolapp.FundingShare{MemberID: share.MemberID, Weight: int64(share.Weight)})
			}
		}
	}
	if err := poolapp.NewService(r.DB).SetFundingRule(ctx, command); err != nil {
		return nil, err
	}
	householdResult, err := loadHousehold(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	for _, resultPool := range householdResult.PoolVariants {
		if value := poolData(resultPool); value != nil && value.ID == input.PoolID {
			return value, nil
		}
	}
	return nil, ErrNotFound
}

func calculatePeriodAccounting(ctx context.Context, db accountingDB, householdID, period string) ([]domain.SettlementAmount, error) {
	return settlementapp.CalculatePeriod(ctx, db, householdID, period)
}

func checkedAdd(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right || right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}
