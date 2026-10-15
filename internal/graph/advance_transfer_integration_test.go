package graph

import (
	"os"
	"testing"

	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const testAdvanceTransferOccurredAt = "2026-06-10T12:00:00Z"

func TestAdvanceTransferAffectsSettlementWithoutBecomingSpending(t *testing.T) {
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

	memberA, memberB := uuid.NewV7().String(), uuid.NewV7().String()
	for id, name := range map[string]string{memberA: "A", memberB: "B"} {
		memberUUID := generatedUUID(id)
		subject := "advance-transfer:" + id
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(generatedUUID(memberA))))).Exec(t.Context(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(generatedUUID(memberB))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "advance-transfer:" + memberA, DisplayName: "A"})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Advance transfer accounting"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	memberBUUID := generatedUUID(memberB)
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &memberBUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}

	const amount int64 = 2400
	advance, err := resolver.recordTransfer(ctx, model.RecordTransferInput{
		HouseholdID: household.ID, FromMemberID: memberA, ToMemberID: memberB,
		Kind: model.TransferInputKindAdvance, AmountYen: model.Long(amount), OccurredAt: testAdvanceTransferOccurredAt,
		Description: "立替", IdempotencyKey: uuid.NewV7().String(),
	})
	if err != nil || advance.TransferID == nil {
		t.Fatalf("advance transfer failed: result=%#v error=%v", advance, err)
	}
	assertHouseholdJournalsBalance(t, ctx, db, householdUUID)

	query := (&Resolver{DB: db}).Query()
	settlement, err := query.Settlement(ctx, household.ID, "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	openSettlement, ok := settlement.(*model.OpenSettlement)
	if !ok || len(openSettlement.Lines) != 2 {
		t.Fatalf("advance should produce an open settlement for both members: settlement=%#v", settlement)
	}
	lines := make(map[string]*model.SettlementLine, len(openSettlement.Lines))
	for _, line := range openSettlement.Lines {
		lines[line.MemberID] = line
	}
	if lines[memberA] == nil || lines[memberB] == nil ||
		int64(lines[memberA].MonthlyAdjustmentYen) != -amount || int64(lines[memberA].AmountDueYen) != -amount ||
		int64(lines[memberB].MonthlyAdjustmentYen) != amount || int64(lines[memberB].AmountDueYen) != amount {
		t.Fatalf("advance must credit the payer and charge the recipient in settlement: lines=%#v", lines)
	}

	spending, err := query.MonthlyCategorySpending(ctx, household.ID, model.YearMonth("2026-06"), model.YearMonth("2026-06"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spending) != 0 {
		t.Fatalf("member-to-member advance must not appear as household spending: %#v", spending)
	}

	advanceUUID := generatedUUID(*advance.TransferID)
	transfer, err := models.Transfers.Query(sm.Where(models.Transfers.Columns.ID.EQ(psql.Arg(advanceUUID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil || transfer.Kind != string(model.TransferInputKindAdvance) || transfer.FromMemberID.String() != memberA || transfer.ToMemberID.String() != memberB {
		t.Fatalf("advance transfer was not persisted for the expected members: transfer=%#v error=%v", transfer, err)
	}
}
