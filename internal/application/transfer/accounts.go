package transfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const (
	paymentAccountRole     = "payment"
	destinationAccountRole = "destination"
)

type transferAccount struct {
	id       gofrsuuid.UUID
	poolID   gofrsuuid.UUID
	poolKind string
}

func loadTransferAccount(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, value *string, ownerID gofrsuuid.UUID, role string) (*transferAccount, error) {
	if value == nil {
		return nil, nil
	}
	id, err := parseID(*value, role+" account")
	if err != nil {
		return nil, err
	}
	account, err := models.Accounts.Query(sm.Where(psql.And(
		models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Accounts.Columns.ID.EQ(psql.Arg(id)),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if account.Kind == "CARD" {
		if role == paymentAccountRole {
			return nil, fmt.Errorf("%w: transfer source must be a non-card account", ErrBadInput)
		}
		return nil, fmt.Errorf("%w: transfer destination must be a non-card account assigned to a pool", ErrBadInput)
	}
	if !account.PoolID.Valid {
		if role == paymentAccountRole {
			return nil, fmt.Errorf("%w: payment account is not assigned to a pool", ErrBadInput)
		}
		return nil, fmt.Errorf("%w: transfer destination must be a non-card account assigned to a pool", ErrBadInput)
	}
	pool, err := models.Pools.Query(sm.Where(psql.And(
		models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Pools.Columns.ID.EQ(psql.Arg(account.PoolID.V)),
	))).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if pool.Kind == "PERSONAL" {
		fundingRule, err := models.PoolFundingRules.Query(sm.Where(psql.And(
			models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(pool.ID)),
			models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
		))).One(ctx, executor)
		if err != nil {
			return nil, err
		}
		if !fundingRule.OwnerMemberID.Valid || fundingRule.OwnerMemberID.V != ownerID {
			if role == paymentAccountRole {
				return nil, fmt.Errorf("%w: personal payment account must belong to the source member", ErrBadInput)
			}
			return nil, fmt.Errorf("%w: personal destination account must belong to the receiving member", ErrBadInput)
		}
	}
	return &transferAccount{id: id, poolID: account.PoolID.V, poolKind: pool.Kind}, nil
}
