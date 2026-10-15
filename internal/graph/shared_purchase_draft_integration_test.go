package graph

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestSharedPurchaseDraftUsesMembershipAndRevisionCAS(t *testing.T) {
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

	owner := createSharedDraftTestMember(t, ctx, db, "shared-draft-owner")
	collaborator := createSharedDraftTestMember(t, ctx, db, "shared-draft-collaborator")
	outsider := createSharedDraftTestMember(t, ctx, db, "shared-draft-outsider")
	householdID := createSharedDraftTestHousehold(t, ctx, db, owner, collaborator)
	otherHouseholdID := createSharedDraftTestHousehold(t, ctx, db, owner)
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	service := purchase.NewService(db)

	validPayload := `{"merchant":"Market","occurredAt":"2026-10-14T12:00:00+09:00","itemName":"","itemAmount":"","categoryId":"","items":[]}`
	ownerContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: owner.String()})
	createdResult, err := resolver.CreateSharedPurchaseDraft(ownerContext, model.CreateSharedPurchaseDraftInput{
		HouseholdID: householdID.String(), Payload: validPayload,
	})
	if err != nil {
		t.Fatal(err)
	}
	createdSuccess, ok := createdResult.(*model.CreateSharedPurchaseDraftSuccess)
	if !ok {
		t.Fatalf("create result = %#v, want success", createdResult)
	}
	created := createdSuccess.Result
	if created.Revision != 1 || created.HouseholdID != householdID.String() || created.Payload == "" {
		t.Fatalf("created shared draft = %#v", created)
	}

	updatedPayload := `{"merchant":"Updated market","occurredAt":"2026-10-14T12:00:00+09:00","itemName":"","itemAmount":"","categoryId":"","items":[]}`
	updatedResult, err := resolver.UpdateSharedPurchaseDraft(ownerContext, model.UpdateSharedPurchaseDraftInput{
		ID: created.ID, ExpectedRevision: 1, Payload: updatedPayload,
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedSuccess, ok := updatedResult.(*model.UpdateSharedPurchaseDraftSuccess)
	if !ok {
		t.Fatalf("revision 1 update result = %#v, want success", updatedResult)
	}
	if updatedSuccess.Result.Revision != 2 || !strings.Contains(updatedSuccess.Result.Payload, "Updated market") {
		t.Fatalf("revision 1 update result = %#v", updatedSuccess)
	}
	staleResult, err := resolver.UpdateSharedPurchaseDraft(ownerContext, model.UpdateSharedPurchaseDraftInput{
		ID: created.ID, ExpectedRevision: 1, Payload: `{`,
	})
	if err != nil {
		t.Fatal(err)
	}
	staleConflict, ok := staleResult.(*model.UpdateSharedPurchaseDraftConflict)
	if !ok {
		t.Fatalf("stale revision result = %#v, want conflict", staleResult)
	}
	if staleConflict.Draft.Revision != 2 || !strings.Contains(staleConflict.Draft.Payload, "Updated market") {
		t.Fatalf("stale revision result = %#v", staleConflict)
	}

	collaboratorContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: collaborator.String()})
	loaded, err := (&queryResolver{Resolver: resolver.Resolver}).SharedPurchaseDraft(collaboratorContext, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || !strings.Contains(loaded.Payload, "Updated market") {
		t.Fatalf("collaborator read result = %#v", loaded)
	}
	outsiderContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: outsider.String()})
	if _, err := service.GetSharedPurchaseDraft(ctx, outsider.String(), created.ID); !errors.Is(err, purchase.ErrNotFound) {
		t.Fatalf("outsider read error = %v, want not found", err)
	}
	if _, err := (&queryResolver{Resolver: resolver.Resolver}).SharedPurchaseDraft(outsiderContext, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider GraphQL read error = %v, want not found", err)
	}
	for _, payload := range []string{
		`{`,
		`{}`,
		`{"merchant":"M","occurredAt":"","itemName":"","itemAmount":"","categoryId":"","items":[],"storageKey":"local-only"}`,
		strings.Repeat("x", 256*1024+1),
	} {
		if _, err := service.CreateSharedPurchaseDraft(ctx, owner.String(), householdID.String(), payload); !errors.Is(err, purchase.ErrBadInput) {
			t.Errorf("invalid payload error = %v, want bad input", err)
		}
	}

	const maxPayloadBytes = 256 * 1024
	largePayloadPrefix := `{"merchant":"`
	largePayloadMiddle := `","occurredAt":"","itemName":"","itemAmount":"","categoryId":"","items":[`
	largePayloadSuffix := `]}`
	item := `{"key":"","name":"","amount":"","categoryId":"","allocations":[{"poolId":"","weight":0}]}`
	itemCount := (maxPayloadBytes - len(largePayloadPrefix) - len(largePayloadMiddle) - len(largePayloadSuffix)) / (len(item) + 1)
	items := strings.TrimSuffix(strings.Repeat(item+",", itemCount), ",")
	merchantLength := maxPayloadBytes - len(largePayloadPrefix) - len(largePayloadMiddle) - len(items) - len(largePayloadSuffix)
	largePayload := largePayloadPrefix + strings.Repeat("x", merchantLength) + largePayloadMiddle + items + largePayloadSuffix
	if len(largePayload) != maxPayloadBytes {
		t.Fatalf("maximum-sized payload length = %d, want %d", len(largePayload), maxPayloadBytes)
	}
	if _, err := service.CreateSharedPurchaseDraft(ctx, owner.String(), householdID.String(), largePayload); err != nil {
		t.Fatalf("create maximum-sized shared draft: %v", err)
	}

	foreignCategory, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	categoryName := "Foreign"
	if _, err := models.Categories.Insert(&models.CategorySetter{ID: &foreignCategory, HouseholdID: &otherHouseholdID, Name: &categoryName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	foreignPool, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	poolName, poolKind := "Foreign pool", "PERSONAL"
	if _, err := models.Pools.Insert(&models.PoolSetter{ID: &foreignPool, HouseholdID: &otherHouseholdID, Name: &poolName, Kind: &poolKind}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	foreignAccount, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	accountName, accountKind := "Foreign account", "CASH"
	if _, err := models.Accounts.Insert(&models.AccountSetter{ID: &foreignAccount, HouseholdID: &otherHouseholdID, Name: &accountName, Kind: &accountKind}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	foreignConcept, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	conceptName, normalizedConcept := "Foreign item", "foreign item"
	if _, err := models.ProductConcepts.Insert(&models.ProductConceptSetter{ID: &foreignConcept, HouseholdID: &otherHouseholdID, Name: &conceptName, NormalizedName: &normalizedConcept}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	foreignVariant, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	variantName, normalizedVariant := "Foreign variant", "foreign variant"
	if _, err := models.ProductVariants.Insert(&models.ProductVariantSetter{ID: &foreignVariant, HouseholdID: &otherHouseholdID, ConceptID: &foreignConcept, Name: &variantName, NormalizedName: &normalizedVariant}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}

	foreignPayloads := []string{
		`{"merchant":"M","occurredAt":"2026-10-14T12:00:00Z","itemName":"","itemAmount":"","categoryId":"` + foreignCategory.String() + `","items":[]}`,
		`{"merchant":"M","occurredAt":"2026-10-14T12:00:00Z","paymentAccountId":"` + foreignAccount.String() + `","itemName":"","itemAmount":"","categoryId":"","items":[]}`,
		`{"merchant":"M","occurredAt":"2026-10-14T12:00:00Z","itemName":"","itemAmount":"","categoryId":"","items":[{"key":"x","name":"x","amount":"1","categoryId":"","allocations":[{"poolId":"` + foreignPool.String() + `","weight":1}]}]}`,
		`{"merchant":"M","occurredAt":"2026-10-14T12:00:00Z","itemName":"","itemAmount":"","categoryId":"","items":[{"key":"x","name":"x","amount":"1","categoryId":"","productVariantId":"` + foreignVariant.String() + `","allocations":[]}]}`,
	}
	for i, payload := range foreignPayloads {
		if _, err := service.CreateSharedPurchaseDraft(ctx, owner.String(), householdID.String(), payload); !errors.Is(err, purchase.ErrBadInput) {
			t.Errorf("foreign reference payload %d error = %v, want bad input", i, err)
		}
	}
	outsiderResult, err := resolver.CreateSharedPurchaseDraft(outsiderContext, model.CreateSharedPurchaseDraftInput{
		HouseholdID: householdID.String(), Payload: validPayload,
	})
	if err != nil {
		t.Fatal(err)
	}
	outsiderError, ok := outsiderResult.(*model.SharedPurchaseDraftUserError)
	if !ok || outsiderError.Code != model.MutationErrorCodeNotFound {
		t.Fatalf("outsider create result = %#v, want not-found user error", outsiderResult)
	}
}

func createSharedDraftTestMember(t *testing.T, ctx context.Context, db *pgxpool.Pool, label string) gofrsuuid.UUID {
	t.Helper()
	id, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	subject, name := label+":"+id.String(), label
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &id, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(id)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	return id
}

func createSharedDraftTestHousehold(t *testing.T, ctx context.Context, db *pgxpool.Pool, members ...gofrsuuid.UUID) gofrsuuid.UUID {
	t.Helper()
	id, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	name := "Shared purchase draft test"
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &id, Name: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(id)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	for _, memberID := range members {
		role := "OWNER"
		if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &id, MemberID: &memberID, Role: &role}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	return id
}
