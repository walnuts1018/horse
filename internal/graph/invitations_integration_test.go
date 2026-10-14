package graph

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestHouseholdInvitationLifecycle(t *testing.T) {
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

	ownerID, memberID, otherID := newID(), newID(), newID()
	for id, name := range map[string]string{ownerID: "Owner", memberID: "Member", otherID: "Other"} {
		memberUUID, _ := gofrsuuid.FromString(id)
		subject := "invitation-test:" + id
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(psql.Or(
			models.Members.Columns.ID.EQ(psql.Arg(ownerID)), models.Members.Columns.ID.EQ(psql.Arg(memberID)), models.Members.Columns.ID.EQ(psql.Arg(otherID)),
		))).Exec(t.Context(), bobpgx.NewPool(db))
	}()

	resolver := &mutationResolver{Resolver: (&Resolver{DB: db})}
	ownerCtx := auth.WithPrincipal(ctx, auth.Principal{MemberID: ownerID, Subject: "invitation-test:" + ownerID, DisplayName: "Owner"})
	memberCtx := auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: "invitation-test:" + memberID, DisplayName: "Member"})
	otherCtx := auth.WithPrincipal(ctx, auth.Principal{MemberID: otherID, Subject: "invitation-test:" + otherID, DisplayName: "Other"})
	household, err := resolver.createHousehold(ownerCtx, model.CreateHouseholdInput{Name: "invitation-test", MemberNames: []string{"Future member"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		householdUUID, _ := gofrsuuid.FromString(household.ID)
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	otherHousehold, err := resolver.createHousehold(otherCtx, model.CreateHouseholdInput{Name: "other-invitation-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		householdUUID, _ := gofrsuuid.FromString(otherHousehold.ID)
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	var placeholderID string
	for _, member := range household.Members {
		if member.DisplayName == "Future member" {
			placeholderID = member.ID
		}
	}
	if placeholderID == "" {
		t.Fatal("household placeholder member was not created")
	}
	for _, expiresAt := range []time.Time{time.Now().Add(-time.Minute), time.Now().Add(31 * 24 * time.Hour)} {
		_, err := resolver.createHouseholdInvitation(ownerCtx, model.CreateHouseholdInvitationInput{
			HouseholdID: household.ID, ExpiresAt: model.DateTime(expiresAt.Format(time.RFC3339Nano)),
		})
		if !errors.Is(err, ErrBadInput) {
			t.Fatalf("invalid invitation expiry must be rejected: expiresAt=%s error=%v", expiresAt, err)
		}
	}

	createInvitation := func(expiresAt time.Time) *model.CreatedHouseholdInvitation {
		t.Helper()
		invitation, err := resolver.createHouseholdInvitation(ownerCtx, model.CreateHouseholdInvitationInput{HouseholdID: household.ID, ExpiresAt: model.DateTime(expiresAt.Format(time.RFC3339Nano))})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := invitation.Invitation.(*model.PendingHouseholdInvitation); !ok || len(invitation.Token) != 43 {
			t.Fatalf("unexpected invitation: type=%T token length=%d", invitation.Invitation, len(invitation.Token))
		}
		return invitation
	}

	active := createInvitation(time.Now().Add(24 * time.Hour))
	rawToken, err := base64.RawURLEncoding.DecodeString(active.Token)
	if err != nil {
		t.Fatal(err)
	}
	expectedHash := sha256.Sum256(rawToken)
	var storedHash []byte
	activeID, _ := gofrsuuid.FromString(active.Invitation.GetID())
	storedInvitation, err := models.HouseholdInvitations.Query(sm.Where(models.HouseholdInvitations.Columns.ID.EQ(psql.Arg(activeID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if storedInvitation.Role != "MEMBER" {
		t.Fatalf("invitation role must be fixed to MEMBER, got %q", storedInvitation.Role)
	}
	storedHash = storedInvitation.TokenHash
	if !bytes.Equal(storedHash, expectedHash[:]) {
		t.Fatal("database must store only the SHA256 invitation token hash")
	}
	householdUUID, _ := gofrsuuid.FromString(household.ID)
	ownerUUID, _ := gofrsuuid.FromString(ownerID)
	createdEvent, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdEvents.Columns.Type.EQ(psql.Arg("household.invitation.created")),
		models.HouseholdEvents.Columns.EntityID.EQ(psql.Arg(activeID)),
	))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if createdEvent.Revision != 2 || !createdEvent.ActorID.Valid || createdEvent.ActorID.V != ownerUUID || string(createdEvent.ChangedFields.Val) != `["invitations"]` {
		t.Fatalf("invitation event has wrong revision, actor, or changed fields: %#v", createdEvent)
	}
	ownerQuery := (&Resolver{DB: db}).Query()
	listed, err := ownerQuery.HouseholdInvitations(ownerCtx, household.ID, nil, nil, nil, nil)
	if err != nil || len(listed.Edges) != 1 || listed.Edges[0].Node.GetID() != active.Invitation.GetID() {
		t.Fatalf("owner invitation list failed: invitations=%#v error=%v", listed, err)
	}
	if _, ok := listed.Edges[0].Node.(*model.PendingHouseholdInvitation); !ok {
		t.Fatalf("pending invitation variant was not returned: type=%T", listed.Edges[0].Node)
	}
	if _, err := ownerQuery.HouseholdInvitations(memberCtx, household.ID, nil, nil, nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner must not list invitations: error=%v", err)
	}
	if _, err := ownerQuery.HouseholdInvitations(ownerCtx, otherHousehold.ID, nil, nil, nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("owner must not list another household's invitations: error=%v", err)
	}
	if _, err := resolver.createHouseholdInvitation(memberCtx, model.CreateHouseholdInvitationInput{HouseholdID: household.ID, ExpiresAt: model.DateTime(time.Now().Add(time.Hour).Format(time.RFC3339Nano))}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner must not create invitations: error=%v", err)
	}
	if _, err := resolver.revokeHouseholdInvitation(ownerCtx, otherHousehold.ID, active.Invitation.GetID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invitation from another household must not be revoked: error=%v", err)
	}
	if _, err := resolver.acceptHouseholdInvitation(memberCtx, active.Token); err != nil {
		t.Fatalf("valid invitation was not accepted: %v", err)
	}
	listed, err = ownerQuery.HouseholdInvitations(ownerCtx, household.ID, nil, nil, nil, nil)
	if err != nil || len(listed.Edges) != 1 {
		t.Fatalf("accepted invitation was not returned: invitations=%#v error=%v", listed, err)
	}
	if _, ok := listed.Edges[0].Node.(*model.AcceptedHouseholdInvitation); !ok {
		t.Fatalf("accepted invitation variant was not returned: type=%T", listed.Edges[0].Node)
	}
	memberUUID, _ := gofrsuuid.FromString(memberID)
	memberRows, err := models.HouseholdMembers.Query(sm.Where(psql.And(models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberUUID)), models.HouseholdMembers.Columns.Role.EQ(psql.S("MEMBER"))))).All(ctx, bobpgx.NewPool(db))
	memberCount := len(memberRows)
	if err != nil || memberCount != 1 {
		t.Fatalf("accepted user membership was not created: count=%d error=%v", memberCount, err)
	}
	placeholderUUID, _ := gofrsuuid.FromString(placeholderID)
	_, placeholderErr := models.HouseholdMembers.Query(sm.Where(psql.And(models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(placeholderUUID))))).One(ctx, bobpgx.NewPool(db))
	placeholderExists := placeholderErr == nil
	if placeholderErr != nil && !errors.Is(placeholderErr, sql.ErrNoRows) {
		t.Fatal(placeholderErr)
	}
	if !placeholderExists {
		t.Fatalf("joining user must not replace historical placeholder membership: exists=%v error=%v", placeholderExists, err)
	}
	if _, err := resolver.acceptHouseholdInvitation(memberCtx, active.Token); !errors.Is(err, ErrConflict) {
		t.Fatalf("used invitation must be rejected: error=%v", err)
	}
	if _, err := resolver.acceptHouseholdInvitation(otherCtx, active.Token); !errors.Is(err, ErrConflict) {
		t.Fatalf("accepted invitation must not be reused by another user: error=%v", err)
	}

	expired := createInvitation(time.Now().Add(24 * time.Hour))
	expiredID, _ := gofrsuuid.FromString(expired.Invitation.GetID())
	expiredAt := time.Now().Add(-time.Second)
	if _, err := models.HouseholdInvitations.Update(um.Where(models.HouseholdInvitations.Columns.ID.EQ(psql.Arg(expiredID))), um.SetCol("expires_at").To(psql.Arg(expiredAt))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.acceptHouseholdInvitation(otherCtx, expired.Token); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired invitation must be rejected: error=%v", err)
	}
	listed, err = ownerQuery.HouseholdInvitations(ownerCtx, household.ID, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	expiredStatusFound := false
	for _, edge := range listed.Edges {
		invitation := edge.Node
		if invitation.GetID() == expired.Invitation.GetID() {
			if _, ok := invitation.(*model.ExpiredHouseholdInvitation); !ok {
				continue
			}
			expiredStatusFound = true
			break
		}
	}
	if !expiredStatusFound {
		t.Fatalf("expired invitation status was not returned: invitations=%#v", listed)
	}

	revoked := createInvitation(time.Now().Add(24 * time.Hour))
	revokedResult, err := resolver.revokeHouseholdInvitation(ownerCtx, household.ID, revoked.Invitation.GetID())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := revokedResult.(*model.RevokedHouseholdInvitation); !ok {
		t.Fatalf("revoked invitation variant was not returned: type=%T", revokedResult)
	}
	if _, err := resolver.acceptHouseholdInvitation(otherCtx, revoked.Token); !errors.Is(err, ErrConflict) {
		t.Fatalf("revoked invitation must be rejected: error=%v", err)
	}
	if _, err := resolver.revokeHouseholdInvitation(memberCtx, household.ID, expired.Invitation.GetID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner must not revoke invitations: error=%v", err)
	}

	duplicate := createInvitation(time.Now().Add(24 * time.Hour))
	otherUUID, _ := gofrsuuid.FromString(otherID)
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &otherUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.acceptHouseholdInvitation(otherCtx, duplicate.Token); !errors.Is(err, ErrConflict) {
		t.Fatalf("existing member must not be inserted twice: error=%v", err)
	}
	otherRows, err := models.HouseholdMembers.Query(sm.Where(psql.And(models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)), models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(otherUUID))))).All(ctx, bobpgx.NewPool(db))
	memberCount = len(otherRows)
	if err != nil || memberCount != 1 {
		t.Fatalf("existing member row changed: count=%d error=%v", memberCount, err)
	}
	duplicateID, _ := gofrsuuid.FromString(duplicate.Invitation.GetID())
	duplicateRow, err := models.HouseholdInvitations.Query(sm.Where(models.HouseholdInvitations.Columns.ID.EQ(psql.Arg(duplicateID)))).One(ctx, bobpgx.NewPool(db))
	var acceptedAt sql.Null[time.Time]
	if err == nil {
		acceptedAt = duplicateRow.AcceptedAt
	}
	if err != nil || acceptedAt.Valid {
		t.Fatalf("membership conflict must not consume invitation: acceptedAt=%v error=%v", acceptedAt, err)
	}
	leftAt := time.Now().UTC().Add(-time.Minute)
	if _, err := models.HouseholdMembers.Update(
		um.Where(psql.And(
			models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
			models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberUUID)),
		)),
		um.SetCol("left_at").To(psql.Arg(leftAt)),
	).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	rejoin := createInvitation(time.Now().Add(24 * time.Hour))
	if _, err := resolver.acceptHouseholdInvitation(memberCtx, rejoin.Token); err != nil {
		t.Fatalf("tombstoned membership could not rejoin: %v", err)
	}
	rejoined, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberUUID)),
	))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if rejoined.LeftAt.Valid || rejoined.Role != "MEMBER" || !rejoined.JoinedAt.After(leftAt) {
		t.Fatalf("rejoined membership = %#v, want active MEMBER with refreshed joined_at", rejoined)
	}
	pageSize := 1
	firstPage, err := ownerQuery.HouseholdInvitations(ownerCtx, household.ID, nil, nil, &pageSize, nil)
	if err != nil || len(firstPage.Edges) != 1 || !firstPage.PageInfo.HasNextPage || firstPage.PageInfo.EndCursor == nil {
		t.Fatalf("invitation connection first page failed: page=%#v error=%v", firstPage, err)
	}
	secondPage, err := ownerQuery.HouseholdInvitations(ownerCtx, household.ID, firstPage.PageInfo.EndCursor, nil, &pageSize, nil)
	if err != nil || len(secondPage.Edges) != 1 || firstPage.Edges[0].Node.GetID() == secondPage.Edges[0].Node.GetID() {
		t.Fatalf("invitation cursor did not continue: first=%#v second=%#v error=%v", firstPage, secondPage, err)
	}
	previousPage, err := ownerQuery.HouseholdInvitations(ownerCtx, household.ID, nil, secondPage.PageInfo.StartCursor, nil, &pageSize)
	if err != nil || len(previousPage.Edges) != 1 || previousPage.Edges[0].Node.GetID() != firstPage.Edges[0].Node.GetID() || !previousPage.PageInfo.HasNextPage || previousPage.PageInfo.HasPreviousPage {
		t.Fatalf("invitation backward pagination failed: connection=%#v error=%v", previousPage, err)
	}
}
