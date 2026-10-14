import { describe, expect, it } from "vitest";

import { receiptItemsNeedingReview } from "./receipt-item-review";

const sourceRegion = { page: 1, top: 0.1, left: 0.1, bottom: 0.2, right: 0.5 };

describe("receiptItemsNeedingReview", () => {
  it("根拠付きの正常な品目を除外し、問題のある品目だけを安定key順に返す", () => {
    const items = [
      {
        key: "source:milk",
        name: "牛乳",
        printedAmount: 200,
        grossAmount: 216,
        confidence: 0.99,
        verification: "VERIFIED",
        region: sourceRegion,
      },
      {
        key: "source:bread",
        name: "パン",
        printedAmount: 100,
        grossAmount: 108,
        confidence: 0.6,
        verification: "VERIFIED",
        region: sourceRegion,
      },
      {
        key: "source:tea",
        name: "お茶",
        printedAmount: 150,
        grossAmount: 162,
        confidence: 0.99,
        verification: "VERIFIED",
      },
      {
        key: "source:tax",
        name: "税解釈不明",
        printedAmount: 100,
        grossAmount: 100,
        confidence: 0.99,
        verification: "UNRESOLVED",
        region: sourceRegion,
      },
    ];

    expect(receiptItemsNeedingReview(items, {})).toEqual([
      { item: items[1], index: 1, state: "UNREVIEWED" },
      { item: items[2], index: 2, state: "UNREVIEWED" },
      { item: items[3], index: 3, state: "UNREVIEWED" },
    ]);
  });

  it("未解決状態を残し、確認済みの品目を対象から除く", () => {
    const items = [
      {
        key: "source:milk",
        name: "牛乳",
        printedAmount: 200,
        grossAmount: 216,
        confidence: 0.2,
        verification: "UNRESOLVED",
        region: sourceRegion,
      },
      {
        key: "manual:new",
        name: "手入力品",
        printedAmount: 300,
        grossAmount: 300,
        confidence: 0,
        verification: "MANUAL",
      },
      {
        key: "manual:valid",
        name: "記入済み品目",
        printedAmount: 180,
        grossAmount: 180,
        confidence: 0,
        verification: "MANUAL",
      },
      {
        key: "manual:invalid",
        name: "",
        printedAmount: 0,
        grossAmount: 0,
        confidence: 0,
        verification: "MANUAL",
      },
    ];

    expect(
      receiptItemsNeedingReview(items, {
        "source:milk": "UNRESOLVED",
        "manual:new": "CONFIRMED",
        "manual:invalid": "CONFIRMED",
      }),
    ).toEqual([
      { item: items[0], index: 0, state: "UNRESOLVED" },
      { item: items[3], index: 3, state: "CONFIRMED" },
    ]);
  });
});
