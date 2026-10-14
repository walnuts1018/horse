import { describe, expect, it } from "vitest";

import {
  purchasePaymentAmount,
  purchasePaymentDifference,
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

  it("assigns the remaining amount to the last account and follows earlier edits", () => {
    const split = reducePurchasePayments(initialState(), {
      type: "add",
      payment: { key: "second", paymentAccountId: "account-2", amountYen: "" },
      itemTotal: 1000,
    });
    const allocated = reducePurchasePayments(split, {
      type: "patch",
      key: "first",
      value: { amountYen: "350" },
    });

    expect(allocated.autoTotal).toBe(true);
    expect(purchasePaymentAmount(allocated, allocated.rows[1], 1000)).toBe(650);
    expect(purchasePaymentDifference(allocated, 1000)).toBe(0);
    expect(purchasePaymentAmount(allocated, allocated.rows[1], 1200)).toBe(850);
  });

  it("reports an over-allocation when the automatic remainder is negative", () => {
    const split = reducePurchasePayments(initialState(), {
      type: "add",
      payment: { key: "second", paymentAccountId: "account-2", amountYen: "" },
      itemTotal: 1000,
    });
    const overAllocated = reducePurchasePayments(split, {
      type: "patch",
      key: "first",
      value: { amountYen: "1250" },
    });

    expect(purchasePaymentAmount(overAllocated, overAllocated.rows[1], 1000)).toBe(-250);
    expect(purchasePaymentDifference(overAllocated, 1000)).toBe(-250);
  });

  it("materializes the remaining amount when switching to manual input", () => {
    const split = reducePurchasePayments(initialState(), {
      type: "add",
      payment: { key: "second", paymentAccountId: "account-2", amountYen: "" },
      itemTotal: 1000,
    });
    const adjusted = reducePurchasePayments(split, {
      type: "patch",
      key: "first",
      value: { amountYen: "420" },
    });
    const manual = reducePurchasePayments(adjusted, {
      type: "setAutoTotal",
      autoTotal: false,
      materializedAmount: "580",
    });

    expect(manual.autoTotal).toBe(false);
    expect(manual.rows[1].amountYen).toBe("580");
    expect(purchasePaymentDifference(manual, 1000)).toBe(0);
  });

  it("keeps automatic allocation enabled through add, restore, and removal", () => {
    const split = reducePurchasePayments(initialState(), {
      type: "add",
      payment: { key: "second", paymentAccountId: "account-2", amountYen: "" },
      itemTotal: 1000,
    });
    const third = reducePurchasePayments(split, {
      type: "add",
      payment: { key: "third", paymentAccountId: "account-3", amountYen: "" },
      itemTotal: 1000,
    });
    const restored = reducePurchasePayments(initialState(), { type: "setRows", rows: third.rows });
    const restoredAuto = reducePurchasePayments(restored, {
      type: "setAutoTotal",
      autoTotal: true,
    });
    const reduced = reducePurchasePayments(restoredAuto, { type: "remove", key: "third" });

    expect(restoredAuto.autoTotal).toBe(true);
    expect(purchasePaymentAmount(restoredAuto, restoredAuto.rows[2], 1400)).toBe(400);
    expect(reduced.autoTotal).toBe(true);
    expect(purchasePaymentAmount(reduced, reduced.rows[1], 1400)).toBe(400);
  });

  it("keeps automatic total active when removing the automatic remainder row", () => {
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

    expect(restored.autoTotal).toBe(true);
    expect(purchasePaymentAmount(restored, restored.rows[0], 1500)).toBe(1500);
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
