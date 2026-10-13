package graph

import (
	"context"
	"errors"
	"os"
	"testing"

	"uuid"

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

func TestTagConnectionPaginatesWithinHouseholdAndFiltersArchivedTags(t *testing.T) {
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
	t.Cleanup(db.Close)
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	memberID := uuid.NewV7().String()
	memberUUID, err := gofrsuuid.FromString(memberID)
	if err != nil {
		t.Fatal(err)
	}
	subject := "tag-connection-integration:" + memberID
	displayName := "tag connection integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	resolver := &queryResolver{Resolver: &Resolver{DB: db}}
	mutation := &mutationResolver{Resolver: resolver.Resolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "tag connection integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	var tags = make(map[string]*model.Tag, 3)
	for _, name := range []string{"A", "B", "C"} {
		tag, err := mutation.createTag(ctx, model.CreateTagInput{HouseholdID: household.ID, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		tags[name] = tag
	}
	if _, err := mutation.archiveTag(ctx, tags["B"].ID, tags["B"].Revision, true); err != nil {
		t.Fatal(err)
	}

	pageSize := 1
	firstPage, err := resolver.tagConnection(ctx, household.ID, false, nil, nil, &pageSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if firstPage.TotalCount != 2 || len(firstPage.Edges) != 1 || firstPage.Edges[0].Node.Name != "A" || firstPage.PageInfo.EndCursor == nil || !firstPage.PageInfo.HasNextPage || firstPage.PageInfo.HasPreviousPage {
		t.Fatalf("unexpected first active-tag page: %#v", firstPage)
	}
	secondPage, err := resolver.tagConnection(ctx, household.ID, false, firstPage.PageInfo.EndCursor, nil, &pageSize, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Edges) != 1 || secondPage.Edges[0].Node.Name != "C" || secondPage.PageInfo.HasNextPage || !secondPage.PageInfo.HasPreviousPage {
		t.Fatalf("unexpected second active-tag page: %#v", secondPage)
	}
	lastPage, err := resolver.tagConnection(ctx, household.ID, false, nil, nil, nil, &pageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(lastPage.Edges) != 1 || lastPage.Edges[0].Node.Name != "C" || !lastPage.PageInfo.HasPreviousPage || lastPage.PageInfo.HasNextPage {
		t.Fatalf("unexpected last active-tag page: %#v", lastPage)
	}
	allTags, err := resolver.tagConnection(ctx, household.ID, true, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if allTags.TotalCount != 3 || len(allTags.Edges) != 3 || allTags.Edges[1].Node.Name != "B" || !allTags.Edges[1].Node.Archived {
		t.Fatalf("includeArchived did not return all tags in stable order: %#v", allTags)
	}

	unauthorizedID := uuid.NewV7().String()
	unauthorizedCtx := auth.WithPrincipal(ctx, auth.Principal{MemberID: unauthorizedID, Subject: "tag-unauthorized"})
	if _, err := mutation.updateTag(unauthorizedCtx, model.UpdateTagInput{TagID: tags["A"].ID, BaseRevision: tags["A"].Revision, Name: "Unauthorized"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unauthorized tag update error = %v; want ErrNotFound", err)
	}

	updatedTag, err := mutation.updateTag(ctx, model.UpdateTagInput{TagID: tags["A"].ID, BaseRevision: tags["A"].Revision, Name: "A updated"})
	if err != nil {
		t.Fatal(err)
	}
	if updatedTag.Name != "A updated" || updatedTag.Revision != tags["A"].Revision+1 {
		t.Fatalf("updated tag = %#v; want name A updated at revision 2", updatedTag)
	}
	if _, err := mutation.archiveTag(ctx, updatedTag.ID, tags["A"].Revision, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale tag archive error = %v; want ErrConflict", err)
	}
	archivedTag, err := mutation.archiveTag(ctx, updatedTag.ID, updatedTag.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if !archivedTag.Archived || archivedTag.Revision != updatedTag.Revision+1 {
		t.Fatalf("archived tag = %#v; want archived at revision 3", archivedTag)
	}
	events, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdEvents.Columns.EntityID.EQ(psql.Arg(generatedUUID(tags["A"].ID))),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("tag lifecycle events = %#v; want created, updated, and archived events", events)
	}
	eventsByType := make(map[string]*models.HouseholdEvent, len(events))
	for _, event := range events {
		eventsByType[event.Type] = event
	}
	updatedEvent := eventsByType["tag.updated"]
	archivedEvent := eventsByType["tag.archived"]
	if updatedEvent == nil || updatedEvent.Revision != 2 || string(updatedEvent.ChangedFields.Val) != `["name"]` || !updatedEvent.ActorID.Valid || updatedEvent.ActorID.V != memberUUID {
		t.Fatalf("tag.updated event = %#v; want revision 2 with name field", updatedEvent)
	}
	if archivedEvent == nil || archivedEvent.Revision != 3 || string(archivedEvent.ChangedFields.Val) != `["archived"]` || !archivedEvent.ActorID.Valid || archivedEvent.ActorID.V != memberUUID {
		t.Fatalf("tag.archived event = %#v; want revision 3 with archived field", archivedEvent)
	}

	otherHousehold, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "other tag connection integration"})
	if err != nil {
		t.Fatal(err)
	}
	otherHouseholdUUID, err := gofrsuuid.FromString(otherHousehold.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(otherHouseholdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	foreignTag, err := mutation.createTag(ctx, model.CreateTagInput{HouseholdID: otherHousehold.ID, Name: "Foreign"})
	if err != nil {
		t.Fatal(err)
	}
	foreignCursor := encodeCursor(nodeTypeTag, foreignTag.Name+"\x00"+foreignTag.ID)
	if _, err := resolver.tagConnection(ctx, household.ID, true, &foreignCursor, nil, &pageSize, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign household cursor error = %v, want ErrNotFound", err)
	}
}
