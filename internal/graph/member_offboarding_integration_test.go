package graph

import (
	"database/sql"
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

func TestHouseholdMemberOffboarding(t *testing.T) {
	t.Parallel()
	const ownerName = "Owner"
	const memberName = nodeTypeMember
	const initialMemberName = "Initial member"
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

	ownerID, secondOwnerID, memberID := newID(), newID(), newID()
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	ownerContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: ownerID, Subject: "offboarding:" + ownerID, DisplayName: ownerName})
	secondOwnerContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: secondOwnerID, Subject: "offboarding:" + secondOwnerID, DisplayName: "Second owner"})
	memberContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: "offboarding:" + memberID, DisplayName: memberName})
	for id, name := range map[string]string{ownerID: ownerName, secondOwnerID: "Second owner", memberID: memberName} {
		if _, err := models.Members.Insert(&models.MemberSetter{
			ID: uuidPointer(t, id), OidcSubject: new("offboarding:" + id), DisplayName: new(name),
		}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(psql.Or(
			models.Members.Columns.ID.EQ(psql.Arg(ownerID)),
			models.Members.Columns.ID.EQ(psql.Arg(secondOwnerID)),
			models.Members.Columns.ID.EQ(psql.Arg(memberID)),
		))).Exec(t.Context(), bobpgx.NewPool(db))
	}()

	household, err := resolver.createHousehold(ownerContext, model.CreateHouseholdInput{
		Name: "member-offboarding", MemberNames: []string{initialMemberName},
	})
	if err != nil {
		t.Fatal(err)
	}
	householdID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	var initialMemberID gofrsuuid.UUID
	for _, member := range household.Members {
		if member.DisplayName == initialMemberName {
			initialMemberID = generatedUUID(member.ID)
		}
	}
	initialMemberContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: initialMemberID.String(), Subject: "offboarding:" + initialMemberID.String(), DisplayName: initialMemberName})
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: uuidPointer(t, secondOwnerID), Role: new("OWNER")}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: uuidPointer(t, memberID), Role: new("MEMBER")}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}

	pool, err := resolver.createPool(ownerContext, model.CreatePoolInput{HouseholdID: household.ID, Name: "Member offboarding", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setPoolFundingRule(ownerContext, model.SetPoolFundingRuleInput{
		HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(initialMemberID.String()), ExpectedVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	conflictedLeave, err := resolver.LeaveHousehold(initialMemberContext, model.LeaveHouseholdInput{HouseholdID: household.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result, ok := conflictedLeave.(*model.LeaveHouseholdUserError); !ok || result.Code != model.MutationErrorCodeConflict {
		t.Fatalf("leaving with an active pool funding reference = %#v, want CONFLICT", conflictedLeave)
	}
	if _, err := resolver.setPoolFundingRule(ownerContext, model.SetPoolFundingRuleInput{
		HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(secondOwnerID), ExpectedVersion: 2,
	}); err != nil {
		t.Fatal(err)
	}
	fundingScheduleID, poolID := generatedUUID(newID()), generatedUUID(pool.ID)
	amount, cadence, startsOn := int64(1000), "MONTHLY", time.Now().UTC().Truncate(24*time.Hour).AddDate(0, -1, 0)
	if _, err := models.FundingSchedules.Insert(&models.FundingScheduleSetter{
		ID: &fundingScheduleID, HouseholdID: &householdID, MemberID: &initialMemberID, PoolID: &poolID,
		AmountYen: &amount, Cadence: &cadence, StartsOn: &startsOn,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	scheduledLeave, err := resolver.LeaveHousehold(initialMemberContext, model.LeaveHouseholdInput{HouseholdID: household.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result, ok := scheduledLeave.(*model.LeaveHouseholdUserError); !ok || result.Code != model.MutationErrorCodeConflict {
		t.Fatalf("leaving with an active funding schedule = %#v, want CONFLICT", scheduledLeave)
	}
	endedOn := sql.Null[time.Time]{V: time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1), Valid: true}
	if _, err := models.FundingSchedules.Update(
		um.Where(models.FundingSchedules.Columns.ID.EQ(psql.Arg(fundingScheduleID))),
		um.SetCol("ends_on").To(psql.Arg(endedOn)),
	).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	journalID, sourceID := generatedUUID(newID()), generatedUUID(newID())
	sourceKind, idempotencyKey := "TEST", "offboarding:"+initialMemberID.String()
	occurredAt := time.Now().UTC()
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &journalID, HouseholdID: &householdID, SourceKind: &sourceKind, SourceID: &sourceID,
		IdempotencyKey: &idempotencyKey, OccurredAt: &occurredAt, CreatedBy: uuidPointer(t, initialMemberID.String()),
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	journalTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = journalTx.Rollback(ctx) }()
	journalExecutor := bobpgx.NewTx(journalTx, nil)
	lineNo, debitYen := int32(1), int64(1250)
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalID, LineNo: &lineNo, MemberID: &sql.Null[gofrsuuid.UUID]{V: initialMemberID, Valid: true}, DebitYen: &debitYen,
	}).Exec(ctx, journalExecutor); err != nil {
		t.Fatal(err)
	}
	creditLineNo, creditYen := int32(2), debitYen
	if _, err := models.JournalEntries.Insert(&models.JournalEntrySetter{
		JournalID: &journalID, LineNo: &creditLineNo, CreditYen: &creditYen,
	}).Exec(ctx, journalExecutor); err != nil {
		t.Fatal(err)
	}
	if err := journalTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		executor := bobpgx.NewPool(db)
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(initialMemberID)))).Exec(t.Context(), executor)
	}()
	unauthorized, err := resolver.RemoveHouseholdMember(memberContext, model.RemoveHouseholdMemberInput{HouseholdID: household.ID, MemberID: secondOwnerID})
	if err != nil {
		t.Fatal(err)
	}
	if result, ok := unauthorized.(*model.RemoveHouseholdMemberUserError); !ok || result.Code != model.MutationErrorCodeNotFound {
		t.Fatalf("non-owner removal = %#v, want NOT_FOUND", unauthorized)
	}
	transferred, err := resolver.TransferHouseholdOwner(ownerContext, model.TransferHouseholdOwnerInput{HouseholdID: household.ID, NewOwnerID: memberID})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := transferred.(*model.TransferHouseholdOwnerSuccess); !ok {
		t.Fatalf("owner transfer = %T, want success", transferred)
	}
	for memberID, wantRole := range map[string]string{ownerID: "MEMBER", memberID: "OWNER"} {
		memberUUID := generatedUUID(memberID)
		membership, err := models.HouseholdMembers.Query(sm.Where(psql.And(
			models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberUUID)),
		))).One(ctx, bobpgx.NewPool(db))
		if err != nil {
			t.Fatal(err)
		}
		if membership.Role != wantRole {
			t.Errorf("member %s role = %q, want %q", memberID, membership.Role, wantRole)
		}
	}
	unauthorizedTransfer, err := resolver.TransferHouseholdOwner(ownerContext, model.TransferHouseholdOwnerInput{HouseholdID: household.ID, NewOwnerID: secondOwnerID})
	if err != nil {
		t.Fatal(err)
	}
	if result, ok := unauthorizedTransfer.(*model.TransferHouseholdOwnerUserError); !ok || result.Code != model.MutationErrorCodeNotFound {
		t.Fatalf("non-owner transfer = %#v, want NOT_FOUND", unauthorizedTransfer)
	}
	transferredAgain, err := resolver.TransferHouseholdOwner(memberContext, model.TransferHouseholdOwnerInput{HouseholdID: household.ID, NewOwnerID: secondOwnerID})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := transferredAgain.(*model.TransferHouseholdOwnerSuccess); !ok {
		t.Fatalf("transfer to existing owner = %T, want success", transferredAgain)
	}
	selfTransfer, err := resolver.TransferHouseholdOwner(secondOwnerContext, model.TransferHouseholdOwnerInput{HouseholdID: household.ID, NewOwnerID: secondOwnerID})
	if err != nil {
		t.Fatal(err)
	}
	if result, ok := selfTransfer.(*model.TransferHouseholdOwnerUserError); !ok || result.Code != model.MutationErrorCodeConflict {
		t.Fatalf("self transfer = %#v, want CONFLICT", selfTransfer)
	}
	leaveMember, err := resolver.LeaveHousehold(initialMemberContext, model.LeaveHouseholdInput{HouseholdID: household.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := leaveMember.(*model.LeaveHouseholdSuccess); !ok {
		t.Fatalf("member leaving = %T, want success", leaveMember)
	}
	departedView, err := (&Resolver{DB: db}).Query().Household(initialMemberContext, household.ID)
	if err != nil || departedView != nil {
		t.Fatalf("departed member household access = %#v, error=%v, want no access", departedView, err)
	}
	activeView, err := (&Resolver{DB: db}).Query().Household(secondOwnerContext, household.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, activeMember := range activeView.Members {
		if activeMember.ID == initialMemberID.String() {
			t.Fatal("departed member appeared in the active household member list")
		}
	}
	if _, err := models.Members.Query(sm.Where(models.Members.Columns.ID.EQ(psql.Arg(initialMemberID)))).One(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatalf("departed member record was deleted: %v", err)
	}
	departedMembership, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(initialMemberID)),
	))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatalf("departed member membership was not retained: %v", err)
	}
	if !departedMembership.LeftAt.Valid {
		t.Fatal("departed member membership has no left_at timestamp")
	}
	journalEntries, err := models.JournalEntries.Query(sm.Where(psql.And(
		models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journalID)),
		models.JournalEntries.Columns.MemberID.EQ(psql.Arg(initialMemberID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(journalEntries) != 1 || journalEntries[0].DebitYen != debitYen {
		t.Fatalf("accounting entry after offboarding = %#v, error=%v", journalEntries, err)
	}

	leave, err := resolver.LeaveHousehold(ownerContext, model.LeaveHouseholdInput{HouseholdID: household.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := leave.(*model.LeaveHouseholdSuccess); !ok {
		t.Fatalf("owner leaving with another owner = %T, want success", leave)
	}

	lastOwnerLeave, err := resolver.LeaveHousehold(secondOwnerContext, model.LeaveHouseholdInput{HouseholdID: household.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result, ok := lastOwnerLeave.(*model.LeaveHouseholdUserError); !ok || result.Code != model.MutationErrorCodeConflict {
		t.Fatalf("last owner leaving = %#v, want CONFLICT", lastOwnerLeave)
	}

	removed, err := resolver.RemoveHouseholdMember(secondOwnerContext, model.RemoveHouseholdMemberInput{HouseholdID: household.ID, MemberID: memberID})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := removed.(*model.RemoveHouseholdMemberSuccess); !ok {
		t.Fatalf("owner removal = %T, want success", removed)
	}

	if _, err := models.Members.Query(sm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).One(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatalf("removed member record was deleted: %v", err)
	}
	if _, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(secondOwnerID)),
	))).One(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatalf("last owner membership is missing: %v", err)
	}
	events, err := models.HouseholdEvents.Query(sm.Where(psql.And(
		models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdEvents.Columns.EntityID.EQ(psql.Arg(memberID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("member events = %#v, want owner transfer and removal events", events)
	}
	seenEvents := map[string]bool{}
	for _, event := range events {
		seenEvents[event.Type] = true
	}
	if !seenEvents["household.owner.transferred"] || !seenEvents["household.member.removed"] {
		t.Fatalf("member events = %#v, want owner transfer and removal events", events)
	}

}

func uuidPointer(t *testing.T, value string) *gofrsuuid.UUID {
	t.Helper()
	parsed, err := gofrsuuid.FromString(value)
	if err != nil {
		t.Fatal(err)
	}
	return new(parsed)
}
