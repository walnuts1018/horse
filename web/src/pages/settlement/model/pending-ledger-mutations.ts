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

function readIntent(key: string): unknown {
  const value = window.localStorage.getItem(key);
  if (!value) return null;
  try {
    return JSON.parse(value) as unknown;
  } catch {
    window.localStorage.removeItem(key);
    return null;
  }
}

function writeIntent(key: string, intent: FundingIntent | TransferIntent) {
  window.localStorage.setItem(key, JSON.stringify(intent));
}

function isFundingIntent(value: unknown): value is FundingIntent {
  if (
    value === null ||
    typeof value !== "object" ||
    !("idempotencyKey" in value) ||
    typeof value.idempotencyKey !== "string" ||
    !("input" in value)
  ) {
    return false;
  }
  const input = value.input;
  return (
    input !== null &&
    typeof input === "object" &&
    "householdId" in input &&
    typeof input.householdId === "string" &&
    "memberId" in input &&
    typeof input.memberId === "string" &&
    "poolId" in input &&
    typeof input.poolId === "string" &&
    "amountYen" in input &&
    typeof input.amountYen === "number" &&
    "occurredAt" in input &&
    typeof input.occurredAt === "string" &&
    "idempotencyKey" in input &&
    typeof input.idempotencyKey === "string" &&
    (!("sourceAccountId" in input) || typeof input.sourceAccountId === "string") &&
    (!("destinationAccountId" in input) || typeof input.destinationAccountId === "string")
  );
}

function isTransferIntent(value: unknown): value is TransferIntent {
  if (
    value === null ||
    typeof value !== "object" ||
    !("idempotencyKey" in value) ||
    typeof value.idempotencyKey !== "string" ||
    !("input" in value)
  ) {
    return false;
  }
  const input = value.input;
  return (
    input !== null &&
    typeof input === "object" &&
    "householdId" in input &&
    typeof input.householdId === "string" &&
    "fromMemberId" in input &&
    typeof input.fromMemberId === "string" &&
    "toMemberId" in input &&
    typeof input.toMemberId === "string" &&
    "amountYen" in input &&
    typeof input.amountYen === "number" &&
    "occurredAt" in input &&
    typeof input.occurredAt === "string" &&
    "description" in input &&
    typeof input.description === "string" &&
    "idempotencyKey" in input &&
    typeof input.idempotencyKey === "string" &&
    "paymentAccountId" in input &&
    (typeof input.paymentAccountId === "string" || input.paymentAccountId === null) &&
    "destinationAccountId" in input &&
    (typeof input.destinationAccountId === "string" || input.destinationAccountId === null) &&
    "kind" in input &&
    (input.kind === "REPAYMENT" ||
      input.kind === "ADVANCE" ||
      input.kind === "LOAN" ||
      input.kind === "GIFT") &&
    (input.kind !== "REPAYMENT" || ("loanId" in input && typeof input.loanId === "string"))
  );
}

export function getFundingIntent(householdId: string, memberId: string) {
  const intent = readIntent(storageKey(householdId, "funding", memberId));
  return isFundingIntent(intent) ? intent : null;
}

export function saveFundingIntent(householdId: string, memberId: string, intent: FundingIntent) {
  writeIntent(storageKey(householdId, "funding", memberId), intent);
}

export function clearFundingIntent(householdId: string, memberId: string) {
  window.localStorage.removeItem(storageKey(householdId, "funding", memberId));
}

export function getTransferIntent(householdId: string) {
  const intent = readIntent(storageKey(householdId, "transfer"));
  return isTransferIntent(intent) ? intent : null;
}

export function saveTransferIntent(householdId: string, intent: TransferIntent) {
  writeIntent(storageKey(householdId, "transfer"), intent);
}

export function clearTransferIntent(householdId: string) {
  window.localStorage.removeItem(storageKey(householdId, "transfer"));
}
