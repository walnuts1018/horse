export type PurchaseDefaults = {
  merchant: string;
  paymentAccountId: string;
};

type StoredPurchaseDefaults = PurchaseDefaults & {
  paymentAccountIdsByMerchant?: Record<string, string>;
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
      paymentAccountId: paymentAccountForMerchant(parsed, parsed.merchant, accountIds),
    };
  } catch {
    return fallback;
  }
}

export function loadPaymentAccountForMerchant(
  memberId: string,
  householdId: string,
  merchant: string,
  accountIds: string[],
): string {
  const fallback = accountIds[0] ?? "";
  const storage = getStorage();
  if (!storage) return fallback;

  try {
    const value = storage.getItem(storageKey(memberId, householdId));
    if (!value) return fallback;
    const parsed: unknown = JSON.parse(value);
    if (!isPurchaseDefaults(parsed)) return fallback;
    return paymentAccountForMerchant(parsed, merchant, accountIds);
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
    const key = storageKey(memberId, householdId);
    const value = storage.getItem(key);
    let previous: unknown = null;
    try {
      previous = value ? JSON.parse(value) : null;
    } catch {
      previous = null;
    }
    const paymentAccountIdsByMerchant = isPurchaseDefaults(previous)
      ? validMerchantAccounts(previous.paymentAccountIdsByMerchant)
      : {};
    const merchant = defaults.merchant.trim();
    if (merchant) paymentAccountIdsByMerchant[merchant] = defaults.paymentAccountId;
    const next: StoredPurchaseDefaults = {
      ...defaults,
      paymentAccountIdsByMerchant,
    };
    storage.setItem(key, JSON.stringify(next));
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

function paymentAccountForMerchant(
  defaults: PurchaseDefaults & { paymentAccountIdsByMerchant?: unknown },
  merchant: string,
  accountIds: string[],
): string {
  const storedByMerchant = validMerchantAccounts(defaults.paymentAccountIdsByMerchant);
  const merchantAccountId = storedByMerchant[merchant.trim()];
  if (merchantAccountId && accountIds.includes(merchantAccountId)) return merchantAccountId;
  if (accountIds.includes(defaults.paymentAccountId)) return defaults.paymentAccountId;
  return accountIds[0] ?? "";
}

function validMerchantAccounts(value: unknown): Record<string, string> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  return Object.fromEntries(
    Object.entries(value).filter(
      ([merchant, accountId]) => merchant.trim() && typeof accountId === "string",
    ),
  );
}

function isPurchaseDefaults(value: unknown): value is StoredPurchaseDefaults {
  if (!value || typeof value !== "object") return false;
  const defaults = value as Partial<PurchaseDefaults>;
  return typeof defaults.merchant === "string" && typeof defaults.paymentAccountId === "string";
}
