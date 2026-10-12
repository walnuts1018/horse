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
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestPlanningMutationsAreAtomicAndHouseholdScoped(t *testing.T) {
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
	memberID := uuid.NewV7().String()
	memberUUID, _ := gofrsuuid.FromString(memberID)
	subject := "planning-integration:" + memberID
	memberName := "Planning"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.Background(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	resolver := (&Resolver{DB: db}).Mutation()
	household, err := resolver.CreateHousehold(ctx, model.CreateHouseholdInput{Name: "planning-integration"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(context.Background(), bobpgx.NewPool(db))
	}()
	poolA, err := resolver.CreatePool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "A", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	poolB, err := resolver.CreatePool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "B", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	category, err := resolver.CreateCategory(ctx, model.CreateCategoryInput{HouseholdID: household.ID, Name: "Food"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.SetDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}, {PoolID: poolB.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	purchase, err := resolver.CreatePurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-04-12T11:59:00Z", Merchant: "Market",
		Items: []*model.PurchaseItemInput{{Name: "Yogurt", AmountYen: 500}, {Name: "Milk", AmountYen: 300}},
	})
	if err != nil {
		t.Fatal(err)
	}
	constraintTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	itemID := generatedUUID(purchase.Items[0].ID)
	invalidItemAmount := int64(purchase.Items[0].AmountYen) + 1
	if _, err := models.PurchaseItems.Update((&models.PurchaseItemSetter{AmountYen: &invalidItemAmount}).UpdateMod(), um.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemID)))).Exec(ctx, bobpgx.NewTx(constraintTx, nil)); err != nil {
		t.Fatal(err)
	}
	if err := constraintTx.Commit(ctx); err == nil {
		t.Fatal("database must reject item totals that do not match their allocations")
	}
	purchase, err = loadPurchase(ctx, db, purchase.ID)
	if err != nil || purchase.Items[0].AmountYen != 500 {
		t.Fatalf("allocation constraint failure must roll back the item amount: purchase=%#v error=%v", purchase, err)
	}
	tag, err := resolver.CreateTag(ctx, model.CreateTagInput{HouseholdID: household.ID, Name: "Weekly"})
	if err != nil {
		t.Fatal(err)
	}
	itemTargets := []*model.PurchaseItemTargetInput{
		{ItemID: purchase.Items[0].ID, BaseRevision: purchase.Items[0].Revision},
		{ItemID: purchase.Items[1].ID, BaseRevision: purchase.Items[1].Revision},
	}
	updated, err := resolver.BulkUpdatePurchaseItems(ctx, model.BulkUpdatePurchaseItemsInput{
		Targets:       itemTargets,
		Patch:         &model.BulkPurchaseItemPatch{CategoryID: &category.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 1}}, TagIds: []string{tag.ID}},
		ChangedFields: []string{"categoryId", "allocations", "tags"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 2 || updated[0].CategoryID == nil || *updated[0].CategoryID != category.ID || len(updated[0].Tags) != 1 || updated[0].Tags[0].ID != tag.ID || amountForPool(updated[0].Allocations, poolA.ID) != 375 {
		t.Fatalf("bulk update did not apply the same category, tags, and allocations to every item: %#v", updated)
	}
	stale, err := resolver.BulkUpdatePurchaseItems(ctx, model.BulkUpdatePurchaseItemsInput{
		Targets: []*model.PurchaseItemTargetInput{{ItemID: updated[0].ID, BaseRevision: updated[0].Revision}, {ItemID: updated[1].ID, BaseRevision: 1}},
		Patch:   &model.BulkPurchaseItemPatch{TagIds: []string{}}, ChangedFields: []string{"tags"},
	})
	if stale != nil || !errors.Is(err, ErrConflict) {
		t.Fatalf("stale target must reject the entire bulk operation: result=%#v error=%v", stale, err)
	}
	reloaded, err := loadPurchase(ctx, db, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range reloaded.Items {
		if len(item.Tags) != 1 || item.Tags[0].ID != tag.ID {
			t.Fatalf("failed bulk operation partially changed item tags: %#v", item)
		}
	}
	if _, err := resolver.PostPurchase(ctx, purchase.ID, reloaded.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	budget, err := resolver.SetBudget(ctx, model.SetBudgetInput{HouseholdID: household.ID, CategoryID: &category.ID, Name: "Food", AmountYen: 1000, StartsOn: "2026-04-01"})
	if err != nil {
		t.Fatal(err)
	}
	budgets, err := (&Resolver{DB: db}).Query().Budgets(ctx, household.ID, "2026-04")
	if err != nil {
		t.Fatal(err)
	}
	if len(budgets) != 1 || budgets[0].ID != budget.ID || budgets[0].ActualYen != 800 || budgets[0].RemainingYen != 200 {
		t.Fatalf("budget should compare posted category actuals to the cap: %#v", budgets)
	}
	allBudget, err := resolver.SetBudget(ctx, model.SetBudgetInput{HouseholdID: household.ID, Name: "All spending", AmountYen: 2000, StartsOn: "2026-04-01"})
	if err != nil {
		t.Fatal(err)
	}
	budgets, err = (&Resolver{DB: db}).Query().Budgets(ctx, household.ID, "2026-04")
	if err != nil || len(budgets) != 2 {
		t.Fatalf("household and category budgets should both be listed: rows=%#v error=%v", budgets, err)
	}
	for _, entry := range budgets {
		if entry.ID == allBudget.ID && (entry.ActualYen != 800 || entry.RemainingYen != 1200) {
			t.Fatalf("uncategorized budget should include all posted spending: %#v", entry)
		}
	}
	recurring, err := resolver.SetRecurringExpense(ctx, model.SetRecurringExpenseInput{HouseholdID: household.ID, Name: "Internet", Merchant: "ISP", AmountYen: 4500, CategoryID: &category.ID, Cadence: "MONTHLY", StartsOn: "2026-04-01", NextDueOn: "2026-05-01"})
	if err != nil {
		t.Fatal(err)
	}
	expenses, err := (&Resolver{DB: db}).Query().RecurringExpenses(ctx, household.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(expenses) != 1 || expenses[0].ID != recurring.ID || expenses[0].NextDueOn != "2026-05-01" {
		t.Fatalf("recurring expense metadata did not round-trip: %#v", expenses)
	}
	bank, err := resolver.CreateAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Bank", Kind: model.AccountKindBank})
	if err != nil {
		t.Fatal(err)
	}
	card, err := resolver.CreateAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Card", Kind: model.AccountKindCard})
	if err != nil {
		t.Fatal(err)
	}
	movementInput := model.RecordAccountMovementInput{
		HouseholdID: household.ID, Kind: model.AccountMovementKindCardBillPayment,
		FromAccountID: bank.ID, ToAccountID: card.ID, AmountYen: 800,
		OccurredAt: "2026-05-01T09:00:00Z", Description: "April card statement", IdempotencyKey: uuid.NewV7().String(),
	}
	movement, err := resolver.RecordAccountMovement(ctx, movementInput)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := resolver.RecordAccountMovement(ctx, movementInput)
	if err != nil || replayed.ID != movement.ID {
		t.Fatalf("idempotent account movement replay created a second record: movement=%#v replay=%#v err=%v", movement, replayed, err)
	}
	listed, err := (&Resolver{DB: db}).Query().AccountMovements(ctx, household.ID, nil)
	if err != nil || len(listed) != 1 || listed[0].ID != movement.ID {
		t.Fatalf("account movement query failed: rows=%#v error=%v", listed, err)
	}
	storedMovement, err := models.AccountMovements.Query(sm.Where(models.AccountMovements.Columns.ID.EQ(psql.Arg(generatedUUID(movement.ID))))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	journalEntries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(storedMovement.JournalID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(journalEntries) != 2 || journalEntries[0].DebitYen+journalEntries[1].DebitYen != 800 || journalEntries[0].CreditYen+journalEntries[1].CreditYen != 800 {
		t.Fatalf("card bill movement must create a balanced journal: %#v", journalEntries)
	}
	for _, entry := range journalEntries {
		if entry.DebitYen == 800 && (!entry.AccountID.Valid || entry.AccountID.V != generatedUUID(card.ID)) || entry.CreditYen == 800 && (!entry.AccountID.Valid || entry.AccountID.V != generatedUUID(bank.ID)) {
			t.Fatalf("card bill movement must reduce card liability and bank cash: %#v", journalEntries)
		}
	}
	if _, err := resolver.RecordAccountMovement(ctx, model.RecordAccountMovementInput{HouseholdID: household.ID, Kind: model.AccountMovementKindCardBillPayment, FromAccountID: card.ID, ToAccountID: bank.ID, AmountYen: 800, OccurredAt: movementInput.OccurredAt, IdempotencyKey: uuid.NewV7().String()}); !errors.Is(err, ErrBadInput) {
		t.Fatalf("card bill payment must debit a bank account into a card account: %v", err)
	}
}

func amountForPool(allocations []*model.Allocation, poolID string) model.Long {
	for _, allocation := range allocations {
		if allocation.PoolID == poolID && allocation.AmountYen != nil {
			return *allocation.AmountYen
		}
	}
	return 0
}
