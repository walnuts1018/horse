import { describe, expect, it } from "vitest";

import type { ImportReceiptDraft } from "../../../shared/lib/purchase-storage";
import type { DocumentReview } from "./imports";

import { reconcileReceiptReviewDraft } from "./receipt-review-draft";

function item(name: string, printedAmount: number, grossAmount: number, confidence = 0.9) {
  return { name, printedAmount, grossAmount, confidence, verification: "UNRESOLVED" };
}

function draftResult(value: NonNullable<DocumentReview["result"]>) {
  return value as Record<string, unknown>;
}

describe("reconcileReceiptReviewDraft", () => {
  it("AIの更新を取り込みながらユーザーが変更した項目だけを維持する", () => {
    const baseResult = {
      receipt: {
        merchant: "AI店舗",
        items: [item("AI商品", 100, 100, 0.7), item("未編集商品", 200, 200, 0.8)],
      },
      issues: ["旧指摘"],
    };
    const editedResult = {
      ...baseResult,
      receipt: {
        ...baseResult.receipt,
        merchant: "修正店舗",
        items: [{ ...baseResult.receipt.items[0], name: "修正商品" }, baseResult.receipt.items[1]],
      },
    };
    const latest: DocumentReview = {
      id: "document-1",
      status: "SUCCEEDED",
      result: {
        receipt: {
          merchant: "更新店舗",
          items: [
            item("更新商品", 100, 110, 0.99),
            item("更新された未編集商品", 220, 220, 0.99),
            item("新商品", 300, 300),
          ],
        },
        issues: ["新指摘"],
      },
    };
    const draft: ImportReceiptDraft = {
      householdId: "household-1",
      documentId: latest.id,
      baseResult: draftResult(baseResult),
      editedResult: draftResult(editedResult),
    };

    expect(reconcileReceiptReviewDraft(latest, draft).document.result).toEqual({
      receipt: {
        merchant: "修正店舗",
        items: [
          item("修正商品", 100, 110, 0.99),
          item("更新された未編集商品", 220, 220, 0.99),
          item("新商品", 300, 300),
        ],
      },
      issues: ["新指摘"],
    });
  });

  it("AI再解析で品目順が変わった場合も一意な品目へ修正を戻す", () => {
    const baseResult = {
      receipt: { items: [item("牛乳", 200, 220), item("パン", 100, 110)] },
    };
    const editedResult = {
      receipt: {
        items: [{ ...baseResult.receipt.items[0], name: "低脂肪乳" }, baseResult.receipt.items[1]],
      },
    };
    const latest: DocumentReview = {
      id: "document-1",
      status: "SUCCEEDED",
      result: { receipt: { items: [item("パン", 100, 110), item("牛乳", 200, 220)] } },
    };
    const reconciled = reconcileReceiptReviewDraft(latest, {
      householdId: "household-1",
      documentId: latest.id,
      baseResult: draftResult(baseResult),
      editedResult: draftResult(editedResult),
    });

    expect(reconciled.document.result?.receipt?.items).toEqual([
      item("パン", 100, 110),
      item("低脂肪乳", 200, 220),
    ]);
    expect(reconciled.conflicts).toEqual([]);
  });

  it("AI再解析後に一意の品目を特定できない修正を別品目へ適用せず競合にする", () => {
    const baseResult = { receipt: { items: [item("牛乳", 200, 220)] } };
    const editedResult = {
      receipt: { items: [{ ...baseResult.receipt.items[0], name: "低脂肪乳" }] },
    };
    const latest: DocumentReview = {
      id: "document-1",
      status: "SUCCEEDED",
      result: { receipt: { items: [item("ヨーグルト", 201, 220)] } },
    };
    const reconciled = reconcileReceiptReviewDraft(latest, {
      householdId: "household-1",
      documentId: latest.id,
      baseResult: draftResult(baseResult),
      editedResult: draftResult(editedResult),
    });

    expect(reconciled.document.result?.receipt?.items?.[0]?.name).toBe("ヨーグルト");
    expect(reconciled.conflicts).toEqual(["品目1"]);
  });
});
