import type { ProductSuggestion } from "../../../entities/product";
import type { Purchase } from "../../../shared/api";

export type ProductSuggestionResponse<
  T extends { matchedName: string } = { matchedName: string },
> = {
  householdId: string;
  query: string;
  merchant: string | null;
  suggestions: T[];
} | null;

export type PurchaseEntrySuggestion =
  | {
      kind: "confirmed";
      name: string;
      categoryId: string | null;
      candidate: ProductSuggestion;
    }
  | { kind: "history"; name: string }
  | { kind: "catalog"; candidate: ProductSuggestion };

type PurchaseHistoryItem = Pick<
  Purchase["items"][number],
  "name" | "productVariantId" | "categoryId"
>;
type PurchaseHistory = Array<{ items: PurchaseHistoryItem[] }>;
type PurchasePriceHistory = Array<
  Pick<Purchase, "occurredAt" | "merchant" | "status"> & {
    items: Array<Pick<Purchase["items"][number], "name" | "amountYen" | "productVariantId">>;
  }
>;

export type RecentPurchasePrice = {
  amountYen: number;
  occurredAt: string;
  merchant: string;
};

export function recentPurchasePrice(
  history: PurchasePriceHistory,
  name: string,
  productVariantId: string | null,
): RecentPurchasePrice | null {
  const normalizedName = name.trim().toLocaleLowerCase();
  if (!normalizedName) return null;

  let latest: RecentPurchasePrice | null = null;
  for (const purchase of history) {
    if (purchase.status === "DRAFT") continue;
    for (const item of purchase.items) {
      if (
        (productVariantId
          ? item.productVariantId !== productVariantId
          : item.name.trim().toLocaleLowerCase() !== normalizedName) ||
        item.amountYen <= 0
      ) {
        continue;
      }
      if (!latest || Date.parse(purchase.occurredAt) > Date.parse(latest.occurredAt)) {
        latest = {
          amountYen: item.amountYen,
          occurredAt: purchase.occurredAt,
          merchant: purchase.merchant,
        };
      }
    }
  }
  return latest;
}

export function purchaseEntrySuggestions(
  history: PurchaseHistory,
  query: string,
  catalog: ProductSuggestion[],
): PurchaseEntrySuggestion[] {
  const normalizedQuery = query.trim().toLocaleLowerCase();
  if (!normalizedQuery) return [];

  const catalogByVariant = new Map(catalog.map((candidate) => [candidate.variant.id, candidate]));
  const confirmed: PurchaseEntrySuggestion[] = [];
  const confirmedVariants = new Set<string>();
  const confirmedNames = new Set<string>();
  const historyNames = new Set<string>();
  for (const item of history.flatMap((purchase) => purchase.items)) {
    const normalizedName = item.name.toLocaleLowerCase();
    if (!normalizedName.includes(normalizedQuery)) continue;
    const candidate = item.productVariantId
      ? catalogByVariant.get(item.productVariantId)
      : undefined;
    if (
      candidate &&
      item.productVariantId &&
      !confirmedVariants.has(item.productVariantId) &&
      confirmed.length < 4
    ) {
      confirmedVariants.add(item.productVariantId);
      confirmedNames.add(normalizedName);
      confirmed.push({
        kind: "confirmed",
        name: item.name,
        categoryId: item.categoryId ?? null,
        candidate,
      });
    } else if (!historyNames.has(normalizedName)) {
      historyNames.add(normalizedName);
    }
  }

  const names = [
    ...confirmed,
    ...[...historyNames]
      .filter((name) => !confirmedNames.has(name))
      .slice(0, 4)
      .map((name) => ({ kind: "history" as const, name })),
  ];
  const catalogSuggestions = catalog
    .filter((candidate) => !confirmedVariants.has(candidate.variant.id))
    .map((candidate) => ({ kind: "catalog" as const, candidate }));
  return [...names, ...catalogSuggestions];
}

export function productSuggestionsForQuery<
  T extends { matchedName: string } = { matchedName: string },
>(
  householdId: string,
  query: string,
  merchant: string | null,
  response: ProductSuggestionResponse<T>,
): T[] {
  const currentQuery = query.trim();
  const currentMerchant = merchant?.trim() || null;
  if (
    response?.householdId !== householdId ||
    response.query !== currentQuery ||
    response.merchant !== currentMerchant
  ) {
    return [];
  }

  if (response.suggestions.length === 0) return [];
  const first = response.suggestions[0];
  return first.matchedName.trim().toLocaleLowerCase() === currentQuery.toLocaleLowerCase()
    ? [first]
    : response.suggestions;
}
