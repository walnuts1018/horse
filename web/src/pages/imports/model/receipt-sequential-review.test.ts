import { describe, expect, it } from "vitest";

import type { DocumentReview } from "./imports";

import {
  nextReceiptToReview,
  receiptReviewProgress,
  reviewableReceipts,
} from "./receipt-sequential-review";

function receipt(id: string, overrides: Partial<DocumentReview> = {}): DocumentReview {
  return {
    id,
    sourceKind: "receipt",
    status: "SUCCEEDED",
    result: { receipt: { merchant: id } },
    ...overrides,
  };
}

describe("reviewableReceipts", () => {
  it("解析済みで未確認のレシートだけを一覧順で返す", () => {
    const documents = [
      receipt("first"),
      receipt("processing", { status: "RUNNING" }),
      receipt("statement", { sourceKind: "card" }),
      receipt("no-result", { result: undefined }),
      receipt("duplicate", { duplicateOf: "first" }),
      receipt("already-confirmed", {
        confirmations: [{ sourceReference: "", purchaseId: "purchase-1" }],
      }),
      receipt("last", { status: "READY" }),
    ];

    expect(reviewableReceipts(documents, new Set(["first"])).map(({ id }) => id)).toEqual(["last"]);
  });

  it("確認済み数と分母を同じreceipt対象集合から数える", () => {
    const documents = [
      receipt("pending"),
      receipt("saved", {
        confirmations: [{ sourceReference: "", purchaseId: "purchase-1" }],
      }),
      receipt("not-a-receipt", { sourceKind: "order" }),
      receipt("failed", { status: "FAILED" }),
    ];

    expect(receiptReviewProgress(documents, new Set())).toEqual({
      receipts: [documents[0]],
      completedCount: 1,
      totalCount: 2,
    });
  });
});

describe("nextReceiptToReview", () => {
  it("次のレシートへ進み、末尾では先頭へ戻る", () => {
    const receipts = [receipt("first"), receipt("second"), receipt("third")];

    expect(nextReceiptToReview(receipts, "second")?.id).toBe("third");
    expect(nextReceiptToReview(receipts, "third")?.id).toBe("first");
    expect(nextReceiptToReview([], "first")).toBeUndefined();
  });
});
