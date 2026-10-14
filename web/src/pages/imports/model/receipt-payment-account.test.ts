import { describe, expect, it } from "vitest";

import type { Account } from "../../../shared/api";

import { receiptPaymentAccountId } from "./receipt-payment-account";

const accounts: Account[] = [
  { id: "card-1234", name: "Visa", kind: "CARD", cardLast4: "1234" },
  { id: "card-5678", name: "Mastercard", kind: "CARD", cardLast4: "5678" },
  { id: "bank-1234", name: "Operating 1234", kind: "BANK", cardLast4: null },
];

describe("receiptPaymentAccountId", () => {
  it("抽出した末尾4桁と一致するカード口座が1件なら選択する", () => {
    expect(receiptPaymentAccountId(accounts, "クレジットカード", "1234")).toBe("card-1234");
  });

  it("一致がない場合は既存の手段名判定を維持する", () => {
    expect(receiptPaymentAccountId(accounts, "Mastercard", "9999")).toBe("card-5678");
  });

  it("末尾4桁の一致が複数なら既存判定が一意でない限り選択しない", () => {
    const duplicated = [...accounts, { ...accounts[1], id: "card-1234-other", cardLast4: "1234" }];

    expect(receiptPaymentAccountId(duplicated, "クレジットカード", "1234")).toBe("");
  });

  it("カード以外の口座に同じ末尾4桁があっても選択しない", () => {
    expect(receiptPaymentAccountId(accounts, "クレジットカード", "1234")).not.toBe("bank-1234");
  });

  it("4桁数字でない抽出値では既存判定を維持する", () => {
    expect(receiptPaymentAccountId(accounts, "Visa", "12345")).toBe("card-1234");
  });
});
