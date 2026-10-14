type AccountIncomeInput = {
  householdId: string;
  accountId: string;
  amountYen: number;
  occurredAt: string;
  description: string;
  idempotencyKey: string;
};

type AccountMovementInput = {
  householdId: string;
  kind: "TRANSFER" | "CARD_BILL_PAYMENT";
  fromAccountId: string;
  toAccountId: string;
  amountYen: number;
  occurredAt: string;
  description: string;
  idempotencyKey: string;
};

export type AccountIncomeIntent = {
  idempotencyKey: string;
  input: AccountIncomeInput;
};

export type AccountMovementIntent = {
  idempotencyKey: string;
  input: AccountMovementInput;
};

function storageKey(householdId: string, operation: string) {
  return `horse:settings:${operation}:${encodeURIComponent(householdId)}`;
}

function readIntent<T>(key: string, isIntent: (value: unknown) => value is T): T | null {
  const value = window.localStorage.getItem(key);
  if (!value) return null;
  try {
    const parsed: unknown = JSON.parse(value);
    if (isIntent(parsed)) return parsed;
    window.localStorage.removeItem(key);
    return null;
  } catch {
    window.localStorage.removeItem(key);
    return null;
  }
}

function writeIntent(key: string, intent: AccountIncomeIntent | AccountMovementIntent) {
  window.localStorage.setItem(key, JSON.stringify(intent));
}

export function getAccountIncomeIntent(householdId: string) {
  return readIntent(storageKey(householdId, "account-income"), isAccountIncomeIntent);
}

function isAccountIncomeIntent(value: unknown): value is AccountIncomeIntent {
  return (
    typeof value === "object" && value !== null && "idempotencyKey" in value && "input" in value
  );
}

export function saveAccountIncomeIntent(householdId: string, intent: AccountIncomeIntent) {
  writeIntent(storageKey(householdId, "account-income"), intent);
}

export function clearAccountIncomeIntent(householdId: string) {
  window.localStorage.removeItem(storageKey(householdId, "account-income"));
}

export function getAccountMovementIntent(householdId: string) {
  return readIntent(storageKey(householdId, "account-movement"), isAccountMovementIntent);
}

function isAccountMovementIntent(value: unknown): value is AccountMovementIntent {
  return (
    typeof value === "object" && value !== null && "idempotencyKey" in value && "input" in value
  );
}

export function saveAccountMovementIntent(householdId: string, intent: AccountMovementIntent) {
  writeIntent(storageKey(householdId, "account-movement"), intent);
}

export function clearAccountMovementIntent(householdId: string) {
  window.localStorage.removeItem(storageKey(householdId, "account-movement"));
}
