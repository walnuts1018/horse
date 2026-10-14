package mailforwarding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"strings"
	"testing"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/apperror"
	householdapp "github.com/walnuts1018/horse/internal/application/household"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	"github.com/walnuts1018/horse/internal/testinfra"
)

func TestForwardingAddressLifecycle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	databaseURL, terminate, err := testinfra.StartPostgres(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := terminate(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("terminate PostgreSQL container: %v", err)
		}
	})
	if err := testinfra.ApplyMigrations(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	ownerID := createMailForwardingTestMember(t, db, "owner")
	otherID := createMailForwardingTestMember(t, db, "member")
	t.Cleanup(func() {
		executor := bobpgx.NewPool(db)
		for _, id := range []gofrsuuid.UUID{ownerID, otherID} {
			_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(id)))).Exec(t.Context(), executor)
		}
	})
	household, err := householdapp.NewService(db).Create(ctx, householdapp.CreateCommand{ActorID: ownerID.String(), Name: "mail forwarding"})
	if err != nil {
		t.Fatal(err)
	}
	householdID := mailForwardingUUID(t, household.ID)
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), bobpgx.NewPool(db))
	})
	memberRole := "MEMBER"
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &otherID, Role: &memberRole}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}

	service := NewService(db, Config{MailboxDomain: "walnuts.dev", AddressLocalPartPrefix: "horse-mail+"})
	command := Command{ActorID: ownerID.String(), HouseholdID: household.ID}
	if _, err := service.Active(ctx, command); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("active forwarding address before issue error = %v; want not found", err)
	}
	issued, err := service.Issue(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	activeStatus, err := service.Active(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if activeStatus.ID != issued.ID || !activeStatus.CreatedAt.Equal(issued.CreatedAt) {
		t.Fatalf("active forwarding status = %#v; want id %s and creation time %s", activeStatus, issued.ID, issued.CreatedAt)
	}
	if !strings.HasSuffix(issued.Address, "@walnuts.dev") {
		t.Fatalf("issued forwarding address = %q; want configured mailbox domain", issued.Address)
	}
	localPart := strings.TrimSuffix(issued.Address, "@walnuts.dev")
	tokenPart, ok := strings.CutPrefix(localPart, "horse-mail+")
	if !ok {
		t.Fatalf("issued forwarding address local part = %q; want configured prefix", localPart)
	}
	if len(localPart) > 64 || tokenPart != strings.ToLower(tokenPart) || len(tokenPart) != 52 {
		t.Fatalf("issued forwarding address local part = %q; want lowercase 52-character tag within 64 bytes", localPart)
	}
	rawToken, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(tokenPart))
	if err != nil {
		t.Fatalf("decode forwarding token: %v", err)
	}
	tokenHash := sha256.Sum256(rawToken)
	forwardingID := mailForwardingUUID(t, issued.ID)
	forwarding, err := models.HouseholdMailForwardings.Query(sm.Where(models.HouseholdMailForwardings.Columns.ID.EQ(psql.Arg(forwardingID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tokenHash[:], forwarding.TokenHash) {
		t.Fatal("stored forwarding token hash does not match the issued token")
	}
	if _, err := service.Issue(ctx, command); !errors.Is(err, apperror.ErrConflict) {
		t.Fatalf("issuing an existing forwarding address error = %v; want conflict", err)
	}
	if _, err := service.Revoke(ctx, Command{ActorID: otherID.String(), HouseholdID: household.ID}); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("non-owner revoke error = %v; want not found", err)
	}

	rotated, err := service.Rotate(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Address == issued.Address {
		t.Fatal("rotated forwarding address matches the previous address")
	}
	forwardings, err := models.HouseholdMailForwardings.Query(sm.Where(models.HouseholdMailForwardings.Columns.HouseholdID.EQ(psql.Arg(householdID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(forwardings) != 2 {
		t.Fatalf("forwarding history count = %d; want 2", len(forwardings))
	}
	activeCount := 0
	for _, entry := range forwardings {
		if !entry.RevokedAt.Valid {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Fatalf("active forwarding count = %d; want 1", activeCount)
	}
	if _, err := service.Revoke(ctx, command); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Revoke(ctx, command); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("repeated revoke error = %v; want not found", err)
	}
	if _, err := service.Active(ctx, command); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("active forwarding address after revoke error = %v; want not found", err)
	}

	executor := bobpgx.NewPool(db)
	householdRow, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if householdRow.Revision != 4 {
		t.Fatalf("household revision = %d; want 4", householdRow.Revision)
	}
	events, err := models.HouseholdEvents.Query(sm.Where(models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	mailForwardingEventCount := 0
	for _, event := range events {
		if strings.HasPrefix(event.Type, "household.mail_forwarding.") {
			mailForwardingEventCount++
		}
	}
	if mailForwardingEventCount != 3 {
		t.Fatalf("mail forwarding event count = %d; want 3", mailForwardingEventCount)
	}
}

func createMailForwardingTestMember(t *testing.T, db *pgxpool.Pool, name string) gofrsuuid.UUID {
	t.Helper()
	id, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	subject := "mail-forwarding-integration:" + id.String()
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &id, OidcSubject: &subject, DisplayName: &name}).Exec(t.Context(), bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	return id
}

func mailForwardingUUID(t *testing.T, value string) gofrsuuid.UUID {
	t.Helper()
	id, err := gofrsuuid.FromString(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
