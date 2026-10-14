import { useCallback, useReducer } from "react";

import type { Household } from "../../../shared/api";

export type PaymentDraft = { key: string; paymentAccountId: string; amountYen: string };

type PurchasePaymentsState = {
  rows: PaymentDraft[];
  autoTotal: boolean;
  manualAmountEdited: boolean;
};

type PurchasePaymentsAction =
  | { type: "add"; payment: PaymentDraft; itemTotal: number }
  | { type: "patch"; key: string; value: Partial<PaymentDraft> }
  | { type: "remove"; key: string }
  | { type: "reset"; payment: PaymentDraft }
  | { type: "setRows"; rows: PaymentDraft[] }
  | { type: "setAutoTotal"; autoTotal: boolean; materializedAmount?: string }
  | { type: "setManualAmountEdited"; edited: boolean };

export function reducePurchasePayments(
  state: PurchasePaymentsState,
  action: PurchasePaymentsAction,
): PurchasePaymentsState {
  switch (action.type) {
    case "add":
      return {
        rows: [
          ...state.rows.map((payment, index) =>
            state.autoTotal && index === state.rows.length - 1
              ? {
                  ...payment,
                  amountYen: String(purchasePaymentAmount(state, payment, action.itemTotal)),
                }
              : payment,
          ),
          action.payment,
        ],
        autoTotal: state.autoTotal,
        manualAmountEdited:
          state.manualAmountEdited || (!state.autoTotal && state.rows.length === 1),
      };
    case "patch":
      return {
        ...state,
        rows: state.rows.map((payment) =>
          payment.key === action.key ? { ...payment, ...action.value } : payment,
        ),
        autoTotal:
          action.value.amountYen === undefined || state.rows.at(-1)?.key !== action.key
            ? state.autoTotal
            : false,
        manualAmountEdited: state.manualAmountEdited || action.value.amountYen !== undefined,
      };
    case "remove": {
      const rows = state.rows.filter((payment) => payment.key !== action.key);
      return {
        ...state,
        rows,
        autoTotal: rows.length === 1 && !state.manualAmountEdited ? true : state.autoTotal,
      };
    }
    case "reset":
      return { rows: [action.payment], autoTotal: true, manualAmountEdited: false };
    case "setRows":
      return { ...state, rows: action.rows };
    case "setAutoTotal":
      return {
        ...state,
        rows:
          !action.autoTotal && action.materializedAmount !== undefined && state.rows.length > 0
            ? state.rows.map((payment, index) =>
                index === state.rows.length - 1
                  ? { ...payment, amountYen: action.materializedAmount! }
                  : payment,
              )
            : state.rows,
        autoTotal: action.autoTotal,
        manualAmountEdited:
          state.manualAmountEdited ||
          (!action.autoTotal && action.materializedAmount !== undefined),
      };
    case "setManualAmountEdited":
      return { ...state, manualAmountEdited: action.edited };
  }
}

export function purchasePaymentAmount(
  state: PurchasePaymentsState,
  payment: PaymentDraft,
  itemTotal: number,
): number {
  if (state.autoTotal && state.rows.at(-1)?.key === payment.key) {
    const precedingTotal = state.rows
      .slice(0, -1)
      .reduce((sum, row) => sum + (Number(row.amountYen) || 0), 0);
    return itemTotal - precedingTotal;
  }
  return Number(payment.amountYen) || 0;
}

export function purchasePaymentDifference(state: PurchasePaymentsState, itemTotal: number): number {
  const total = state.rows.reduce(
    (sum, payment) => sum + purchasePaymentAmount(state, payment, itemTotal),
    0,
  );
  const autoRemainder =
    state.autoTotal && state.rows.length > 1
      ? purchasePaymentAmount(state, state.rows[state.rows.length - 1], itemTotal)
      : undefined;
  return autoRemainder !== undefined && autoRemainder < 0 ? autoRemainder : itemTotal - total;
}

export function usePurchasePayments(
  household: Household,
  itemTotal: number,
  onDirty: () => void,
  initialPaymentAccountId?: string,
) {
  const [state, dispatch] = useReducer(reducePurchasePayments, {
    rows: [createPayment(household, initialPaymentAccountId)],
    autoTotal: true,
    manualAmountEdited: false,
  });
  const { rows, autoTotal } = state;
  const amount = (payment: PaymentDraft) => purchasePaymentAmount(state, payment, itemTotal);
  const total = rows.reduce((sum, payment) => sum + amount(payment), 0);
  const difference = purchasePaymentDifference(state, itemTotal);

  const add = () => {
    onDirty();
    dispatch({
      type: "add",
      payment: { key: crypto.randomUUID(), paymentAccountId: "", amountYen: "" },
      itemTotal,
    });
  };
  const patch = (key: string, value: Partial<PaymentDraft>) => {
    onDirty();
    dispatch({ type: "patch", key, value });
  };
  const remove = (key: string) => {
    onDirty();
    dispatch({ type: "remove", key });
  };
  const reset = useCallback(
    (paymentAccountId?: string) => {
      dispatch({ type: "reset", payment: createPayment(household, paymentAccountId) });
    },
    [household],
  );

  return {
    rows,
    setRows: (nextRows: PaymentDraft[]) => dispatch({ type: "setRows", rows: nextRows }),
    autoTotal,
    setAutoTotal: (enabled: boolean, materializedAmount?: string) =>
      dispatch({ type: "setAutoTotal", autoTotal: enabled, materializedAmount }),
    manualAmountEdited: state.manualAmountEdited,
    setManualAmountEdited: (edited: boolean) => dispatch({ type: "setManualAmountEdited", edited }),
    amount,
    total,
    difference,
    add,
    patch,
    remove,
    reset,
  };
}

function createPayment(household: Household, paymentAccountId?: string): PaymentDraft {
  return {
    key: crypto.randomUUID(),
    paymentAccountId: resolveInitialPaymentAccountId(
      household.accounts.map((account) => account.id),
      paymentAccountId,
    ),
    amountYen: "",
  };
}

export function resolveInitialPaymentAccountId(accountIds: string[], preferredAccountId?: string) {
  if (preferredAccountId !== undefined) {
    return preferredAccountId && accountIds.includes(preferredAccountId) ? preferredAccountId : "";
  }
  return accountIds.length === 1 ? accountIds[0] : "";
}
