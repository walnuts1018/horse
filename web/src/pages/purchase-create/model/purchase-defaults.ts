export type PurchaseDefaults = {
  merchant: string;
  paymentAccountId: string;
};

type PaymentAccountHistory = Array<{
  paymentAllocations: Array<{ paymentAccountId: string }>;
}>;

export function orderPaymentAccountsByUsage<T extends { id: string }>(
  accounts: T[],
  history: PaymentAccountHistory,
): T[] {
  const usageCount = new Map<string, number>();
  for (const purchase of history) {
    for (const accountId of new Set(
      purchase.paymentAllocations.map((payment) => payment.paymentAccountId),
    )) {
      usageCount.set(accountId, (usageCount.get(accountId) ?? 0) + 1);
    }
  }
  const ordered: T[] = [];
  for (const account of accounts) {
    const usage = usageCount.get(account.id) ?? 0;
    const position = ordered.findIndex((previous) => {
      return usage > (usageCount.get(previous.id) ?? 0);
    });
    ordered.splice(position < 0 ? ordered.length : position, 0, account);
  }
  return ordered;
}

type StoredPurchaseDefaults = PurchaseDefaults & {
  paymentAccountIdsByMerchant?: Record<string, string>;
};

type StorageLike = Pick<Storage, "getItem" | "setItem">;

export function loadPurchaseDefaults(
  memberId: string,
  householdId: string,
  accountIds: string[],
): PurchaseDefaults {
  const fallback = { merchant: "", paymentAccountId: uniqueAccountId(accountIds) };
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
  const fallback = uniqueAccountId(accountIds);
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
  const merchantName = merchant.trim();
  const merchantAccountId = storedByMerchant[merchantName];
  if (merchantAccountId) {
    return accountIds.includes(merchantAccountId) ? merchantAccountId : "";
  }
  if (accountIds.includes(defaults.paymentAccountId)) return defaults.paymentAccountId;
  if (defaults.paymentAccountId) return "";
  return uniqueAccountId(accountIds);
}

function uniqueAccountId(accountIds: string[]): string {
  return accountIds.length === 1 ? accountIds[0] : "";
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
