import { describe, expect, it } from "vitest";

import { compareReceiptExtractions } from "./extraction-diff";

describe("compareReceiptExtractions", () => {
  it("lists only changed receipt, item, tax and payment values", () => {
    const diff = compareReceiptExtractions(
      {
        total: 1100,
        items: [
          {
            name: "コーヒー",
            printedAmount: 500,
            grossAmount: 500,
            categoryHint: "飲料",
            taxGroup: "10%",
            confidence: 1,
            verification: "verified",
          },
        ],
        taxGroups: [{ id: "tax-10", numerator: 10, denominator: 100, printedTax: 100 }],
        payments: [{ method: "現金", amount: 1100 }],
      },
      {
        total: 1200,
        items: [
          {
            name: "コーヒー豆",
            printedAmount: 600,
            grossAmount: 600,
            categoryHint: "食品",
            taxGroup: "8%",
            confidence: 1,
            verification: "verified",
          },
        ],
        taxGroups: [{ id: "tax-10", numerator: 8, denominator: 100, printedTax: 96 }],
        payments: [{ method: "クレジットカード", cardLast4: "1234", amount: 1200 }],
      },
    );

    expect(diff.receiptChanges).toEqual([{ label: "合計", before: "1,100円", after: "1,200円" }]);
    expect(diff.itemChanges[0]).toEqual({
      label: "明細1・コーヒー豆",
      changes: [
        { label: "品目名", before: "コーヒー", after: "コーヒー豆" },
        { label: "金額", before: "500円", after: "600円" },
        { label: "カテゴリ", before: "飲料", after: "食品" },
        { label: "税区分", before: "10%", after: "8%" },
      ],
    });
    expect(diff.taxChanges[0].changes).toContainEqual({
      label: "印字税額",
      before: "100円",
      after: "96円",
    });
    expect(diff.paymentChanges[0].changes).toEqual([
      { label: "支払方法", before: "現金", after: "クレジットカード" },
      { label: "カード番号下4桁", before: "未設定", after: "1234" },
      { label: "支払額", before: "1,100円", after: "1,200円" },
    ]);
  });

  it("reports added and removed rows", () => {
    const diff = compareReceiptExtractions(
      {
        items: [
          {
            name: "前の品目",
            printedAmount: 300,
            grossAmount: 300,
            confidence: 1,
            verification: "verified",
          },
        ],
      },
      {
        items: [
          {
            name: "新しい品目",
            printedAmount: 200,
            grossAmount: 200,
            confidence: 1,
            verification: "verified",
          },
          {
            name: "追加品",
            printedAmount: 100,
            grossAmount: 100,
            confidence: 1,
            verification: "verified",
          },
        ],
      },
    );

    expect(diff.itemChanges).toHaveLength(2);
    expect(diff.itemChanges[0].changes).toContainEqual({
      label: "品目名",
      before: "前の品目",
      after: "新しい品目",
    });
    expect(diff.itemChanges[1].changes).toContainEqual({
      label: "品目名",
      before: "未設定",
      after: "追加品",
    });
  });
});
