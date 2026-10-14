import { describe, expect, it } from "vitest";

import { receiptItemsNeedingReview } from "./receipt-item-review";

describe("receiptItemsNeedingReview", () => {
  it("AIの認識状態ではなく人の状態で未確認品目を安定key順に返す", () => {
    const items = [
      {
        key: "source:milk",
        name: "牛乳",
        printedAmount: 200,
        grossAmount: 216,
        confidence: 0.99,
        verification: "VERIFIED",
      },
      {
        key: "source:bread",
        name: "パン",
        printedAmount: 100,
        grossAmount: 108,
        confidence: 0.2,
        verification: "UNRESOLVED",
      },
      {
        key: "manual:new",
        name: "",
        printedAmount: 0,
        grossAmount: 0,
        confidence: 0,
        verification: "MANUAL",
      },
    ];

    expect(
      receiptItemsNeedingReview(items, {
        "source:bread": "CONFIRMED",
        "manual:new": "UNRESOLVED",
      }),
    ).toEqual([
      { item: items[0], index: 0, state: "UNREVIEWED" },
      { item: items[2], index: 2, state: "UNRESOLVED" },
    ]);
  });
});
