export type ProductSuggestionResponse<T> = {
  householdId: string;
  query: string;
  merchant: string | null;
  suggestions: T[];
} | null;

export function productSuggestionsForQuery<T>(
  householdId: string,
  query: string,
  merchant: string | null,
  response: ProductSuggestionResponse<T>,
): T[] {
  const currentQuery = query.trim();
  const currentMerchant = merchant?.trim() || null;
  return response?.householdId === householdId &&
    response.query === currentQuery &&
    response.merchant === currentMerchant
    ? response.suggestions
    : [];
}
