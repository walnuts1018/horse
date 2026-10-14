import { describe, expect, it } from "vitest";

import type { DocumentReview } from "./imports";

import { createReceiptPurchaseInput } from "./receipt-confirmation";

describe("createReceiptPurchaseInput", () => {
  const document: DocumentReview = {
    id: "document-1",
    status: "REVIEW",
    result: {
      receipt: {
        merchant: "スーパーA",
        items: [
          {
            key: "item-1",
            name: "牛乳",
            printedAmount: 200,
            grossAmount: 220,
            confidence: 1,
            verification: "VERIFIED",
          },
          {
            key: "item-2",
            name: "パン",
            printedAmount: 100,
            grossAmount: 110,
            confidence: 1,
            verification: "VERIFIED",
          },
        ],
      },
      candidates: [
        {
          id: "candidate-1",
          amounts: [210, 105],
          computedTotal: 315,
          consistent: true,
          issues: [],
        },
      ],
    },
  };

  it("金額候補、複数決済、品目ごとの配賦をMutation inputへ反映する", () => {
    expect(
      createReceiptPurchaseInput({
        document,
        householdId: "household-1",
        occurredAt: "2026-10-14T03:00:00.000Z",
        candidateId: "candidate-1",
        requiresCandidateSelection: true,
        requiresOverrideReason: false,
        overrideReason: "",
        paymentDraft: [
          { key: "payment-1", paymentAccountId: "account-1", amountYen: 200 },
          { key: "payment-2", paymentAccountId: "account-2", amountYen: 110 },
        ],
        accountCount: 2,
        manualAmounts: { "item-1": 205 },
        allocations: { "item-2": [{ poolId: "pool-1", weight: 100 }] },
        suggestedCategoryIds: ["category-1", undefined],
      }),
    ).toMatchObject({
      householdId: "household-1",
      origin: "AI",
      merchant: "スーパーA",
      receiptCandidateId: "candidate-1",
      paymentAllocations: [
        { paymentAccountId: "account-1", amountYen: 200 },
        { paymentAccountId: "account-2", amountYen: 110 },
      ],
      items: [
        { name: "牛乳", amountYen: 205, categoryId: "category-1", allocations: null },
        {
          name: "パン",
          amountYen: 105,
          categoryId: null,
          allocations: [{ poolId: "pool-1", weight: 100 }],
        },
      ],
    });
  });

  it("選択した税計算候補の品目額を帳簿登録額として保存する", () => {
    const input = createReceiptPurchaseInput({
      document,
      householdId: "household-1",
      occurredAt: "2026-10-14T03:00:00.000Z",
      candidateId: "candidate-1",
      requiresCandidateSelection: true,
      requiresOverrideReason: false,
      overrideReason: "",
      paymentDraft: [{ key: "payment-1", paymentAccountId: "account-1", amountYen: 315 }],
      accountCount: 1,
      manualAmounts: undefined,
      allocations: undefined,
      suggestedCategoryIds: [],
    });

    expect(input.items.map((item) => item.amountYen)).toEqual([210, 105]);
    expect(input.items.reduce((total, item) => total + item.amountYen, 0)).toBe(315);
  });

  it("決済合計が補正後の品目合計と一致しない場合は拒否する", () => {
    expect(() =>
      createReceiptPurchaseInput({
        document,
        householdId: "household-1",
        occurredAt: "2026-10-14T03:00:00.000Z",
        candidateId: "candidate-1",
        requiresCandidateSelection: true,
        requiresOverrideReason: false,
        overrideReason: "",
        paymentDraft: [{ key: "payment-1", paymentAccountId: "account-1", amountYen: 314 }],
        accountCount: 1,
        manualAmounts: undefined,
        allocations: undefined,
        suggestedCategoryIds: [],
      }),
    ).toThrow("決済内訳と購入額を一致させてください");
  });
});
