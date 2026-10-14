import { describe, expect, it } from "vitest";

import type { ReceiptPaymentDraft } from "./imports";

import { addReceiptPayment, updateReceiptPaymentAmount } from "./receipt-payment-split";

const payments: ReceiptPaymentDraft[] = [
  { key: "first", paymentAccountId: "account-1", amountYen: 1200 },
];

describe("receipt payment split", () => {
  it("keeps the recognized payment and starts the added payment at zero", () => {
    expect(
      addReceiptPayment(payments, {
        key: "second",
        paymentAccountId: "account-2",
        amountYen: 0,
      }),
    ).toEqual([payments[0], { key: "second", paymentAccountId: "account-2", amountYen: 0 }]);
  });

  it("updates the other payment to the remaining total when the first amount changes", () => {
    expect(
      updateReceiptPaymentAmount(
        [payments[0], { key: "second", paymentAccountId: "account-2", amountYen: 0 }],
        "first",
        450,
        1200,
      ),
    ).toEqual([
      { ...payments[0], amountYen: 450 },
      { key: "second", paymentAccountId: "account-2", amountYen: 750 },
    ]);
  });

  it("updates the first payment when the second amount changes", () => {
    expect(
      updateReceiptPaymentAmount(
        [payments[0], { key: "second", paymentAccountId: "account-2", amountYen: 0 }],
        "second",
        300,
        1200,
      ),
    ).toEqual([
      { ...payments[0], amountYen: 900 },
      { key: "second", paymentAccountId: "account-2", amountYen: 300 },
    ]);
  });

  it("does not change other amounts when there are more than two payments", () => {
    const threePayments = [
      payments[0],
      { key: "second", paymentAccountId: "account-2", amountYen: 300 },
      { key: "third", paymentAccountId: "account-3", amountYen: 200 },
    ];

    expect(updateReceiptPaymentAmount(threePayments, "first", 500, 1200)).toEqual([
      { ...payments[0], amountYen: 500 },
      threePayments[1],
      threePayments[2],
    ]);
  });
});
