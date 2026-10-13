import type {
  Purchase,
  PurchaseCorrection,
  PurchaseHistoryQuery$data,
  PurchaseOrigin,
} from "../../../shared/api";
import { mapPurchaseItem } from "./map-purchase-item";

type PurchaseHistoryRecord = PurchaseHistoryQuery$data["purchases"]["edges"][number]["node"];

export function mapPurchase(record: PurchaseHistoryRecord): Purchase {
  return {
    ...record,
    sourceKinds: [...record.sourceKinds],
    origin: mapPurchaseOrigin(record.origin),
    corrections: record.corrections.edges.map(({ node }) => mapPurchaseCorrection(node)),
    items: record.items.map(mapPurchaseItem),
    paymentAllocations: record.paymentAllocations.map((payment) => ({
      paymentAccountId: payment.paymentAccountId,
      amountYen: payment.amountYen,
      account: payment.account,
    })),
  };
}

function mapPurchaseCorrection(
  correction: PurchaseHistoryRecord["corrections"]["edges"][number]["node"],
): PurchaseCorrection {
  const fields = {
    id: correction.id,
    amountDeltaYen: correction.amountDeltaYen,
    reason: correction.reason,
    occurredAt: correction.occurredAt,
    settlementMonth: correction.settlementMonth,
    paymentAllocations: correction.paymentAllocations.map((payment) => ({
      paymentAccountId: payment.paymentAccountId,
      amountYen: payment.amountYen,
      account: payment.account,
    })),
  };
  if (correction.itemId === undefined) return { ...fields, __typename: "PurchaseWideCorrection" };
  return {
    ...fields,
    __typename: "PurchaseItemCorrection",
    itemId: correction.itemId,
  };
}

function mapPurchaseOrigin(origin: PurchaseOrigin | "%future added value"): PurchaseOrigin {
  switch (origin) {
    case "AI":
    case "MANUAL":
    case "UNKNOWN":
      return origin;
    default:
      return "UNKNOWN";
  }
}
