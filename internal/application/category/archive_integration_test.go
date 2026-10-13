package category

import (
	"errors"
	"os"
	"testing"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestSetArchivedChecksMembershipAndRecordsOnlyStateChanges(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	executor := bobpgx.NewPool(db)
	memberID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	outsiderID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	householdID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	categoryID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	memberName, outsiderName := "Category member", "Category outsider"
	memberSubject, outsiderSubject := "category-member:"+memberID.String(), "category-outsider:"+outsiderID.String()
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &memberSubject, DisplayName: &memberName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &outsiderID, OidcSubject: &outsiderSubject, DisplayName: &outsiderName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.In(psql.Arg(memberID), psql.Arg(outsiderID)))).Exec(t.Context(), executor)
	}()
	householdName, categoryName := "Category archive integration", "Groceries"
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberID}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.Categories.Insert(&models.CategorySetter{ID: &categoryID, HouseholdID: &householdID, Name: &categoryName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	service := NewService(db)
	command := SetArchivedCommand{ActorID: memberID.String(), CategoryID: categoryID.String(), Archived: true}
	for range 2 {
		result, err := service.SetArchived(ctx, command)
		if err != nil || !result.Archived || result.ID != categoryID.String() || result.Name != categoryName {
			t.Fatalf("SetArchived(true) = %#v, %v", result, err)
		}
	}
	command.Archived = false
	result, err := service.SetArchived(ctx, command)
	if err != nil || result.Archived {
		t.Fatalf("SetArchived(false) = %#v, %v", result, err)
	}
	if _, err := service.SetArchived(ctx, SetArchivedCommand{ActorID: outsiderID.String(), CategoryID: categoryID.String(), Archived: true}); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("unauthorized SetArchived() error = %v, want not found", err)
	}
	events, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdEvents.Columns.EntityID.EQ(psql.Arg(categoryID)),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	eventTypes := make(map[string]int, len(events))
	for _, event := range events {
		eventTypes[event.Type]++
	}
	if len(events) != 2 || eventTypes["category.archived"] != 1 || eventTypes["category.unarchived"] != 1 {
		t.Fatalf("category events = %#v, want one archive and one unarchive event", eventTypes)
	}
}
