import { useState } from "react";
import type { Household } from "../../../shared/api";

export type PaymentDraft = { key: string; paymentAccountId: string; amountYen: string };

export function usePurchasePayments(household: Household, itemTotal: number, onDirty: () => void) {
  const [rows, setRows] = useState<PaymentDraft[]>(() => [createPayment(household)]);
  const [autoTotal, setAutoTotal] = useState(true);
  const amount = (payment: PaymentDraft) =>
    Number(autoTotal && rows.length === 1 ? itemTotal : payment.amountYen) || 0;
  const total = rows.reduce((sum, payment) => sum + amount(payment), 0);
  const difference = itemTotal - total;

  const add = () => {
    onDirty();
    setRows((current) => [
      ...current.map((payment) => ({
        ...payment,
        amountYen: autoTotal && current.length === 1 ? String(itemTotal) : payment.amountYen,
      })),
      { key: crypto.randomUUID(), paymentAccountId: "", amountYen: "" },
    ]);
    setAutoTotal(false);
  };
  const patch = (key: string, value: Partial<PaymentDraft>) => {
    onDirty();
    if (value.amountYen !== undefined) setAutoTotal(false);
    setRows((current) =>
      current.map((payment) => (payment.key === key ? { ...payment, ...value } : payment)),
    );
  };
  const remove = (key: string) => {
    onDirty();
    setRows((current) => current.filter((payment) => payment.key !== key));
  };
  const reset = () => {
    setRows([createPayment(household)]);
    setAutoTotal(true);
  };

  return {
    rows,
    setRows,
    autoTotal,
    setAutoTotal,
    amount,
    total,
    difference,
    add,
    patch,
    remove,
    reset,
  };
}

function createPayment(household: Household): PaymentDraft {
  return {
    key: crypto.randomUUID(),
    paymentAccountId: household.accounts[0]?.id ?? "",
    amountYen: "",
  };
}
