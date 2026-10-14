import type { ProductSuggestion } from "../../../entities/product";

export const INITIAL_CANDIDATE_COUNT = 5;

export function visibleProductCandidates(suggestions: ProductSuggestion[], expanded: boolean) {
  return expanded ? suggestions : suggestions.slice(0, INITIAL_CANDIDATE_COUNT);
}
