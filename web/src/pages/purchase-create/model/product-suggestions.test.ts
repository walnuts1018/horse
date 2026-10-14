import { describe, expect, it } from "vitest";

import type { ProductSuggestion } from "../../../entities/product";

import {
  productSuggestionsForQuery,
  purchaseEntrySuggestions,
  recentPurchasePrice,
} from "./product-suggestions";

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

describe("recentPurchasePrice", () => {
  it("returns the latest positive price for the selected variant", () => {
    const history = [
      {
        status: "POSTED",
        occurredAt: "2026-01-10T12:00:00Z",
        merchant: "古い店",
        items: [{ name: "牛乳", amountYen: 180, productVariantId: "variant-1" }],
      },
      {
        status: "POSTED",
        occurredAt: "2026-02-10T12:00:00Z",
        merchant: "別商品",
        items: [{ name: "牛乳", amountYen: 230, productVariantId: "variant-2" }],
      },
      {
        status: "CLOSED",
        occurredAt: "2026-03-10T12:00:00Z",
        merchant: "新しい店",
        items: [{ name: "牛乳 1L", amountYen: 198, productVariantId: "variant-1" }],
      },
      {
        status: "DRAFT",
        occurredAt: "2026-04-10T12:00:00Z",
        merchant: "下書き",
        items: [{ name: "牛乳", amountYen: 160, productVariantId: "variant-1" }],
      },
    ] satisfies Parameters<typeof recentPurchasePrice>[0];

    expect(recentPurchasePrice(history, "牛乳", "variant-1")).toEqual({
      amountYen: 198,
      occurredAt: "2026-03-10T12:00:00Z",
      merchant: "新しい店",
    });
  });

  it("requires an exact item name and a positive price when no variant is selected", () => {
    const history = [
      {
        status: "POSTED",
        occurredAt: "2026-01-10T12:00:00Z",
        merchant: "店",
        items: [
          { name: "牛乳ドリンク", amountYen: 190, productVariantId: "variant-1" },
          { name: "牛乳", amountYen: 0, productVariantId: null },
        ],
      },
    ] satisfies Parameters<typeof recentPurchasePrice>[0];

    expect(recentPurchasePrice(history, "牛乳", null)).toBeNull();
  });
});
