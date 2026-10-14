import { describe, expect, it } from "vitest";

import type { DocumentReview } from "./imports";

import {
  createReceiptPurchaseInput,
  uniqueConsistentReceiptCandidate,
  receiptOverrideReasonRequired,
} from "./receipt-confirmation";

describe("receipt override reason requirement", () => {
  it("requires a reason only when the confirmed total differs from the printed total", () => {
    expect(receiptOverrideReasonRequired(300, 300)).toBe(false);
    expect(receiptOverrideReasonRequired(300, 299)).toBe(true);
    expect(receiptOverrideReasonRequired(undefined, 300)).toBe(true);
  });
});

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
        datePrecision: "DATE_TIME",
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
      datePrecision: "DATE_TIME",
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

  it("選択式の金額訂正理由を購入Mutation inputへ保存する", () => {
    const input = createReceiptPurchaseInput({
      document,
      householdId: "household-1",
      occurredAt: "2026-10-14T03:00:00.000Z",
      datePrecision: "DATE_TIME",
      candidateId: "candidate-1",
      requiresCandidateSelection: false,
      requiresOverrideReason: true,
      overrideReason: "OCRの読み間違い",
      paymentDraft: [],
      accountCount: 0,
      manualAmounts: { "item-1": 205 },
      allocations: undefined,
      suggestedCategoryIds: [],
    });

    expect(input.manualAmountOverrideReason).toBe("OCRの読み間違い");
    expect(input.items[0]?.amountYen).toBe(205);
  });

  it("決済合計が補正後の品目合計と一致しない場合は拒否する", () => {
    expect(() =>
      createReceiptPurchaseInput({
        document,
        householdId: "household-1",
        occurredAt: "2026-10-14T03:00:00.000Z",
        datePrecision: "DATE_TIME",
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

  it("決済口座が未選択の場合は確定を拒否する", () => {
    expect(() =>
      createReceiptPurchaseInput({
        document,
        householdId: "household-1",
        occurredAt: "2026-10-14T03:00:00.000Z",
        datePrecision: "DATE_TIME",
        candidateId: "candidate-1",
        requiresCandidateSelection: false,
        requiresOverrideReason: false,
        overrideReason: "",
        paymentDraft: [{ key: "payment-1", paymentAccountId: "", amountYen: 330 }],
        accountCount: 2,
        manualAmounts: undefined,
        allocations: undefined,
        suggestedCategoryIds: [],
      }),
    ).toThrow("決済口座を選択してください");
  });

  it("DATE_ONLYで日付だけのレシートを確定する", () => {
    const input = createReceiptPurchaseInput({
      document,
      householdId: "household-1",
      occurredAt: "2026-10-14T03:00:00.000Z",
      datePrecision: "DATE_ONLY",
      candidateId: "",
      requiresCandidateSelection: false,
      requiresOverrideReason: false,
      overrideReason: "",
      paymentDraft: [],
      accountCount: 0,
      manualAmounts: undefined,
      allocations: undefined,
      suggestedCategoryIds: [],
    });
    expect(input).toMatchObject({
      occurredAt: "2026-10-14T03:00:00.000Z",
      datePrecision: "DATE_ONLY",
    });
  });

  it("購入日を解決できない場合は確定を拒否する", () => {
    for (const occurredAt of [null, "invalid-date"]) {
      expect(() =>
        createReceiptPurchaseInput({
          document,
          householdId: "household-1",
          occurredAt,
          datePrecision: "DATE_ONLY",
          candidateId: "candidate-1",
          requiresCandidateSelection: false,
          requiresOverrideReason: false,
          overrideReason: "",
          paymentDraft: [],
          accountCount: 0,
          manualAmounts: undefined,
          allocations: undefined,
          suggestedCategoryIds: [],
        }),
      ).toThrow("購入日を入力してください");
    }
  });
});

describe("uniqueConsistentReceiptCandidate", () => {
  const candidateDocument: DocumentReview = {
    id: "document-1",
    status: "REVIEW",
    result: {
      receipt: {
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
      computedTotal: 315,
      balanced: true,
    },
  };

  it("合計が検証済みの候補が一件だけの場合に返す", () => {
    expect(uniqueConsistentReceiptCandidate(candidateDocument)).toEqual(
      candidateDocument.result?.candidates?.[0],
    );
  });

  it("複数候補または不整合の候補を自動選択しない", () => {
    const candidate = candidateDocument.result?.candidates?.[0];
    if (!candidate) throw new Error("候補fixtureがありません");
    const documents: DocumentReview[] = [
      {
        ...candidateDocument,
        result: {
          ...candidateDocument.result,
          candidates: [candidate, { ...candidate, id: "candidate-2" }],
        },
      },
      {
        ...candidateDocument,
        result: { ...candidateDocument.result, balanced: false },
      },
      {
        ...candidateDocument,
        result: { ...candidateDocument.result, computedTotal: 316 },
      },
    ];

    for (const document of documents) {
      expect(uniqueConsistentReceiptCandidate(document)).toBeUndefined();
    }
  });
});
