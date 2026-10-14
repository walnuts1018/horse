import { describe, expect, it } from "vitest";

import {
  purchasePaymentAmount,
  resolveInitialPaymentAccountId,
  reducePurchasePayments,
  type PaymentDraft,
} from "./purchase-payments";

const firstPayment: PaymentDraft = {
  key: "first",
  paymentAccountId: "account-1",
  amountYen: "",
};

function initialState() {
  return { rows: [firstPayment], autoTotal: true, manualAmountEdited: false };
}

describe("purchase payment allocation", () => {
  it("preselects only a saved account or the sole available account", () => {
    expect(resolveInitialPaymentAccountId(["account-1", "account-2"])).toBe("");
    expect(resolveInitialPaymentAccountId(["account-1"])).toBe("account-1");
    expect(resolveInitialPaymentAccountId(["account-1"], "")).toBe("");
    expect(resolveInitialPaymentAccountId(["account-1", "account-2"], "account-2")).toBe(
      "account-2",
    );
    expect(resolveInitialPaymentAccountId(["account-1", "account-2"], "removed-account")).toBe("");
    expect(resolveInitialPaymentAccountId(["account-1"], "removed-account")).toBe("");
  });

  it("restores automatic total after removing a split that was not manually edited", () => {
    const split = reducePurchasePayments(initialState(), {
      type: "add",
      payment: { key: "second", paymentAccountId: "account-2", amountYen: "" },
      itemTotal: 1000,
    });
    const restored = reducePurchasePayments(split, { type: "remove", key: "second" });

    expect(restored.autoTotal).toBe(true);
    expect(purchasePaymentAmount(restored, firstPayment, 1500)).toBe(1500);
  });

  it("keeps a manually entered amount after removing the other payment", () => {
    const split = reducePurchasePayments(initialState(), {
      type: "add",
      payment: { key: "second", paymentAccountId: "account-2", amountYen: "" },
      itemTotal: 1000,
    });
    const edited = reducePurchasePayments(split, {
      type: "patch",
      key: "first",
      value: { amountYen: "700" },
    });
    const restored = reducePurchasePayments(edited, { type: "remove", key: "second" });

    expect(restored.autoTotal).toBe(false);
    expect(purchasePaymentAmount(restored, restored.rows[0], 1500)).toBe(700);
  });

  it("keeps a manually entered single payment amount after a split is added and removed", () => {
    const edited = reducePurchasePayments(initialState(), {
      type: "patch",
      key: "first",
      value: { amountYen: "850" },
    });
    const split = reducePurchasePayments(edited, {
      type: "add",
      payment: { key: "second", paymentAccountId: "account-2", amountYen: "" },
      itemTotal: 1000,
    });
    const restored = reducePurchasePayments(split, { type: "remove", key: "second" });

    expect(restored.autoTotal).toBe(false);
    expect(purchasePaymentAmount(restored, restored.rows[0], 1500)).toBe(850);
  });

  it("preserves manual amount intent when restoring a split draft", () => {
    const split = reducePurchasePayments(initialState(), {
      type: "add",
      payment: { key: "second", paymentAccountId: "account-2", amountYen: "" },
      itemTotal: 1000,
    });
    const edited = reducePurchasePayments(split, {
      type: "patch",
      key: "first",
      value: { amountYen: "700" },
    });
    const restoredDraft = reducePurchasePayments(initialState(), {
      type: "setRows",
      rows: edited.rows,
    });
    const restoredMode = reducePurchasePayments(restoredDraft, {
      type: "setAutoTotal",
      autoTotal: false,
    });
    const restoredIntent = reducePurchasePayments(restoredMode, {
      type: "setManualAmountEdited",
      edited: edited.manualAmountEdited,
    });
    const onePayment = reducePurchasePayments(restoredIntent, { type: "remove", key: "second" });

    expect(onePayment.autoTotal).toBe(false);
    expect(purchasePaymentAmount(onePayment, onePayment.rows[0], 1500)).toBe(700);
  });
});
