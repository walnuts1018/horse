import type { Purchase } from "../../../shared/api";

export type PurchaseDraft = {
  merchant: string;
  occurredAt: string;
  paymentAllocations: Array<{ paymentAccountId: string; amountYen: string }>;
};

export function createPurchaseDraft(purchase: Purchase): PurchaseDraft {
  const occurredAt = new Date(purchase.occurredAt);
  occurredAt.setMinutes(occurredAt.getMinutes() - occurredAt.getTimezoneOffset());
  return {
    merchant: purchase.merchant,
    occurredAt: occurredAt.toISOString().slice(0, 16),
    paymentAllocations: purchase.paymentAllocations.map((payment) => ({
      paymentAccountId: payment.paymentAccountId,
      amountYen: String(payment.amountYen),
    })),
  };
}
