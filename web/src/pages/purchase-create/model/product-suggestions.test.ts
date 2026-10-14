import { describe, expect, it } from "vitest";

import { productSuggestionsForQuery } from "./product-suggestions";

describe("productSuggestionsForQuery", () => {
  it("returns suggestions only for the current trimmed query", () => {
    const suggestions = [{ id: "variant-1" }];

    expect(productSuggestionsForQuery("  milk ", { query: "milk", suggestions })).toBe(suggestions);
    expect(productSuggestionsForQuery("coffee", { query: "milk", suggestions })).toEqual([]);
  });

  it("returns no suggestions before a matching response arrives", () => {
    expect(productSuggestionsForQuery("milk", null)).toEqual([]);
  });
});
