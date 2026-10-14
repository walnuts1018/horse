export type ProductSuggestionResponse<
  T extends { matchedName: string } = { matchedName: string },
> = {
  householdId: string;
  query: string;
  merchant: string | null;
  suggestions: T[];
} | null;

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
