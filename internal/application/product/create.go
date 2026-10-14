package product

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/inputvalidation"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type CreateConceptCommand struct {
	ActorID     string
	HouseholdID string
	Name        string `validate:"notblank,max=240"`
	CategoryID  *string
}

type CreatedConcept struct {
	ID string
}

func (s *Service) CreateConcept(ctx context.Context, command CreateConceptCommand) (CreatedConcept, error) {
	name := strings.TrimSpace(command.Name)
	command.Name = name
	if err := inputvalidation.Check(command); err != nil {
		return CreatedConcept{}, err
	}
	normalizedName := normalizeText(name)
	householdID, err := parseProductID(command.HouseholdID, apperror.KindBadInput)
	if err != nil {
		return CreatedConcept{}, err
	}
	actorID, err := parseProductID(command.ActorID, apperror.KindNotFound)
	if err != nil {
		return CreatedConcept{}, err
	}
	var categoryID sql.Null[gofrsuuid.UUID]
	if command.CategoryID != nil {
		id, err := parseProductID(*command.CategoryID, apperror.KindBadInput)
		if err != nil {
			return CreatedConcept{}, err
		}
		categoryID = sql.Null[gofrsuuid.UUID]{V: id, Valid: true}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreatedConcept{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := pgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForShare()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return CreatedConcept{}, apperror.ErrNotFound
	} else if err != nil {
		return CreatedConcept{}, err
	}
	if err := ensureProductMember(ctx, executor, householdID, actorID); err != nil {
		return CreatedConcept{}, err
	}
	if categoryID.Valid {
		if _, err := models.Categories.Query(sm.Where(psql.And(
			models.Categories.Columns.ID.EQ(psql.Arg(categoryID.V)),
			models.Categories.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
			return CreatedConcept{}, apperror.ErrNotFound
		} else if err != nil {
			return CreatedConcept{}, err
		}
	}

	id, err := gofrsuuid.NewV7()
	if err != nil {
		return CreatedConcept{}, fmt.Errorf("generate product concept id: %w", err)
	}
	if _, err := models.ProductConcepts.Insert(&models.ProductConceptSetter{
		ID: &id, HouseholdID: &householdID, Name: &name, NormalizedName: &normalizedName, CategoryID: &categoryID,
	}).Exec(ctx, executor); err != nil {
		return CreatedConcept{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, id, "product.concept.created", 1, []string{"name", "categoryId"}, actorID); err != nil {
		return CreatedConcept{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedConcept{}, err
	}
	return CreatedConcept{ID: id.String()}, nil
}

type CreateVariantCommand struct {
	ActorID     string
	HouseholdID string
	ConceptID   string
	Name        string `validate:"notblank,max=240"`
	Brand       *string
	PackageSize *string
	Barcode     *string `validate:"omitempty,notblank"`
}

type CreatedVariant struct {
	ID string
}

func (s *Service) CreateVariant(ctx context.Context, command CreateVariantCommand) (CreatedVariant, error) {
	name := strings.TrimSpace(command.Name)
	command.Name = name
	if err := inputvalidation.Check(command); err != nil {
		return CreatedVariant{}, err
	}
	normalizedName := normalizeText(name)
	householdID, err := parseProductID(command.HouseholdID, apperror.KindBadInput)
	if err != nil {
		return CreatedVariant{}, err
	}
	actorID, err := parseProductID(command.ActorID, apperror.KindNotFound)
	if err != nil {
		return CreatedVariant{}, err
	}
	conceptID, err := parseProductID(command.ConceptID, apperror.KindNotFound)
	if err != nil {
		return CreatedVariant{}, err
	}
	brand := trimProductOptional(command.Brand)
	packageSize := trimProductOptional(command.PackageSize)
	var barcode sql.Null[string]
	if command.Barcode != nil {
		barcode = sql.Null[string]{V: strings.TrimSpace(*command.Barcode), Valid: true}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreatedVariant{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := pgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForShare()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return CreatedVariant{}, apperror.ErrNotFound
	} else if err != nil {
		return CreatedVariant{}, err
	}
	if err := ensureProductMember(ctx, executor, householdID, actorID); err != nil {
		return CreatedVariant{}, err
	}
	if _, err := models.ProductConcepts.Query(sm.Where(psql.And(
		models.ProductConcepts.Columns.ID.EQ(psql.Arg(conceptID)),
		models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		psql.Not(models.ProductConcepts.Columns.Archived),
	))).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return CreatedVariant{}, apperror.ErrNotFound
	} else if err != nil {
		return CreatedVariant{}, err
	}

	id, err := gofrsuuid.NewV7()
	if err != nil {
		return CreatedVariant{}, fmt.Errorf("generate product variant id: %w", err)
	}
	if _, err := models.ProductVariants.Insert(&models.ProductVariantSetter{
		ID: &id, HouseholdID: &householdID, ConceptID: &conceptID, Name: &name, NormalizedName: &normalizedName,
		Brand: &brand, PackageSize: &packageSize, Barcode: &barcode,
	}).Exec(ctx, executor); err != nil {
		return CreatedVariant{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, id, "product.variant.created", 1, []string{"name", "conceptId"}, actorID); err != nil {
		return CreatedVariant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedVariant{}, err
	}
	return CreatedVariant{ID: id.String()}, nil
}

func ensureProductMember(ctx context.Context, executor bob.Executor, householdID, actorID gofrsuuid.UUID) error {
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
		models.HouseholdMembers.Columns.LeftAt.IsNull(),
	))).Exists(ctx, executor)
	if err != nil {
		return err
	}
	if !member {
		return apperror.ErrNotFound
	}
	return nil
}

func parseProductID(value string, kind apperror.Kind) (gofrsuuid.UUID, error) {
	id, err := gofrsuuid.FromString(value)
	if err != nil {
		return gofrsuuid.Nil, apperror.Wrap(kind, fmt.Errorf("invalid product identifier: %w", err))
	}
	return id, nil
}

func trimProductOptional(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
