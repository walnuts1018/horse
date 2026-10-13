import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { convertPurchaseItems } from "./purchase-items";

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
