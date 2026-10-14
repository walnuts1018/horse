export type PurchaseDefaults = {
  merchant: string;
  paymentAccountId: string;
};

type StorageLike = Pick<Storage, "getItem" | "setItem">;

export function loadPurchaseDefaults(
  memberId: string,
  householdId: string,
  accountIds: string[],
): PurchaseDefaults {
  const fallback = { merchant: "", paymentAccountId: accountIds[0] ?? "" };
  const storage = getStorage();
  if (!storage) return fallback;

  try {
    const value = storage.getItem(storageKey(memberId, householdId));
    if (!value) return fallback;
    const parsed: unknown = JSON.parse(value);
    if (!isPurchaseDefaults(parsed)) return fallback;
    return {
      merchant: parsed.merchant,
      paymentAccountId: accountIds.includes(parsed.paymentAccountId)
        ? parsed.paymentAccountId
        : fallback.paymentAccountId,
    };
  } catch {
    return fallback;
  }
}

export function savePurchaseDefaults(
  memberId: string,
  householdId: string,
  defaults: PurchaseDefaults,
) {
  const storage = getStorage();
  if (!storage) return;

  try {
    storage.setItem(storageKey(memberId, householdId), JSON.stringify(defaults));
  } catch {
    return;
  }
}

function storageKey(memberId: string, householdId: string) {
  return `horse:purchase-defaults:${JSON.stringify([memberId, householdId])}`;
}

function getStorage(): StorageLike | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function isPurchaseDefaults(value: unknown): value is PurchaseDefaults {
  if (!value || typeof value !== "object") return false;
  const defaults = value as Partial<PurchaseDefaults>;
  return typeof defaults.merchant === "string" && typeof defaults.paymentAccountId === "string";
}
