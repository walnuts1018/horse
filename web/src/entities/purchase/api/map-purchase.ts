import type { Purchase, PurchaseHistoryQuery$data, PurchaseOrigin } from "../../../shared/api";

import { mapPurchaseItem } from "./map-purchase-item";

type PurchaseHistoryRecord = PurchaseHistoryQuery$data["purchases"]["edges"][number]["node"];

export function mapPurchase(record: PurchaseHistoryRecord): Purchase {
  return {
    ...record,
    sourceKinds: [...record.sourceKinds],
    origin: mapPurchaseOrigin(record.origin),
    corrections: [],
    items: record.items.map(mapPurchaseItem),
    paymentAllocations: record.paymentAllocations.map((payment) => ({
      paymentAccountId: payment.paymentAccountId,
      amountYen: payment.amountYen,
      account: payment.account,
    })),
  };
}

function mapPurchaseOrigin(origin: PurchaseOrigin | "%future added value"): PurchaseOrigin {
  switch (origin) {
    case "AI":
    case "MANUAL":
    case "UNKNOWN":
      return origin;
    case "%future added value":
    default:
      return "UNKNOWN";
  }
}
