package query

import (
	"context"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const productSuggestionCandidateLimit = 500

type ProductSuggestion struct {
	Concept     *models.ProductConcept
	Variant     *models.ProductVariant
	MatchedName string
	Score       int
}

type ProductSuggestions struct {
	Items             []ProductSuggestion
	VariantsByConcept map[gofrsuuid.UUID][]*models.ProductVariant
	AliasesByVariant  map[gofrsuuid.UUID][]*models.ProductAlias
}

type rankedProductSuggestion struct {
	variant                             *models.ProductVariant
	concept                             *models.ProductConcept
	score                               int
	memberPurchaseCount                 int
	memberLastPurchasedAt               int64
	purchaseCount                       int
	lastPurchasedAt                     int64
	sameMerchant                        bool
	conceptName, variantName, variantID string
	matchedName                         string
}

type productPurchaseStats struct {
	memberPurchaseCount   int
	memberLastPurchasedAt int64
	purchaseCount         int
	lastPurchasedAt       int64
	sameMerchant          bool
}

func SearchProductSuggestions(ctx context.Context, db bob.Executor, householdID, memberID gofrsuuid.UUID, searchKey, merchantKey string, pageSize int) (*ProductSuggestions, error) {
	escapedSearchKey := escapeProductSearchPattern(searchKey)
	prefixPattern := escapedSearchKey + "%"
	searchPattern := "%" + escapedSearchKey + "%"
	variantRows, err := models.ProductVariants.Query(
		sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)), psql.Not(models.ProductVariants.Columns.Archived), models.ProductVariants.Columns.NormalizedName.Like(psql.Arg(prefixPattern)))),
		sm.OrderBy(models.ProductVariants.Columns.NormalizedName), sm.OrderBy(models.ProductVariants.Columns.ID),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	partialVariantRows, err := models.ProductVariants.Query(
		sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)), psql.Not(models.ProductVariants.Columns.Archived), models.ProductVariants.Columns.NormalizedName.Like(psql.Arg(searchPattern)), psql.Not(models.ProductVariants.Columns.NormalizedName.Like(psql.Arg(prefixPattern))))),
		sm.OrderBy(models.ProductVariants.Columns.NormalizedName), sm.OrderBy(models.ProductVariants.Columns.ID),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	variantRows = append(variantRows, partialVariantRows...)
	variantByID := make(map[gofrsuuid.UUID]*models.ProductVariant, len(variantRows))
	for _, variantRow := range variantRows {
		variantByID[variantRow.ID] = variantRow
	}
	conceptRows, err := models.ProductConcepts.Query(
		sm.Where(psql.And(models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdID)), psql.Not(models.ProductConcepts.Columns.Archived), models.ProductConcepts.Columns.NormalizedName.Like(psql.Arg(prefixPattern)))),
		sm.OrderBy(models.ProductConcepts.Columns.NormalizedName), sm.OrderBy(models.ProductConcepts.Columns.ID),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	partialConceptRows, err := models.ProductConcepts.Query(
		sm.Where(psql.And(models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdID)), psql.Not(models.ProductConcepts.Columns.Archived), models.ProductConcepts.Columns.NormalizedName.Like(psql.Arg(searchPattern)), psql.Not(models.ProductConcepts.Columns.NormalizedName.Like(psql.Arg(prefixPattern))))),
		sm.OrderBy(models.ProductConcepts.Columns.NormalizedName), sm.OrderBy(models.ProductConcepts.Columns.ID),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	conceptRows = append(conceptRows, partialConceptRows...)
	conceptIDs := make([]any, 0, len(conceptRows))
	for _, conceptRow := range conceptRows {
		conceptIDs = append(conceptIDs, conceptRow.ID)
	}
	for start := 0; start < len(conceptIDs); start += productSuggestionCandidateLimit {
		end := min(start+productSuggestionCandidateLimit, len(conceptIDs))
		conceptVariantRows, err := models.ProductVariants.Query(
			sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)), psql.Not(models.ProductVariants.Columns.Archived), models.ProductVariants.Columns.ConceptID.In(psql.Arg(conceptIDs[start:end]...)))),
			sm.OrderBy(models.ProductVariants.Columns.NormalizedName), sm.OrderBy(models.ProductVariants.Columns.ID),
		).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, variantRow := range conceptVariantRows {
			variantByID[variantRow.ID] = variantRow
		}
	}
	matchedAliases := make(map[gofrsuuid.UUID]*models.ProductAlias)
	if merchantKey != "" {
		aliasRows, err := models.ProductAliases.Query(
			sm.Where(psql.And(models.ProductAliases.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.ProductAliases.Columns.MerchantKey.EQ(psql.Arg(merchantKey)), models.ProductAliases.Columns.NormalizedAlias.Like(psql.Arg(prefixPattern)))),
			sm.OrderBy(models.ProductAliases.Columns.NormalizedAlias), sm.OrderBy(models.ProductAliases.Columns.ID),
		).All(ctx, db)
		if err != nil {
			return nil, err
		}
		partialAliasRows, err := models.ProductAliases.Query(
			sm.Where(psql.And(models.ProductAliases.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.ProductAliases.Columns.MerchantKey.EQ(psql.Arg(merchantKey)), models.ProductAliases.Columns.NormalizedAlias.Like(psql.Arg(searchPattern)), psql.Not(models.ProductAliases.Columns.NormalizedAlias.Like(psql.Arg(prefixPattern))))),
			sm.OrderBy(models.ProductAliases.Columns.NormalizedAlias), sm.OrderBy(models.ProductAliases.Columns.ID),
		).All(ctx, db)
		if err != nil {
			return nil, err
		}
		aliasRows = append(aliasRows, partialAliasRows...)
		aliasVariantIDs := make([]any, 0, len(aliasRows))
		aliasVariantIDSet := make(map[gofrsuuid.UUID]struct{}, len(aliasRows))
		for _, aliasRow := range aliasRows {
			if _, ok := aliasVariantIDSet[aliasRow.VariantID]; !ok {
				aliasVariantIDSet[aliasRow.VariantID] = struct{}{}
				aliasVariantIDs = append(aliasVariantIDs, aliasRow.VariantID)
			}
			matchedAliases[aliasRow.VariantID] = preferredProductAlias(matchedAliases[aliasRow.VariantID], aliasRow, searchKey)
		}
		for start := 0; start < len(aliasVariantIDs); start += productSuggestionCandidateLimit {
			end := min(start+productSuggestionCandidateLimit, len(aliasVariantIDs))
			aliasVariantRows, err := models.ProductVariants.Query(
				sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.ProductVariants.Columns.ID.In(psql.Arg(aliasVariantIDs[start:end]...)), psql.Not(models.ProductVariants.Columns.Archived))),
			).All(ctx, db)
			if err != nil {
				return nil, err
			}
			for _, variantRow := range aliasVariantRows {
				variantByID[variantRow.ID] = variantRow
			}
		}
	}
	conceptByID := make(map[gofrsuuid.UUID]*models.ProductConcept, len(conceptRows))
	for _, conceptRow := range conceptRows {
		conceptByID[conceptRow.ID] = conceptRow
	}
	conceptUUIDs := make([]any, 0, len(variantByID))
	conceptUUIDSet := make(map[gofrsuuid.UUID]struct{}, len(variantByID))
	for _, variantRow := range variantByID {
		if _, ok := conceptUUIDSet[variantRow.ConceptID]; !ok {
			conceptUUIDSet[variantRow.ConceptID] = struct{}{}
			conceptUUIDs = append(conceptUUIDs, variantRow.ConceptID)
		}
	}
	for start := 0; start < len(conceptUUIDs); start += productSuggestionCandidateLimit {
		end := min(start+productSuggestionCandidateLimit, len(conceptUUIDs))
		rows, err := models.ProductConcepts.Query(
			sm.Where(psql.And(models.ProductConcepts.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.ProductConcepts.Columns.ID.In(psql.Arg(conceptUUIDs[start:end]...)), psql.Not(models.ProductConcepts.Columns.Archived))),
		).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, conceptRow := range rows {
			conceptByID[conceptRow.ID] = conceptRow
		}
	}
	variantUUIDs := make([]any, 0, len(variantByID))
	for variantID := range variantByID {
		variantUUIDs = append(variantUUIDs, variantID)
	}
	purchaseStats, err := loadProductPurchaseStats(ctx, db, householdID, memberID, variantUUIDs, merchantKey)
	if err != nil {
		return nil, err
	}
	items := make([]rankedProductSuggestion, 0, len(variantByID))
	for _, variantRow := range variantByID {
		conceptRow := conceptByID[variantRow.ConceptID]
		if conceptRow == nil {
			continue
		}
		matchedAlias := matchedAliases[variantRow.ID]
		matchedName, score := variantRow.Name, 50
		if matchedAlias != nil {
			matchedName = matchedAlias.AliasText
		}
		switch {
		case matchedAlias != nil && matchedAlias.NormalizedAlias == searchKey:
			score = 100
		case variantRow.NormalizedName == searchKey:
			score = 95
		case conceptRow.NormalizedName == searchKey:
			score = 90
		case matchedAlias != nil && strings.HasPrefix(matchedAlias.NormalizedAlias, searchKey):
			score = 80
		case strings.HasPrefix(variantRow.NormalizedName, searchKey):
			score = 75
		case strings.HasPrefix(conceptRow.NormalizedName, searchKey):
			score = 70
		}
		stats := purchaseStats[variantRow.ID]
		items = append(items, rankedProductSuggestion{
			variant: variantRow, concept: conceptRow, score: score,
			memberPurchaseCount: stats.memberPurchaseCount, memberLastPurchasedAt: stats.memberLastPurchasedAt,
			purchaseCount: stats.purchaseCount, lastPurchasedAt: stats.lastPurchasedAt, sameMerchant: stats.sameMerchant,
			conceptName: conceptRow.NormalizedName, variantName: variantRow.NormalizedName,
			variantID: variantRow.ID.String(), matchedName: matchedName,
		})
	}
	slices.SortFunc(items, compareProductSuggestions)
	if len(items) > pageSize {
		items = items[:pageSize]
	}
	result := &ProductSuggestions{
		Items:             make([]ProductSuggestion, 0, len(items)),
		VariantsByConcept: make(map[gofrsuuid.UUID][]*models.ProductVariant),
		AliasesByVariant:  make(map[gofrsuuid.UUID][]*models.ProductAlias),
	}
	if len(items) == 0 {
		return result, nil
	}
	selectedConceptIDs := make(map[gofrsuuid.UUID]struct{}, len(items))
	for _, item := range items {
		selectedConceptIDs[item.concept.ID] = struct{}{}
	}
	selectedConceptUUIDs := make([]any, 0, len(selectedConceptIDs))
	for id := range selectedConceptIDs {
		selectedConceptUUIDs = append(selectedConceptUUIDs, id)
	}
	selectedVariantRows, err := models.ProductVariants.Query(
		sm.Where(psql.And(models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.ProductVariants.Columns.ConceptID.In(psql.Arg(selectedConceptUUIDs...)))),
		sm.OrderBy(models.ProductVariants.Columns.NormalizedName), sm.OrderBy(models.ProductVariants.Columns.ID),
	).All(ctx, db)
	if err != nil {
		return nil, err
	}
	selectedVariantIDs := make([]any, 0, len(selectedVariantRows))
	for _, variantRow := range selectedVariantRows {
		selectedVariantIDs = append(selectedVariantIDs, variantRow.ID)
		result.VariantsByConcept[variantRow.ConceptID] = append(result.VariantsByConcept[variantRow.ConceptID], variantRow)
	}
	if len(selectedVariantIDs) > 0 {
		aliasRows, err := models.ProductAliases.Query(
			sm.Where(psql.And(models.ProductAliases.Columns.HouseholdID.EQ(psql.Arg(householdID)), models.ProductAliases.Columns.VariantID.In(psql.Arg(selectedVariantIDs...)))),
			sm.OrderBy(models.ProductAliases.Columns.MerchantKey), sm.OrderBy(models.ProductAliases.Columns.NormalizedAlias),
		).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, aliasRow := range aliasRows {
			result.AliasesByVariant[aliasRow.VariantID] = append(result.AliasesByVariant[aliasRow.VariantID], aliasRow)
		}
	}
	for _, item := range items {
		result.Items = append(result.Items, ProductSuggestion{Concept: item.concept, Variant: item.variant, MatchedName: item.matchedName, Score: item.score})
	}
	return result, nil
}

func loadProductPurchaseStats(ctx context.Context, db bob.Executor, householdID, memberID gofrsuuid.UUID, variantIDs []any, merchantKey string) (map[gofrsuuid.UUID]productPurchaseStats, error) {
	stats := make(map[gofrsuuid.UUID]productPurchaseStats)
	if len(variantIDs) == 0 {
		return stats, nil
	}
	items := make([]*models.PurchaseItem, 0)
	purchaseIDSet := make(map[gofrsuuid.UUID]struct{})
	for start := 0; start < len(variantIDs); start += productSuggestionCandidateLimit {
		end := min(start+productSuggestionCandidateLimit, len(variantIDs))
		rows, err := models.PurchaseItems.Query(sm.Where(psql.And(
			models.PurchaseItems.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.PurchaseItems.Columns.ProductVariantID.IsNotNull(),
			models.PurchaseItems.Columns.ProductVariantID.In(psql.Arg(variantIDs[start:end]...)),
		))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		items = append(items, rows...)
		for _, item := range rows {
			purchaseIDSet[item.PurchaseID] = struct{}{}
		}
	}
	purchaseIDs := make([]any, 0, len(purchaseIDSet))
	for purchaseID := range purchaseIDSet {
		purchaseIDs = append(purchaseIDs, purchaseID)
	}
	purchasesByID := make(map[gofrsuuid.UUID]*models.Purchase, len(purchaseIDs))
	for start := 0; start < len(purchaseIDs); start += productSuggestionCandidateLimit {
		end := min(start+productSuggestionCandidateLimit, len(purchaseIDs))
		rows, err := models.Purchases.Query(sm.Where(psql.And(
			models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Purchases.Columns.ID.In(psql.Arg(purchaseIDs[start:end]...)),
			models.Purchases.Columns.Status.In(psql.Arg("POSTED"), psql.Arg("CLOSED")),
		))).All(ctx, db)
		if err != nil {
			return nil, err
		}
		for _, purchase := range rows {
			purchasesByID[purchase.ID] = purchase
		}
	}
	countedPurchases := make(map[gofrsuuid.UUID]map[gofrsuuid.UUID]struct{})
	for _, item := range items {
		if !item.ProductVariantID.Valid {
			continue
		}
		purchase := purchasesByID[item.PurchaseID]
		if purchase == nil {
			continue
		}
		variantID := item.ProductVariantID.V
		seen := countedPurchases[variantID]
		if seen == nil {
			seen = make(map[gofrsuuid.UUID]struct{})
			countedPurchases[variantID] = seen
		}
		if _, ok := seen[purchase.ID]; ok {
			continue
		}
		seen[purchase.ID] = struct{}{}
		stat := stats[variantID]
		stat.purchaseCount++
		occurredAt := purchase.OccurredAt.UnixNano()
		if purchase.CreatedBy == memberID {
			stat.memberPurchaseCount++
			if occurredAt > stat.memberLastPurchasedAt {
				stat.memberLastPurchasedAt = occurredAt
			}
		}
		if occurredAt > stat.lastPurchasedAt {
			stat.lastPurchasedAt = occurredAt
		}
		if merchantKey != "" && normalizeSuggestionMerchant(purchase.Merchant) == merchantKey {
			stat.sameMerchant = true
		}
		stats[variantID] = stat
	}
	return stats, nil
}

func compareProductSuggestions(a, b rankedProductSuggestion) int {
	if a.score != b.score {
		return b.score - a.score
	}
	if a.memberPurchaseCount != b.memberPurchaseCount {
		return b.memberPurchaseCount - a.memberPurchaseCount
	}
	if a.memberLastPurchasedAt != b.memberLastPurchasedAt {
		if a.memberLastPurchasedAt > b.memberLastPurchasedAt {
			return -1
		}
		return 1
	}
	if a.sameMerchant != b.sameMerchant {
		if a.sameMerchant {
			return -1
		}
		return 1
	}
	if a.purchaseCount != b.purchaseCount {
		return b.purchaseCount - a.purchaseCount
	}
	if a.lastPurchasedAt != b.lastPurchasedAt {
		if a.lastPurchasedAt > b.lastPurchasedAt {
			return -1
		}
		return 1
	}
	if a.conceptName != b.conceptName {
		return strings.Compare(a.conceptName, b.conceptName)
	}
	if a.variantName != b.variantName {
		return strings.Compare(a.variantName, b.variantName)
	}
	return strings.Compare(a.variantID, b.variantID)
}

func normalizeSuggestionMerchant(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func escapeProductSearchPattern(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}

func preferredProductAlias(current, candidate *models.ProductAlias, searchKey string) *models.ProductAlias {
	if current == nil {
		return candidate
	}
	candidateExact := candidate.NormalizedAlias == searchKey
	currentExact := current.NormalizedAlias == searchKey
	if candidateExact != currentExact {
		if candidateExact {
			return candidate
		}
		return current
	}
	if candidate.Revision > current.Revision || candidate.Revision == current.Revision && candidate.ID.String() < current.ID.String() {
		return candidate
	}
	return current
}
