import * as fc from "fast-check";
import { describe, expect, it } from "vitest";

import {
  convertPurchaseItems,
  restorePurchaseItemsValue,
  suggestedPurchaseItemCategory,
} from "./purchase-items";

describe("suggested purchase item category", () => {
  const history = [
    { items: [{ name: "牛乳", categoryId: "dairy" }] },
    { items: [{ name: "コーヒー", categoryId: "drink" }] },
  ];

  it("uses the previous category for an exact item name", () => {
    expect(suggestedPurchaseItemCategory(" 牛乳 ", "", false, history, () => true)).toBe("dairy");
  });

  it("preserves an existing or explicitly selected category, including uncategorized", () => {
    expect(suggestedPurchaseItemCategory("牛乳", "drink", false, history, () => true)).toBe("");
    expect(suggestedPurchaseItemCategory("牛乳", "", true, history, () => true)).toBe("");
  });

  it("does not suggest archived categories", () => {
    expect(suggestedPurchaseItemCategory("牛乳", "", false, history, (id) => id === "drink")).toBe(
      "",
    );
  });
});

describe("purchase item draft restoration", () => {
  it("restores the selected product variant with the unfinished item", () => {
    const restored = restorePurchaseItemsValue(
      {
        itemName: "牛乳",
        itemAmount: "258",
        selectedProductVariantId: "variant-1",
        categoryId: "category-1",
        items: [],
      },
      (id) => id === "category-1",
    );

    expect(restored).toMatchObject({
      itemName: "牛乳",
      itemAmount: "258",
      selectedProductVariantId: "variant-1",
      categoryId: "category-1",
    });
  });

  it("loads older drafts without a selected product variant", () => {
    const restored = restorePurchaseItemsValue(
      { itemName: "牛乳", itemAmount: "258", categoryId: "", items: [] },
      () => false,
    );

    expect(restored.selectedProductVariantId).toBe("");
  });
});

describe("purchase item conversion", () => {
  it("金額の合計を保ち、正の割当だけを送信する", () => {
    const arbitraryDraftItem = fc.record({
      key: fc.uuid(),
      name: fc.string(),
      amount: fc.integer({ min: 0, max: 1_000_000 }).map(String),
      categoryId: fc.string(),
      productVariantId: fc.option(fc.string(), { nil: undefined }),
      allocations: fc.array(
        fc.record({
          poolId: fc.uuid(),
          weight: fc.integer({ min: -100, max: 100 }),
        }),
      ),
    });

    fc.assert(
      fc.property(fc.array(arbitraryDraftItem, { minLength: 1, maxLength: 30 }), (draftItems) => {
        const converted = convertPurchaseItems(draftItems);

        expect(converted.reduce((total, item) => total + item.amountYen, 0)).toBe(
          draftItems.reduce((total, item) => total + Number(item.amount), 0),
        );
        for (const [index, item] of converted.entries()) {
          const allocations = draftItems[index].allocations;
          expect(item.allocations).toEqual(
            allocations.length ? allocations.filter(({ weight }) => weight > 0) : null,
          );
        }
      }),
      { numRuns: 300 },
    );
  });
});
