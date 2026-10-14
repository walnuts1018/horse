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
  | { type: "setAutoTotal"; autoTotal: boolean }
  | { type: "setManualAmountEdited"; edited: boolean };

export function reducePurchasePayments(
  state: PurchasePaymentsState,
  action: PurchasePaymentsAction,
): PurchasePaymentsState {
  switch (action.type) {
    case "add":
      return {
        rows: [
          ...state.rows.map((payment) => ({
            ...payment,
            amountYen:
              state.autoTotal && state.rows.length === 1
                ? String(action.itemTotal)
                : payment.amountYen,
          })),
          action.payment,
        ],
        autoTotal: false,
        manualAmountEdited:
          state.manualAmountEdited || (!state.autoTotal && state.rows.length === 1),
      };
    case "patch":
      return {
        ...state,
        rows: state.rows.map((payment) =>
          payment.key === action.key ? { ...payment, ...action.value } : payment,
        ),
        autoTotal: action.value.amountYen === undefined ? state.autoTotal : false,
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
      return { ...state, autoTotal: action.autoTotal };
    case "setManualAmountEdited":
      return { ...state, manualAmountEdited: action.edited };
  }
}

export function purchasePaymentAmount(
  state: PurchasePaymentsState,
  payment: PaymentDraft,
  itemTotal: number,
): number {
  return Number(state.autoTotal && state.rows.length === 1 ? itemTotal : payment.amountYen) || 0;
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
  const difference = itemTotal - total;

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
    setAutoTotal: (enabled: boolean) => dispatch({ type: "setAutoTotal", autoTotal: enabled }),
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
