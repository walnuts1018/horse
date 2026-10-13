package graph

import (
	"os"
	"strings"
	"testing"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestCreateHouseholdApplicationService(t *testing.T) {
	t.Parallel()
	const placeholderName = "Future member"
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

	ownerID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	ownerSubject := "household-creation-test:" + ownerID.String()
	ownerName := "Household owner"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &ownerID, OidcSubject: &ownerSubject, DisplayName: &ownerName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(ownerID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()

	ownerContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: ownerID.String(), Subject: ownerSubject, DisplayName: ownerName})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	result, err := resolver.CreateHousehold(ownerContext, model.CreateHouseholdInput{Name: "  Household creation test  ", MemberNames: []string{"  " + placeholderName + "  "}})
	if err != nil {
		t.Fatal(err)
	}
	success, ok := result.(*model.CreateHouseholdSuccess)
	if !ok {
		t.Fatalf("CreateHousehold() result type = %T, want success", result)
	}
	household := success.Result
	householdID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	placeholderIDs := make([]gofrsuuid.UUID, 0, len(household.Members))
	defer func() {
		for _, placeholderID := range placeholderIDs {
			_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(placeholderID)))).Exec(t.Context(), bobpgx.NewPool(db))
		}
	}()
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()

	ownerMembership, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(ownerID)),
	))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if ownerMembership.Role != "OWNER" {
		t.Fatalf("owner membership role = %q, want OWNER", ownerMembership.Role)
	}
	if len(household.Members) != 2 {
		t.Fatalf("created household member count = %d, want owner and placeholder", len(household.Members))
	}
	var placeholder *model.Member
	for _, member := range household.Members {
		if member.ID != ownerID.String() {
			placeholder = member
		}
	}
	if placeholder == nil || placeholder.DisplayName != placeholderName {
		t.Fatalf("created placeholder member = %#v, want trimmed %q", placeholder, placeholderName)
	}
	placeholderID, err := gofrsuuid.FromString(placeholder.ID)
	if err != nil {
		t.Fatal(err)
	}
	placeholderIDs = append(placeholderIDs, placeholderID)
	placeholderRecord, err := models.Members.Query(sm.Where(models.Members.Columns.ID.EQ(psql.Arg(placeholderID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if placeholderRecord.OidcSubject != "invite:"+placeholder.ID {
		t.Fatalf("placeholder subject = %q, want invite:%s", placeholderRecord.OidcSubject, placeholder.ID)
	}
	if _, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(placeholderID)),
	))).One(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatalf("placeholder household membership was not created: %v", err)
	}
	createdEvent, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdEvents.Columns.Type.EQ(psql.Arg("household.created")),
	))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if createdEvent.EntityID != householdID || createdEvent.Revision != 1 || !createdEvent.ActorID.Valid || createdEvent.ActorID.V != ownerID || string(createdEvent.ChangedFields.Val) != "[]" {
		t.Fatalf("created event has wrong household, revision, or actor: %#v", createdEvent)
	}

	invalidInputs := []model.CreateHouseholdInput{
		{Name: "  "},
		{Name: "Household A", MemberNames: make([]string, 13)},
		{Name: "Household B", MemberNames: []string{"  "}},
		{Name: "Household C", MemberNames: []string{strings.Repeat("m", 121)}},
	}
	for _, input := range invalidInputs {
		invalidResult, err := resolver.CreateHousehold(ownerContext, input)
		if err != nil {
			t.Fatal(err)
		}
		userError, ok := invalidResult.(*model.CreateHouseholdUserError)
		if !ok || userError.Code != model.MutationErrorCodeBadInput {
			t.Fatalf("invalid CreateHousehold() result = %#v, want BAD_INPUT user error", invalidResult)
		}
	}
}
