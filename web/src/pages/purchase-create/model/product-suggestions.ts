export type ProductSuggestionResponse<T> = {
  query: string;
  suggestions: T[];
} | null;

export function productSuggestionsForQuery<T>(
  query: string,
  response: ProductSuggestionResponse<T>,
): T[] {
  const currentQuery = query.trim();
  return response?.query === currentQuery ? response.suggestions : [];
}
