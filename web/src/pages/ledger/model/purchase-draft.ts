import type { Purchase } from "../../../shared/api";

import { purchaseDateInputValue } from "../../../entities/purchase";

export type PurchaseDraft = {
  merchant: string;
  occurredAt: string;
  datePrecision: Purchase["datePrecision"];
  paymentAllocations: Array<{ key: string; paymentAccountId: string; amountYen: string }>;
};

export function createPurchaseDraft(purchase: Purchase): PurchaseDraft {
  if (purchase.datePrecision === "DATE_ONLY") {
    return {
      merchant: purchase.merchant,
      occurredAt: purchaseDateInputValue(purchase.occurredAt),
      datePrecision: purchase.datePrecision,
      paymentAllocations: purchase.paymentAllocations.map((payment) => ({
        key: crypto.randomUUID(),
        paymentAccountId: payment.paymentAccountId,
        amountYen: String(payment.amountYen),
      })),
    };
  }
  const occurredAt = new Date(purchase.occurredAt);
  occurredAt.setMinutes(occurredAt.getMinutes() - occurredAt.getTimezoneOffset());
  return {
    merchant: purchase.merchant,
    occurredAt: occurredAt.toISOString().slice(0, 16),
    datePrecision: purchase.datePrecision,
    paymentAllocations: purchase.paymentAllocations.map((payment) => ({
      key: crypto.randomUUID(),
      paymentAccountId: payment.paymentAccountId,
      amountYen: String(payment.amountYen),
    })),
  };
}
