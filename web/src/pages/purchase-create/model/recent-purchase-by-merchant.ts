import type { Purchase } from "../../../shared/api";

export function purchaseMerchantKey(merchant: string) {
  return merchant.normalize("NFKC").trim().toLocaleLowerCase("ja-JP");
}

export function indexRecentPurchasesByMerchant(purchases: Purchase[]) {
  const recentPurchases = new Map<string, Purchase>();

  for (const purchase of purchases) {
    const merchant = purchaseMerchantKey(purchase.merchant);
    if (!merchant) continue;

    const current = recentPurchases.get(merchant);
    if (!current || Date.parse(purchase.occurredAt) > Date.parse(current.occurredAt)) {
      recentPurchases.set(merchant, purchase);
    }
  }

  return recentPurchases;
}
