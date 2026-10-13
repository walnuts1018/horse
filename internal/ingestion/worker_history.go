package ingestion

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type historyCandidate struct {
	merchant       string
	printedName    string
	normalizedName string
	category       string
}

func (w *Worker) historyHints(ctx context.Context, householdID string, lines []OCRLine) ([]HistoryHint, error) {
	if len(lines) == 0 {
		return nil, nil
	}
	householdUUID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, fmt.Errorf("parse household id: %w", err)
	}
	executor := bobpgx.NewPool(w.DB)
	purchases, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		psql.Or(
			models.Purchases.Columns.Status.EQ(psql.Arg("POSTED")),
			models.Purchases.Columns.Status.EQ(psql.Arg("CLOSED")),
		),
	)), sm.OrderBy(models.Purchases.Columns.OccurredAt).Desc(), sm.OrderBy(models.Purchases.Columns.ID).Desc(), sm.Limit(20)).All(ctx, executor)
	if err != nil {
		return nil, fmt.Errorf("query recent purchases: %w", err)
	}
	if len(purchases) == 0 {
		return nil, nil
	}
	if err := purchases.LoadPurchaseItems(ctx, executor, sm.OrderBy(models.PurchaseItems.Columns.CreatedAt).Desc()); err != nil {
		return nil, fmt.Errorf("load recent purchase items: %w", err)
	}
	var items models.PurchaseItemSlice
	for _, purchase := range purchases {
		items = append(items, purchase.R.PurchaseItems...)
	}
	if len(items) == 0 {
		return nil, nil
	}
	if err := items.LoadProductVariant(ctx, executor); err != nil {
		return nil, fmt.Errorf("load recent product variants: %w", err)
	}
	if err := items.LoadCategory(ctx, executor); err != nil {
		return nil, fmt.Errorf("load recent item categories: %w", err)
	}
	merchantByPurchase := make(map[string]string, len(purchases))
	for _, purchase := range purchases {
		merchantByPurchase[purchase.ID.String()] = purchase.Merchant
	}
	candidates := make([]historyCandidate, 0, len(items))
	for _, item := range items {
		normalizedName := item.Name
		if item.R.ProductVariant != nil {
			if item.R.ProductVariant.NormalizedName != "" {
				normalizedName = item.R.ProductVariant.NormalizedName
			} else if item.R.ProductVariant.Name != "" {
				normalizedName = item.R.ProductVariant.Name
			}
		}
		candidate := historyCandidate{merchant: merchantByPurchase[item.PurchaseID.String()], printedName: item.Name, normalizedName: normalizedName}
		if item.R.Category != nil {
			candidate.category = item.R.Category.Name
		}
		candidates = append(candidates, candidate)
	}
	return selectHistoryHints(candidates, lines), nil
}

func selectHistoryHints(candidates []historyCandidate, lines []OCRLine) []HistoryHint {
	var source strings.Builder
	for _, line := range lines {
		source.WriteString(normalizeHistoryText(line.Text))
	}
	sourceText := source.String()
	if sourceText == "" {
		return nil
	}
	hints := make([]HistoryHint, 0, 10)
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		printedKey := normalizeHistoryText(candidate.printedName)
		normalizedKey := normalizeHistoryText(candidate.normalizedName)
		if len([]rune(printedKey)) < 3 && len([]rune(normalizedKey)) < 3 {
			continue
		}
		if (printedKey == "" || !strings.Contains(sourceText, printedKey)) && (normalizedKey == "" || !strings.Contains(sourceText, normalizedKey)) {
			continue
		}
		key := printedKey + "\x00" + normalizedKey
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		hints = append(hints, HistoryHint{Merchant: candidate.merchant, PrintedName: candidate.printedName, NormalizedName: candidate.normalizedName, Category: candidate.category})
		if len(hints) == 10 {
			break
		}
	}
	return hints
}

func normalizeHistoryText(value string) string {
	var normalized strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			normalized.WriteRune(r)
		}
	}
	return normalized.String()
}
