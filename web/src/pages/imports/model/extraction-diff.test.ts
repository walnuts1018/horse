import { describe, expect, it } from "vitest";

import { compareExtractions } from "./extraction-diff";

const region = { page: 0, top: 0.1, left: 0.1, bottom: 0.2, right: 0.8 };

describe("compareExtractions", () => {
  it("compares receipt fields for items matched by their stable region", () => {
    const diff = compareExtractions(
      {
        receipt: {
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
              key: "before-key",
              region,
            },
          ],
          taxGroups: [{ id: "tax-10", numerator: 10, denominator: 100, printedTax: 100 }],
          payments: [{ method: "クレジットカード", cardLast4: "1234", amount: 1100 }],
        },
      },
      {
        receipt: {
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
              key: "after-key",
              region,
            },
          ],
          taxGroups: [{ id: "tax-10", numerator: 8, denominator: 100, printedTax: 96 }],
          payments: [{ method: "デビットカード", cardLast4: "1234", amount: 1200 }],
        },
      },
    );

    expect(diff.receiptChanges).toEqual([{ label: "合計", before: "1,100円", after: "1,200円" }]);
    expect(diff.itemChanges).toEqual([
      {
        id: "changed-0",
        status: "changed",
        label: "明細1・コーヒー豆",
        changes: [
          { label: "品目名", before: "コーヒー", after: "コーヒー豆" },
          { label: "金額", before: "500円", after: "600円" },
          { label: "カテゴリ", before: "飲料", after: "食品" },
          { label: "税区分", before: "10%", after: "8%" },
        ],
      },
    ]);
    expect(diff.taxChanges[0].changes).toContainEqual({
      label: "印字税額",
      before: "100円",
      after: "96円",
    });
    expect(diff.paymentChanges[0].changes).toEqual([
      { label: "支払方法", before: "クレジットカード", after: "デビットカード" },
      { label: "支払額", before: "1,100円", after: "1,200円" },
    ]);
  });

  it("ignores a reorder when item names and amounts uniquely match", () => {
    const diff = compareExtractions(
      {
        receipt: {
          items: [
            {
              name: "パン",
              printedAmount: 100,
              grossAmount: 100,
              confidence: 1,
              verification: "verified",
            },
            {
              name: "牛乳",
              printedAmount: 200,
              grossAmount: 200,
              confidence: 1,
              verification: "verified",
            },
          ],
        },
      },
      {
        receipt: {
          items: [
            {
              name: "牛乳",
              printedAmount: 200,
              grossAmount: 200,
              confidence: 1,
              verification: "verified",
            },
            {
              name: "パン",
              printedAmount: 100,
              grossAmount: 100,
              confidence: 1,
              verification: "verified",
            },
          ],
        },
      },
    );

    expect(diff.itemChanges).toEqual([]);
  });

  it("matches an unkeyed item when its name and amount uniquely identify it", () => {
    const diff = compareExtractions(
      {
        receipt: {
          items: [
            {
              name: "お茶",
              printedAmount: 110,
              grossAmount: 110,
              categoryHint: "飲料",
              taxGroup: "8%",
              confidence: 1,
              verification: "verified",
            },
          ],
        },
      },
      {
        receipt: {
          items: [
            {
              name: "お茶",
              printedAmount: 110,
              grossAmount: 110,
              categoryHint: "食品",
              taxGroup: "10%",
              confidence: 1,
              verification: "verified",
            },
          ],
        },
      },
    );

    expect(diff.itemChanges).toEqual([
      {
        id: "changed-0",
        status: "changed",
        label: "明細1・お茶",
        changes: [
          { label: "カテゴリ", before: "飲料", after: "食品" },
          { label: "税区分", before: "8%", after: "10%" },
        ],
      },
    ]);
  });

  it("leaves indistinguishable duplicate items unmatched", () => {
    const item = {
      name: "パン",
      printedAmount: 100,
      grossAmount: 100,
      categoryHint: "食品",
      taxGroup: "8%",
      confidence: 1,
      verification: "verified",
    };
    const diff = compareExtractions(
      { receipt: { items: [item, item] } },
      { receipt: { items: [item, item] } },
    );

    expect(diff.itemChanges.map(({ status }) => status)).toEqual([
      "unmatched-current",
      "unmatched-current",
      "unmatched-previous",
      "unmatched-previous",
    ]);
  });

  it("does not confirm additions or removals for duplicate item identities", () => {
    const item = {
      name: "パン",
      printedAmount: 100,
      grossAmount: 100,
      confidence: 1,
      verification: "verified",
      key: "duplicate",
      region,
    };
    const diff = compareExtractions(
      { receipt: { items: [item, item] } },
      { receipt: { items: [item, item] } },
    );

    expect(diff.itemChanges.map(({ status }) => status)).toEqual([
      "unmatched-current",
      "unmatched-current",
      "unmatched-previous",
      "unmatched-previous",
    ]);
  });

  it("ignores reordering when item keys are stable", () => {
    const first = {
      name: "パン",
      printedAmount: 100,
      grossAmount: 100,
      confidence: 1,
      verification: "verified",
      key: "a",
    };
    const second = {
      name: "牛乳",
      printedAmount: 200,
      grossAmount: 200,
      confidence: 1,
      verification: "verified",
      key: "b",
    };

    const diff = compareExtractions(
      { receipt: { items: [first, second] } },
      { receipt: { items: [second, first] } },
    );

    expect(diff.itemChanges).toEqual([]);
  });

  it("reports added and removed receipt items when keys are stable", () => {
    const diff = compareExtractions(
      {
        receipt: {
          items: [
            {
              name: "既存品",
              printedAmount: 100,
              grossAmount: 100,
              confidence: 1,
              verification: "verified",
              key: "existing",
            },
            {
              name: "削除品",
              printedAmount: 200,
              grossAmount: 200,
              confidence: 1,
              verification: "verified",
              key: "removed",
            },
          ],
        },
      },
      {
        receipt: {
          items: [
            {
              name: "既存品",
              printedAmount: 100,
              grossAmount: 100,
              confidence: 1,
              verification: "verified",
              key: "existing",
            },
            {
              name: "追加品",
              printedAmount: 300,
              grossAmount: 300,
              confidence: 1,
              verification: "verified",
              key: "added",
            },
          ],
        },
      },
    );

    expect(diff.itemChanges.map(({ status, label }) => [status, label])).toEqual([
      ["added", "明細2・追加品"],
      ["removed", "明細2・削除品"],
    ]);
  });

  it("compares statement rows by source reference and reports added or removed rows", () => {
    const diff = compareExtractions(
      {
        statements: [
          {
            row: 2,
            sourceReference: "card:purchase:1",
            kind: "CARD_PURCHASE",
            occurredAt: "2026-10-01T03:00:00Z",
            merchant: "喫茶店",
            amount: 1200,
            transactionId: "tx-1",
            cardLast4: "1234",
          },
          {
            row: 3,
            sourceReference: "card:purchase:2",
            kind: "CARD_PURCHASE",
            occurredAt: "2026-10-02T03:00:00Z",
            merchant: "書店",
            amount: 800,
            transactionId: "tx-2",
          },
        ],
      },
      {
        statements: [
          {
            row: 3,
            sourceReference: "card:purchase:1",
            kind: "REFUND",
            occurredAt: "2026-10-01T04:00:00Z",
            merchant: "喫茶店",
            amount: -1200,
            transactionId: "tx-1-new",
            orderId: "order-1",
            cardLast4: "9876",
          },
          {
            row: 4,
            sourceReference: "card:purchase:3",
            kind: "CARD_PURCHASE",
            occurredAt: "2026-10-03T03:00:00Z",
            merchant: "スーパー",
            amount: 500,
          },
        ],
      },
    );

    expect(diff.statementChanges.map(({ status }) => status)).toEqual([
      "changed",
      "added",
      "removed",
    ]);
    expect(diff.statementChanges[0].changes).toEqual([
      { label: "金額", before: "1,200円", after: "-1,200円" },
      { label: "日時", before: "2026/10/01 12:00", after: "2026/10/01 13:00" },
      { label: "種別", before: "カード利用", after: "返金" },
      { label: "取引ID", before: "tx-1", after: "tx-1-new" },
      { label: "注文ID", before: "未設定", after: "order-1" },
      { label: "カード番号下4桁", before: "1234", after: "9876" },
    ]);
    expect(diff.statementChanges[1].changes).toContainEqual({
      label: "加盟店",
      before: "未設定",
      after: "スーパー",
    });
    expect(diff.statementChanges[2].changes).toContainEqual({
      label: "加盟店",
      before: "書店",
      after: "未設定",
    });
  });

  it("ignores statement ordering when source references are stable", () => {
    const first = {
      row: 1,
      sourceReference: "bank:1",
      kind: "EXTERNAL_INCOME",
      occurredAt: "2026-10-01",
      merchant: "給与",
      amount: 300000,
    };
    const second = {
      row: 2,
      sourceReference: "bank:2",
      kind: "CARD_BILL_PAYMENT",
      occurredAt: "2026-10-02",
      merchant: "カード引落し",
      amount: -12000,
    };

    const diff = compareExtractions(
      { statements: [first, second] },
      { statements: [second, first] },
    );

    expect(diff.statementChanges).toEqual([]);
  });
});
