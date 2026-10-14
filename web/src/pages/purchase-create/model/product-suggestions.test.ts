import { describe, expect, it } from "vitest";

import { productSuggestionsForQuery } from "./product-suggestions";

describe("productSuggestionsForQuery", () => {
  it("returns suggestions only for the current trimmed query", () => {
    const suggestions = [{ id: "variant-1", matchedName: "milk" }];
    const response = {
      householdId: "household-1",
      query: "milk",
      merchant: "Store A",
      suggestions,
    };

    expect(productSuggestionsForQuery("household-1", "  milk ", " Store A ", response)).toEqual(
      suggestions,
    );
    expect(productSuggestionsForQuery("household-1", "coffee", "Store A", response)).toEqual([]);
    expect(productSuggestionsForQuery("household-1", "milk", "Store B", response)).toEqual([]);
    expect(productSuggestionsForQuery("household-2", "milk", "Store A", response)).toEqual([]);
  });

  it("returns no suggestions before a matching response arrives", () => {
    expect(productSuggestionsForQuery("household-1", "milk", null, null)).toEqual([]);
  });

  it("hides extra candidates when the first candidate exactly matches the query", () => {
    const exact = { id: "variant-1", matchedName: "MILK" };
    const response = {
      householdId: "household-1",
      query: "milk",
      merchant: null,
      suggestions: [exact, { id: "variant-2", matchedName: "milk drink" }],
    };

    expect(productSuggestionsForQuery("household-1", "milk", null, response)).toEqual([exact]);
  });
});
