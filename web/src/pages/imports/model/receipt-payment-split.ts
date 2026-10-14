import type { ReceiptPaymentDraft } from "./imports";

export function addReceiptPayment(
  payments: ReceiptPaymentDraft[],
  payment: ReceiptPaymentDraft,
): ReceiptPaymentDraft[] {
  return [...payments, { ...payment, amountYen: 0 }];
}

export function updateReceiptPaymentAmount(
  payments: ReceiptPaymentDraft[],
  key: string,
  amountYen: number,
  totalYen: number,
): ReceiptPaymentDraft[] {
  const amount = Math.max(0, amountYen);
  const updated = payments.map((payment) =>
    payment.key === key ? { ...payment, amountYen: amount } : payment,
  );
  if (updated.length !== 2 || !updated.some((payment) => payment.key === key)) return updated;

  return updated.map((payment) =>
    payment.key === key
      ? payment
      : Object.assign({}, payment, { amountYen: Math.max(0, totalYen - amount) }),
  );
}
