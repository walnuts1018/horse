import { describe, expect, it } from "vitest";

import type { ProductSuggestion } from "../../../entities/product";

import { productSuggestionsForQuery, purchaseEntrySuggestions } from "./product-suggestions";

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

  it("prioritizes historically confirmed products over purchase names and estimated catalog matches", () => {
    const catalog = [
      {
        variant: { id: "confirmed-variant", name: "Whole milk" },
        concept: { id: "confirmed-concept", name: "Milk" },
        matchedName: "milk",
        score: 100,
      },
      {
        variant: { id: "estimated-variant", name: "Milk drink" },
        concept: { id: "estimated-concept", name: "Milk drink" },
        matchedName: "milk drink",
        score: 75,
      },
    ] satisfies ProductSuggestion[];
    const history = [
      {
        items: [
          { name: "牛乳", productVariantId: "confirmed-variant", categoryId: "dairy" },
          { name: "牛乳ドリンク", productVariantId: null, categoryId: null },
          { name: "牛乳", productVariantId: "archived-variant", categoryId: "dairy" },
        ],
      },
    ] satisfies Parameters<typeof purchaseEntrySuggestions>[0];

    expect(purchaseEntrySuggestions(history, "牛乳", catalog)).toEqual([
      {
        kind: "confirmed",
        name: "牛乳",
        categoryId: "dairy",
        candidate: catalog[0],
      },
      { kind: "history", name: "牛乳ドリンク" },
      { kind: "catalog", candidate: catalog[1] },
    ]);
  });
});
