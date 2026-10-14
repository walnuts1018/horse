package product

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/inputvalidation"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

type SetAliasCommand struct {
	ActorID     string
	HouseholdID string
	VariantID   string
	Merchant    string `validate:"notblank,max=240"`
	Alias       string `validate:"notblank,max=240"`
}

type Alias struct {
	ID string
}

func (s *Service) SetAlias(ctx context.Context, command SetAliasCommand) (Alias, error) {
	merchantKey := normalizeText(command.Merchant)
	command.Merchant = merchantKey
	command.Alias = strings.TrimSpace(command.Alias)
	if err := inputvalidation.Check(command); err != nil {
		return Alias{}, err
	}
	alias := command.Alias
	normalizedAlias := normalizeText(alias)
	householdID, err := parseID(command.HouseholdID)
	if err != nil {
		return Alias{}, apperror.Wrap(apperror.KindNotFound, err)
	}
	variantID, err := parseID(command.VariantID)
	if err != nil {
		return Alias{}, apperror.Wrap(apperror.KindNotFound, err)
	}
	actorID, err := parseID(command.ActorID)
	if err != nil {
		return Alias{}, apperror.Wrap(apperror.KindNotFound, err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Alias{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return Alias{}, apperror.ErrNotFound
	} else if err != nil {
		return Alias{}, err
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
		models.HouseholdMembers.Columns.LeftAt.IsNull(),
	))).Exists(ctx, executor)
	if err != nil {
		return Alias{}, err
	}
	if !member {
		return Alias{}, apperror.ErrNotFound
	}
	if _, err := models.ProductVariants.Query(sm.Where(psql.And(
		models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.ProductVariants.Columns.ID.EQ(psql.Arg(variantID)),
		psql.Not(models.ProductVariants.Columns.Archived),
	))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return Alias{}, apperror.ErrNotFound
	} else if err != nil {
		return Alias{}, err
	}
	id, err := gofrsuuid.NewV7()
	if err != nil {
		return Alias{}, fmt.Errorf("generate product alias id: %w", err)
	}
	row, err := models.ProductAliases.Insert(
		&models.ProductAliasSetter{ID: &id, HouseholdID: &householdID, VariantID: &variantID, MerchantKey: &merchantKey, AliasText: &alias, NormalizedAlias: &normalizedAlias},
		im.OnConflict("household_id", "merchant_key", "normalized_alias").DoUpdate(
			im.SetCol("variant_id").To(im.Excluded("variant_id")),
			im.SetCol("alias_text").To(im.Excluded("alias_text")),
			im.SetCol("revision").To(models.ProductAliases.Columns.Revision.Plus(psql.Arg(1))),
			im.SetCol("updated_at").To(psql.F("now")),
		),
	).One(ctx, executor)
	if err != nil {
		return Alias{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, row.ID, "product.alias.learned", row.Revision, []string{"alias", "variantId", "merchant"}, actorID); err != nil {
		return Alias{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Alias{}, err
	}
	return Alias{ID: row.ID.String()}, nil
}

func parseID(value string) (gofrsuuid.UUID, error) {
	return gofrsuuid.FromString(value)
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
