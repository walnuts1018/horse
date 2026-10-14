package migration

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	"github.com/walnuts1018/horse/internal/testinfra"
)

func TestJournalAndLedgerEntriesEnforceHouseholdConsistency(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	databaseURL, terminate, err := testinfra.StartPostgres(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := terminate(cleanupCtx); err != nil {
			t.Errorf("terminate PostgreSQL test container: %v", err)
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
	executor := bobpgx.NewPool(db)

	newID := func() gofrsuuid.UUID {
		t.Helper()
		id, err := gofrsuuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	householdIDs := []gofrsuuid.UUID{newID(), newID()}
	memberIDs := []gofrsuuid.UUID{newID(), newID()}
	poolIDs := []gofrsuuid.UUID{newID(), newID()}
	accountIDs := []gofrsuuid.UUID{newID(), newID()}
	journalIDs := []gofrsuuid.UUID{newID(), newID()}
	now := time.Now()
	for i := range householdIDs {
		holdName := "Tenant " + string(rune('A'+i))
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberIDs[i], OidcSubject: &holdName, DisplayName: &holdName}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		householdName := holdName
		if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdIDs[i], Name: &householdName}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		role := "OWNER"
		if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdIDs[i], MemberID: &memberIDs[i], Role: &role}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		poolName := holdName
		poolKind := "SHARED"
		if _, err := models.Pools.Insert(&models.PoolSetter{ID: &poolIDs[i], HouseholdID: &householdIDs[i], Name: &poolName, Kind: &poolKind}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		accountName := holdName
		accountKind := "BANK"
		if _, err := models.Accounts.Insert(&models.AccountSetter{ID: &accountIDs[i], HouseholdID: &householdIDs[i], Name: &accountName, Kind: &accountKind}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		journalKind := "TEST"
		sourceID := newID()
		key := holdName
		if _, err := models.Journals.Insert(&models.JournalSetter{ID: &journalIDs[i], HouseholdID: &householdIDs[i], SourceKind: &journalKind, SourceID: &sourceID, IdempotencyKey: &key, OccurredAt: &now, CreatedBy: &memberIDs[i]}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}

	validAccount := sql.Null[gofrsuuid.UUID]{V: accountIDs[0], Valid: true}
	validPool := sql.Null[gofrsuuid.UUID]{V: poolIDs[0], Valid: true}
	validMember := sql.Null[gofrsuuid.UUID]{V: memberIDs[0], Valid: true}
	noReference := sql.Null[gofrsuuid.UUID]{}
	entries := []*models.JournalEntrySetter{
		{JournalID: &journalIDs[0], LineNo: new(int32(1)), AccountID: &validAccount, PoolID: &validPool, MemberID: &validMember, DebitYen: new(int64(100)), CreditYen: new(int64(0))},
		{JournalID: &journalIDs[0], LineNo: new(int32(2)), AccountID: &noReference, PoolID: &noReference, MemberID: &noReference, DebitYen: new(int64(0)), CreditYen: new(int64(100))},
	}
	if _, err := models.JournalEntries.Insert(bob.ToMods(entries...)).All(ctx, executor); err != nil {
		t.Fatalf("insert same-household journal entries: %v", err)
	}

	invalidEntries := []*models.JournalEntrySetter{
		{JournalID: &journalIDs[0], LineNo: new(int32(3)), AccountID: new(sql.Null[gofrsuuid.UUID]{V: accountIDs[1], Valid: true}), PoolID: &validPool, MemberID: &validMember, DebitYen: new(int64(10)), CreditYen: new(int64(0))},
		{JournalID: &journalIDs[0], LineNo: new(int32(4)), AccountID: &validAccount, PoolID: new(sql.Null[gofrsuuid.UUID]{V: poolIDs[1], Valid: true}), MemberID: &validMember, DebitYen: new(int64(10)), CreditYen: new(int64(0))},
		{JournalID: &journalIDs[0], LineNo: new(int32(5)), AccountID: &validAccount, PoolID: &validPool, MemberID: new(sql.Null[gofrsuuid.UUID]{V: memberIDs[1], Valid: true}), DebitYen: new(int64(10)), CreditYen: new(int64(0))},
		{JournalID: &journalIDs[1], LineNo: new(int32(1)), AccountID: &validAccount, PoolID: &validPool, MemberID: &validMember, DebitYen: new(int64(10)), CreditYen: new(int64(0))},
	}
	for _, entry := range invalidEntries {
		if _, err := models.JournalEntries.Insert(entry).Exec(ctx, executor); err == nil {
			t.Fatalf("insert cross-household journal entry %#v succeeded", entry)
		}
	}

	ledgerKind := "PURCHASE"
	description := "same household"
	amount := int64(100)
	ledgerID := newID()
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{ID: &ledgerID, HouseholdID: &householdIDs[0], JournalID: &journalIDs[0], Kind: &ledgerKind, OccurredAt: &now, Description: &description, AmountYen: &amount}).Exec(ctx, executor); err != nil {
		t.Fatalf("insert same-household ledger entry: %v", err)
	}
	otherLedgerID := newID()
	if _, err := models.LedgerEntries.Insert(&models.LedgerEntrySetter{ID: &otherLedgerID, HouseholdID: &householdIDs[1], JournalID: &journalIDs[0], Kind: &ledgerKind, OccurredAt: &now, Description: &description, AmountYen: &amount}).Exec(ctx, executor); err == nil {
		t.Fatal("insert cross-household ledger entry succeeded")
	}
}

func TestJournalEntriesHouseholdBackfillOnUpgrade(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	databaseURL, terminate, err := testinfra.StartPostgres(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := terminate(cleanupCtx); err != nil {
			t.Errorf("terminate PostgreSQL test container: %v", err)
		}
	})
	if err := applyMigrationsThroughVersion(ctx, databaseURL, "20261014210000"); err != nil {
		t.Fatal(err)
	}

	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	executor := bobpgx.NewPool(db)

	newID := func() gofrsuuid.UUID {
		t.Helper()
		id, err := gofrsuuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	householdIDs := []gofrsuuid.UUID{newID(), newID()}
	memberIDs := []gofrsuuid.UUID{newID(), newID()}
	accountIDs := []gofrsuuid.UUID{newID(), newID()}
	journalIDs := []gofrsuuid.UUID{newID(), newID()}
	now := time.Now()
	for i := range householdIDs {
		name := fmt.Sprintf("Upgrade tenant %d", i)
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberIDs[i], OidcSubject: &name, DisplayName: &name}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		householdName := name
		if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdIDs[i], Name: &householdName}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		role := "OWNER"
		if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdIDs[i], MemberID: &memberIDs[i], Role: &role}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		accountName := name
		accountKind := "BANK"
		if _, err := models.Accounts.Insert(&models.AccountSetter{ID: &accountIDs[i], HouseholdID: &householdIDs[i], Name: &accountName, Kind: &accountKind}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		journalKind := "TEST"
		sourceID := newID()
		key := name
		if _, err := models.Journals.Insert(&models.JournalSetter{ID: &journalIDs[i], HouseholdID: &householdIDs[i], SourceKind: &journalKind, SourceID: &sourceID, IdempotencyKey: &key, OccurredAt: &now, CreatedBy: &memberIDs[i]}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}

	noReference := sql.Null[gofrsuuid.UUID]{}
	for i, journalID := range journalIDs {
		account := sql.Null[gofrsuuid.UUID]{V: accountIDs[i], Valid: true}
		if _, err := psql.Insert(
			im.Into(models.JournalEntries.NameAsExpr(), "journal_id", "line_no", "account_id", "pool_id", "member_id", "debit_yen", "credit_yen"),
			im.Rows(
				[]bob.Expression{psql.Arg(journalID, int32(1), account, noReference, noReference, int64(100), int64(0))},
				[]bob.Expression{psql.Arg(journalID, int32(2), noReference, noReference, noReference, int64(0), int64(100))},
			),
		).Exec(ctx, executor); err != nil {
			t.Fatalf("insert legacy journal entries: %v", err)
		}
	}

	if err := testinfra.ApplyMigrations(ctx, databaseURL); err != nil {
		t.Fatal(err)
	}
	for i, journalID := range journalIDs {
		entries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journalID)))).All(ctx, executor)
		if err != nil {
			t.Fatalf("query upgraded journal entries: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("got %d backfilled entries for journal %s, want 2", len(entries), journalID)
		}
		for _, entry := range entries {
			if entry.HouseholdID != householdIDs[i] {
				t.Errorf("journal %s entry household ID = %s, want %s", journalID, entry.HouseholdID, householdIDs[i])
			}
		}
	}

	otherAccount := sql.Null[gofrsuuid.UUID]{V: accountIDs[1], Valid: true}
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID:   &journalIDs[0],
		LineNo:      new(int32(3)),
		AccountID:   &otherAccount,
		PoolID:      &noReference,
		MemberID:    &noReference,
		DebitYen:    new(int64(10)),
		CreditYen:   new(int64(0)),
		HouseholdID: &householdIDs[1],
	}).Exec(ctx, executor); err == nil {
		t.Fatal("insert journal entry referencing another household's account succeeded")
	}
}

func applyMigrationsThroughVersion(ctx context.Context, databaseURL, version string) error {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("resolve migration test source path")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", ".."))
	command := exec.CommandContext(ctx, "mise", "exec", "--", "atlas", "migrate", "apply", "--env", "kubernetes", "--to-version", version)
	command.Dir = repositoryRoot
	for _, variable := range command.Environ() {
		if !strings.HasPrefix(variable, "DATABASE_URL=") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env, "DATABASE_URL="+databaseURL)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apply Atlas migrations through %s: %w: %s", version, err, output)
	}
	return nil
}
