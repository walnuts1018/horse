import { describe, expect, it } from "vitest";

import type { ProductSuggestion } from "../../../entities/product";

import { visibleProductCandidates } from "./product-candidate-visibility";

describe("visibleProductCandidates", () => {
  it("keeps the server order in the initial shortlist and expanded results", () => {
    const suggestions = Array.from({ length: 7 }, (_, index) => ({
      concept: { id: `concept-${index}`, name: `商品${index}` },
      variant: { id: `variant-${index}`, name: `規格${index}` },
      matchedName: `商品${index}`,
      score: 1 - index / 10,
    })) satisfies ProductSuggestion[];

    expect(visibleProductCandidates(suggestions, false)).toEqual(suggestions.slice(0, 5));
    expect(visibleProductCandidates(suggestions, true)).toEqual(suggestions);
  });
});
