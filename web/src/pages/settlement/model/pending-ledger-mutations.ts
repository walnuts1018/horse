export type FundingMutationInput = {
  householdId: string;
  memberId: string;
  poolId: string;
  amountYen: number;
  occurredAt: string;
  idempotencyKey: string;
  sourceAccountId?: string;
  destinationAccountId?: string;
};

type TransferMutationInput = {
  householdId: string;
  fromMemberId: string;
  toMemberId: string;
  paymentAccountId: string | null;
  destinationAccountId: string | null;
  amountYen: number;
  occurredAt: string;
  description: string;
  idempotencyKey: string;
};

export type FundingIntent = {
  idempotencyKey: string;
  input: FundingMutationInput;
};

export type TransferIntentInput = TransferMutationInput &
  ({ kind: "REPAYMENT"; loanId: string } | { kind: "ADVANCE" | "LOAN" | "GIFT" });

export type TransferIntent = {
  idempotencyKey: string;
  input: TransferIntentInput;
};

function storageKey(householdId: string, operation: string, subjectId = "") {
  return `horse:settlement:${operation}:${encodeURIComponent(householdId)}:${encodeURIComponent(subjectId)}`;
}

function readIntent<T>(key: string): T | null {
  const value = window.localStorage.getItem(key);
  if (!value) return null;
  try {
    return JSON.parse(value) as T;
  } catch {
    window.localStorage.removeItem(key);
    return null;
  }
}

function writeIntent<T>(key: string, intent: T) {
  window.localStorage.setItem(key, JSON.stringify(intent));
}

export function getFundingIntent(householdId: string, memberId: string) {
  return readIntent<FundingIntent>(storageKey(householdId, "funding", memberId));
}

export function saveFundingIntent(householdId: string, memberId: string, intent: FundingIntent) {
  writeIntent(storageKey(householdId, "funding", memberId), intent);
}

export function clearFundingIntent(householdId: string, memberId: string) {
  window.localStorage.removeItem(storageKey(householdId, "funding", memberId));
}

export function getTransferIntent(householdId: string) {
  return readIntent<TransferIntent>(storageKey(householdId, "transfer"));
}

export function saveTransferIntent(householdId: string, intent: TransferIntent) {
  writeIntent(storageKey(householdId, "transfer"), intent);
}

export function clearTransferIntent(householdId: string) {
  window.localStorage.removeItem(storageKey(householdId, "transfer"));
}
