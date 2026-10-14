package allocation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

type Weight struct {
	PoolID string
	Value  int64
}

type SetDefaultCommand struct {
	ActorID     string
	HouseholdID string
	Weights     []Weight
}

type RuleTarget struct {
	CategoryID       *string
	ProductConceptID *string
	ProductVariantID *string
	Merchant         *string
}

type SetRuleCommand struct {
	ActorID     string
	HouseholdID string
	Target      RuleTarget
	Weights     []Weight
}

func (s *Service) SetDefault(ctx context.Context, command SetDefaultCommand) error {
	actorID, err := parseID(command.ActorID, "actor")
	if err != nil {
		return apperror.Wrap(apperror.KindBadInput, err)
	}
	householdID, err := parseID(command.HouseholdID, "household")
	if err != nil {
		return apperror.Wrap(apperror.KindBadInput, err)
	}
	weights, err := parseWeights(command.Weights)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	household, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := requireMember(ctx, executor, householdID, actorID); err != nil {
		return err
	}
	for _, weight := range weights {
		poolID, err := parseID(weight.PoolID, "pool")
		if err != nil {
			return apperror.Wrap(apperror.KindBadInput, err)
		}
		if _, err := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Pools.Columns.ID.EQ(psql.Arg(poolID)),
		))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return apperror.ErrNotFound
		} else if err != nil {
			return err
		}
	}

	version := household.DefaultAllocationVersion + 1
	for _, weight := range weights {
		poolID, _ := gofrsuuid.FromString(weight.PoolID)
		weightValue := weight.Value
		if _, err := models.AllocationDefaults.Insert(&models.AllocationDefaultSetter{
			HouseholdID: &householdID, Version: &version, PoolID: &poolID, Weight: &weightValue,
		}).Exec(ctx, executor); err != nil {
			return err
		}
	}
	revision, updatedAt := household.Revision+1, time.Now().UTC()
	if _, err := models.Households.Update((&models.HouseholdSetter{
		DefaultAllocationVersion: &version, Revision: &revision, UpdatedAt: &updatedAt,
	}).UpdateMod(), um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(ctx, executor); err != nil {
		return err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, householdID, "allocation.default.updated", version, []string{"defaultAllocation"}, actorID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) SetRule(ctx context.Context, command SetRuleCommand) (string, error) {
	actorID, err := parseID(command.ActorID, "actor")
	if err != nil {
		return "", apperror.Wrap(apperror.KindBadInput, err)
	}
	householdID, err := parseID(command.HouseholdID, "household")
	if err != nil {
		return "", apperror.Wrap(apperror.KindBadInput, err)
	}
	target, merchantKey, err := validateTarget(command.Target)
	if err != nil {
		return "", err
	}
	weights, err := parseWeights(command.Weights)
	if err != nil {
		return "", err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return "", apperror.ErrNotFound
	} else if err != nil {
		return "", err
	}
	if err := requireMember(ctx, executor, householdID, actorID); err != nil {
		return "", err
	}
	for _, weight := range weights {
		poolID, err := parseID(weight.PoolID, "pool")
		if err != nil {
			return "", apperror.Wrap(apperror.KindBadInput, err)
		}
		if _, err := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Pools.Columns.ID.EQ(psql.Arg(poolID)),
		))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return "", apperror.ErrNotFound
		} else if err != nil {
			return "", err
		}
	}
	if target.CategoryID != nil {
		id, _ := gofrsuuid.FromString(*target.CategoryID)
		if _, err := models.Categories.Query(sm.Where(psql.And(models.Categories.Columns.ID.EQ(psql.Arg(id)), models.Categories.Columns.HouseholdID.EQ(psql.Arg(householdID))))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return "", apperror.ErrNotFound
		} else if err != nil {
			return "", err
		}
	}
	if target.ProductConceptID != nil {
		id, _ := gofrsuuid.FromString(*target.ProductConceptID)
		if _, err := models.ProductConcepts.Query(sm.Where(psql.And(models.ProductConcepts.Columns.ID.EQ(psql.Arg(id)), models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdID))))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return "", apperror.ErrNotFound
		} else if err != nil {
			return "", err
		}
	}
	if target.ProductVariantID != nil {
		id, _ := gofrsuuid.FromString(*target.ProductVariantID)
		if _, err := models.ProductVariants.Query(sm.Where(psql.And(models.ProductVariants.Columns.ID.EQ(psql.Arg(id)), models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID))))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return "", apperror.ErrNotFound
		} else if err != nil {
			return "", err
		}
	}

	rule, err := findRule(ctx, executor, householdID, target, merchantKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var ruleID gofrsuuid.UUID
	var revision int64 = 1
	if errors.Is(err, sql.ErrNoRows) {
		ruleID, err = gofrsuuid.NewV7()
		if err != nil {
			return "", fmt.Errorf("generate allocation rule id: %w", err)
		}
		categoryID, conceptID, variantID := nullableUUID(target.CategoryID), nullableUUID(target.ProductConceptID), nullableUUID(target.ProductVariantID)
		merchant := sql.Null[string]{V: merchantKey, Valid: target.Merchant != nil}
		if _, err := models.AllocationRules.Insert(&models.AllocationRuleSetter{
			ID: &ruleID, HouseholdID: &householdID, CategoryID: &categoryID, ProductConceptID: &conceptID,
			ProductVariantID: &variantID, MerchantKey: &merchant, Revision: &revision,
		}).Exec(ctx, executor); err != nil {
			return "", err
		}
	} else {
		ruleID, revision = rule.ID, rule.Revision+1
		updatedAt := time.Now().UTC()
		if _, err := models.AllocationRules.Update((&models.AllocationRuleSetter{Revision: &revision, UpdatedAt: &updatedAt}).UpdateMod(), um.Where(models.AllocationRules.Columns.ID.EQ(psql.Arg(ruleID)))).Exec(ctx, executor); err != nil {
			return "", err
		}
		if _, err := models.AllocationRuleWeights.Delete(dm.Where(models.AllocationRuleWeights.Columns.RuleID.EQ(psql.Arg(ruleID)))).Exec(ctx, executor); err != nil {
			return "", err
		}
	}
	for _, weight := range weights {
		poolID, _ := gofrsuuid.FromString(weight.PoolID)
		weightValue := weight.Value
		if _, err := models.AllocationRuleWeights.Insert(&models.AllocationRuleWeightSetter{
			HouseholdID: &householdID, RuleID: &ruleID, PoolID: &poolID, Weight: &weightValue,
		}).Exec(ctx, executor); err != nil {
			return "", err
		}
	}
	fields := []string{"allocations"}
	switch {
	case target.ProductVariantID != nil:
		fields = append(fields, "productVariantId")
	case target.ProductConceptID != nil:
		fields = append(fields, "productConceptId")
	case target.CategoryID != nil:
		fields = append(fields, "categoryId")
	default:
		fields = append(fields, "merchant")
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, ruleID, "allocation.rule.updated", revision, fields, actorID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return ruleID.String(), nil
}

func parseWeights(input []Weight) ([]domain.Weight, error) {
	weights := make([]domain.Weight, 0, len(input))
	for _, weight := range input {
		if weight.Value <= 0 {
			return nil, apperror.Wrap(apperror.KindBadInput, errors.New("allocation weights must be positive"))
		}
		if _, err := parseID(weight.PoolID, "pool"); err != nil {
			return nil, apperror.Wrap(apperror.KindBadInput, err)
		}
		weights = append(weights, domain.Weight{PoolID: weight.PoolID, Value: weight.Value})
	}
	if _, err := domain.Allocate(1, weights); err != nil {
		return nil, apperror.Wrap(apperror.KindBadInput, err)
	}
	return weights, nil
}

func validateTarget(target RuleTarget) (RuleTarget, string, error) {
	selected := 0
	for _, value := range []*string{target.ProductVariantID, target.ProductConceptID, target.CategoryID, target.Merchant} {
		if value != nil {
			selected++
		}
	}
	if selected != 1 {
		return RuleTarget{}, "", apperror.Wrap(apperror.KindBadInput, errors.New("select exactly one allocation rule target"))
	}
	for _, value := range []*string{target.CategoryID, target.ProductConceptID, target.ProductVariantID} {
		if value != nil {
			if _, err := parseID(*value, "allocation rule reference"); err != nil {
				return RuleTarget{}, "", apperror.Wrap(apperror.KindBadInput, err)
			}
		}
	}
	merchantKey := ""
	if target.Merchant != nil {
		merchantKey = normalizeMerchant(*target.Merchant)
		if merchantKey == "" || len(merchantKey) > 240 {
			return RuleTarget{}, "", apperror.Wrap(apperror.KindBadInput, errors.New("invalid merchant rule key"))
		}
	}
	return target, merchantKey, nil
}

func findRule(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, target RuleTarget, merchantKey string) (*models.AllocationRule, error) {
	where := models.AllocationRules.Columns.CategoryID
	var id gofrsuuid.UUID
	if target.CategoryID != nil {
		id, _ = gofrsuuid.FromString(*target.CategoryID)
	} else if target.ProductConceptID != nil {
		where = models.AllocationRules.Columns.ProductConceptID
		id, _ = gofrsuuid.FromString(*target.ProductConceptID)
	} else if target.ProductVariantID != nil {
		where = models.AllocationRules.Columns.ProductVariantID
		id, _ = gofrsuuid.FromString(*target.ProductVariantID)
	} else if target.Merchant != nil {
		return models.AllocationRules.Query(sm.Where(psql.And(
			models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.AllocationRules.Columns.MerchantKey.EQ(psql.Arg(merchantKey)),
		)), sm.ForUpdate()).One(ctx, executor)
	}
	return models.AllocationRules.Query(sm.Where(psql.And(
		models.AllocationRules.Columns.HouseholdID.EQ(psql.Arg(householdID)), where.EQ(psql.Arg(id)),
	)), sm.ForUpdate()).One(ctx, executor)
}

func nullableUUID(value *string) sql.Null[gofrsuuid.UUID] {
	if value == nil {
		return sql.Null[gofrsuuid.UUID]{}
	}
	id, _ := gofrsuuid.FromString(*value)
	return sql.Null[gofrsuuid.UUID]{V: id, Valid: true}
}

func requireMember(ctx context.Context, executor bob.Executor, householdID, actorID gofrsuuid.UUID) error {
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
	))).Exists(ctx, executor)
	if err != nil {
		return err
	}
	if !member {
		return apperror.ErrNotFound
	}
	return nil
}

func normalizeMerchant(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func parseID(value, resource string) (gofrsuuid.UUID, error) {
	id, err := gofrsuuid.FromString(value)
	if err != nil {
		return gofrsuuid.Nil, fmt.Errorf("invalid %s id", resource)
	}
	return id, nil
}
