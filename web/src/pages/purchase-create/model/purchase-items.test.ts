import * as fc from "fast-check";
import { describe, expect, it } from "vitest";

import {
  resolvePurchaseAllocations,
  type PurchaseAllocationRule,
} from "./purchase-allocation-rules";
import {
  convertPurchaseItems,
  restorePurchaseItemsValue,
  suggestedPurchaseItemCategory,
} from "./purchase-items";

describe("purchase allocation rule selection", () => {
  const rules: PurchaseAllocationRule[] = [
    {
      target: { kind: "merchant", key: "スーパー a" },
      allocations: [{ poolId: "merchant", weight: 1 }],
    },
    {
      target: { kind: "category", id: "category" },
      allocations: [{ poolId: "category", weight: 1 }],
    },
    { target: { kind: "concept", id: "concept" }, allocations: [{ poolId: "concept", weight: 1 }] },
    { target: { kind: "variant", id: "variant" }, allocations: [{ poolId: "variant", weight: 1 }] },
  ];

  it("matches the backend priority from variant through merchant", () => {
    const concepts = new Map([["variant", "concept"]]);
    expect(
      resolvePurchaseAllocations(rules, concepts, "  SUPER  A ", "category", "variant"),
    ).toEqual(rules[3].allocations);
    expect(
      resolvePurchaseAllocations(rules.slice(0, 3), concepts, "スーパー A", "category", "variant"),
    ).toEqual(rules[2].allocations);
    expect(
      resolvePurchaseAllocations(rules.slice(0, 2), concepts, "スーパー A", "category"),
    ).toEqual(rules[1].allocations);
    expect(resolvePurchaseAllocations(rules.slice(0, 1), concepts, "スーパー  A", "")).toEqual(
      rules[0].allocations,
    );
  });

  it("returns no match when no target rule applies", () => {
    expect(resolvePurchaseAllocations([], new Map(), "スーパー A", "category")).toBeNull();
  });
});

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

  it("leaves the item uncategorized when matching history disagrees", () => {
    expect(
      suggestedPurchaseItemCategory(
        "牛乳",
        "",
        false,
        [...history, { items: [{ name: "牛乳", categoryId: "drink" }] }],
        () => true,
      ),
    ).toBe("");
  });

  it("leaves the item uncategorized when any exact history entry is uncategorized", () => {
    expect(
      suggestedPurchaseItemCategory(
        "牛乳",
        "",
        false,
        [...history, { items: [{ name: "牛乳", categoryId: null }] }],
        () => true,
      ),
    ).toBe("");
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
