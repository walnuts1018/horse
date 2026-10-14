export type PurchaseDefaults = {
  merchant: string;
  paymentAccountId: string;
};

export type PurchaseDefaultHistory = {
  merchant: string;
  paymentAccountId: string;
  purchaseCount: number;
  lastUsedAt: string;
};

export type PurchaseDefaultHistoryState = {
  householdId: string;
  history: PurchaseDefaultHistory[];
  loaded: boolean;
};

export function purchaseDefaultHistoryForHousehold(
  state: PurchaseDefaultHistoryState,
  householdId: string,
): PurchaseDefaultHistoryState {
  return state.householdId === householdId ? state : { householdId, history: [], loaded: false };
}

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

export function initialPurchaseDefaultsFromHistory(
  history: PurchaseDefaultHistory[],
  accountIds: string[],
): PurchaseDefaults {
  const latest = history.reduce<PurchaseDefaultHistory | undefined>((current, candidate) => {
    return !current || Date.parse(candidate.lastUsedAt) > Date.parse(current.lastUsedAt)
      ? candidate
      : current;
  }, undefined);
  if (!latest) return { merchant: "", paymentAccountId: uniqueAccountId(accountIds) };
  return {
    merchant: latest.merchant,
    paymentAccountId: paymentAccountForHistory(history, latest.merchant, accountIds),
  };
}

export function paymentAccountFromHistory(
  history: PurchaseDefaultHistory[],
  merchant: string,
  accountIds: string[],
): string | undefined {
  const exactMerchantHistory = history.filter((entry) => entry.merchant === merchant);
  if (exactMerchantHistory.length === 0) return undefined;
  return paymentAccountForHistory(history, merchant, accountIds);
}

export function merchantsFromHistory(history: PurchaseDefaultHistory[]): string[] {
  const latestByMerchant = new Map<string, string>();
  for (const entry of history) {
    if (!entry.merchant) continue;
    const lastUsedAt = latestByMerchant.get(entry.merchant);
    if (!lastUsedAt || Date.parse(entry.lastUsedAt) > Date.parse(lastUsedAt)) {
      latestByMerchant.set(entry.merchant, entry.lastUsedAt);
    }
  }
  const ordered: Array<[string, string]> = [];
  for (const entry of latestByMerchant.entries()) {
    const position = ordered.findIndex(
      (previous) => Date.parse(entry[1]) > Date.parse(previous[1]),
    );
    ordered.splice(position < 0 ? ordered.length : position, 0, entry);
  }
  return ordered.map(([merchant]) => merchant);
}

function paymentAccountForHistory(
  history: PurchaseDefaultHistory[],
  merchant: string,
  accountIds: string[],
): string {
  const entry = history.reduce<PurchaseDefaultHistory | undefined>((current, candidate) => {
    if (candidate.merchant !== merchant) return current;
    return !current ||
      candidate.purchaseCount > current.purchaseCount ||
      (candidate.purchaseCount === current.purchaseCount &&
        Date.parse(candidate.lastUsedAt) > Date.parse(current.lastUsedAt))
      ? candidate
      : current;
  }, undefined);
  if (!entry) return uniqueAccountId(accountIds);
  return accountIds.includes(entry.paymentAccountId) ? entry.paymentAccountId : "";
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
