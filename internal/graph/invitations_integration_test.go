package graph

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func TestHouseholdInvitationLifecycle(t *testing.T) {
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
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
		if _, err := db.Exec(ctx, `INSERT INTO members(id, oidc_subject, display_name) VALUES ($1, $2, $3)`, id, "invitation-test:"+id, name); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM members WHERE id IN ($1, $2, $3)`, ownerID, memberID, otherID)
	}()

	resolver := (&Resolver{DB: db}).Mutation()
	ownerCtx := auth.WithPrincipal(ctx, auth.Principal{MemberID: ownerID, Subject: "invitation-test:" + ownerID, DisplayName: "Owner"})
	memberCtx := auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: "invitation-test:" + memberID, DisplayName: "Member"})
	otherCtx := auth.WithPrincipal(ctx, auth.Principal{MemberID: otherID, Subject: "invitation-test:" + otherID, DisplayName: "Other"})
	household, err := resolver.CreateHousehold(ownerCtx, model.CreateHouseholdInput{Name: "invitation-test", MemberNames: []string{"Future member"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, household.ID) }()
	otherHousehold, err := resolver.CreateHousehold(otherCtx, model.CreateHouseholdInput{Name: "other-invitation-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM households WHERE id = $1`, otherHousehold.ID)
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

	createInvitation := func(expiresAt time.Time) *model.CreatedHouseholdInvitation {
		t.Helper()
		invitation, err := resolver.CreateHouseholdInvitation(ownerCtx, model.CreateHouseholdInvitationInput{HouseholdID: household.ID, ExpiresAt: expiresAt.Format(time.RFC3339Nano)})
		if err != nil {
			t.Fatal(err)
		}
		if invitation.Invitation.Role != "MEMBER" || len(invitation.Token) != 43 {
			t.Fatalf("unexpected invitation role or token: role=%q token length=%d", invitation.Invitation.Role, len(invitation.Token))
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
	if err := db.QueryRow(ctx, `SELECT token_hash FROM household_invitations WHERE id = $1`, active.Invitation.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedHash, expectedHash[:]) {
		t.Fatal("database must store only the SHA256 invitation token hash")
	}
	ownerQuery := (&Resolver{DB: db}).Query()
	listed, err := ownerQuery.HouseholdInvitations(ownerCtx, household.ID)
	if err != nil || len(listed) != 1 || listed[0].ID != active.Invitation.ID {
		t.Fatalf("owner invitation list failed: invitations=%#v error=%v", listed, err)
	}
	if _, err := ownerQuery.HouseholdInvitations(memberCtx, household.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner must not list invitations: error=%v", err)
	}
	if _, err := ownerQuery.HouseholdInvitations(ownerCtx, otherHousehold.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("owner must not list another household's invitations: error=%v", err)
	}
	if _, err := resolver.CreateHouseholdInvitation(memberCtx, model.CreateHouseholdInvitationInput{HouseholdID: household.ID, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner must not create invitations: error=%v", err)
	}
	if _, err := resolver.RevokeHouseholdInvitation(ownerCtx, otherHousehold.ID, active.Invitation.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invitation from another household must not be revoked: error=%v", err)
	}
	if _, err := resolver.AcceptHouseholdInvitation(memberCtx, active.Token); err != nil {
		t.Fatalf("valid invitation was not accepted: %v", err)
	}
	var memberCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM household_members WHERE household_id = $1 AND member_id = $2 AND role = 'MEMBER'`, household.ID, memberID).Scan(&memberCount); err != nil || memberCount != 1 {
		t.Fatalf("accepted user membership was not created: count=%d error=%v", memberCount, err)
	}
	var placeholderExists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM household_members WHERE household_id = $1 AND member_id = $2)`, household.ID, placeholderID).Scan(&placeholderExists); err != nil || !placeholderExists {
		t.Fatalf("joining user must not replace historical placeholder membership: exists=%v error=%v", placeholderExists, err)
	}
	if _, err := resolver.AcceptHouseholdInvitation(memberCtx, active.Token); !errors.Is(err, ErrConflict) {
		t.Fatalf("used invitation must be rejected: error=%v", err)
	}
	if _, err := resolver.AcceptHouseholdInvitation(otherCtx, active.Token); !errors.Is(err, ErrConflict) {
		t.Fatalf("accepted invitation must not be reused by another user: error=%v", err)
	}

	expired := createInvitation(time.Now().Add(24 * time.Hour))
	if _, err := db.Exec(ctx, `UPDATE household_invitations SET expires_at = now() - interval '1 second' WHERE id = $1`, expired.Invitation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.AcceptHouseholdInvitation(otherCtx, expired.Token); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired invitation must be rejected: error=%v", err)
	}

	revoked := createInvitation(time.Now().Add(24 * time.Hour))
	if _, err := resolver.RevokeHouseholdInvitation(ownerCtx, household.ID, revoked.Invitation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.AcceptHouseholdInvitation(otherCtx, revoked.Token); !errors.Is(err, ErrConflict) {
		t.Fatalf("revoked invitation must be rejected: error=%v", err)
	}
	if _, err := resolver.RevokeHouseholdInvitation(memberCtx, household.ID, expired.Invitation.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner must not revoke invitations: error=%v", err)
	}

	duplicate := createInvitation(time.Now().Add(24 * time.Hour))
	if _, err := db.Exec(ctx, `INSERT INTO household_members(household_id, member_id) VALUES ($1, $2)`, household.ID, otherID); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.AcceptHouseholdInvitation(otherCtx, duplicate.Token); !errors.Is(err, ErrConflict) {
		t.Fatalf("existing member must not be inserted twice: error=%v", err)
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM household_members WHERE household_id = $1 AND member_id = $2`, household.ID, otherID).Scan(&memberCount); err != nil || memberCount != 1 {
		t.Fatalf("existing member row changed: count=%d error=%v", memberCount, err)
	}
	var acceptedAt *time.Time
	if err := db.QueryRow(ctx, `SELECT accepted_at FROM household_invitations WHERE id = $1`, duplicate.Invitation.ID).Scan(&acceptedAt); err != nil || acceptedAt != nil {
		t.Fatalf("membership conflict must not consume invitation: acceptedAt=%v error=%v", acceptedAt, err)
	}
}
