import type { Purchase } from "../../../shared/api";
import type { PurchaseDraftItem } from "../../../shared/lib";

export type PurchaseEditSnapshot = {
  items: PurchaseDraftItem[];
  categoryId?: string;
  merchant?: string;
  copiedPurchase?: Purchase | null;
};

export type PurchaseEditHistory = {
  past: PurchaseEditSnapshot[];
  future: PurchaseEditSnapshot[];
};

export function emptyPurchaseEditHistory(): PurchaseEditHistory {
  return { past: [], future: [] };
}

export function recordPurchaseEdit(
  history: PurchaseEditHistory,
  snapshot: PurchaseEditSnapshot,
  limit = 50,
): PurchaseEditHistory {
  return {
    past: [...history.past, snapshot].slice(-limit),
    future: [],
  };
}

function snapshotCurrent(
  current: PurchaseEditSnapshot,
  scope: PurchaseEditSnapshot,
): PurchaseEditSnapshot {
  return {
    items: current.items,
    ...(Object.hasOwn(scope, "categoryId") ? { categoryId: current.categoryId } : {}),
    ...(Object.hasOwn(scope, "merchant") ? { merchant: current.merchant } : {}),
    ...(Object.hasOwn(scope, "copiedPurchase") ? { copiedPurchase: current.copiedPurchase } : {}),
  };
}

export function undoPurchaseEdit(
  history: PurchaseEditHistory,
  current: PurchaseEditSnapshot,
): { history: PurchaseEditHistory; snapshot: PurchaseEditSnapshot } | null {
  const snapshot = history.past.at(-1);
  if (!snapshot) return null;
  return {
    history: {
      past: history.past.slice(0, -1),
      future: [snapshotCurrent(current, snapshot), ...history.future],
    },
    snapshot,
  };
}

export function redoPurchaseEdit(
  history: PurchaseEditHistory,
  current: PurchaseEditSnapshot,
): { history: PurchaseEditHistory; snapshot: PurchaseEditSnapshot } | null {
  const snapshot = history.future.at(0);
  if (!snapshot) return null;
  return {
    history: {
      past: [...history.past, snapshotCurrent(current, snapshot)],
      future: history.future.slice(1),
    },
    snapshot,
  };
}
