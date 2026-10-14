export type SharedPurchaseDraftRecovery = {
  version: 1;
  draftId: string;
  householdId: string;
  baseRevision: number;
  payload: string;
  updatedAt: number;
};

export type SharedPurchaseDraftRecoveryDecision = "clear" | "restore" | "conflict";

const storageKey = (draftId: string) =>
  `horse:shared-purchase-draft-recovery:v1:${encodeURIComponent(draftId)}`;

function isSharedPurchaseDraftRecovery(value: unknown): value is SharedPurchaseDraftRecovery {
  return (
    typeof value === "object" &&
    value !== null &&
    "version" in value &&
    value.version === 1 &&
    "draftId" in value &&
    typeof value.draftId === "string" &&
    "householdId" in value &&
    typeof value.householdId === "string" &&
    "baseRevision" in value &&
    typeof value.baseRevision === "number" &&
    Number.isSafeInteger(value.baseRevision) &&
    value.baseRevision >= 0 &&
    "payload" in value &&
    typeof value.payload === "string" &&
    "updatedAt" in value &&
    typeof value.updatedAt === "number" &&
    Number.isSafeInteger(value.updatedAt) &&
    value.updatedAt >= 0
  );
}

export function readSharedPurchaseDraftRecovery(
  draftId: string,
  householdId: string,
  storage?: Pick<Storage, "getItem">,
): SharedPurchaseDraftRecovery | undefined {
  try {
    const raw = (storage ?? window.localStorage).getItem(storageKey(draftId));
    if (!raw) return undefined;
    const value: unknown = JSON.parse(raw);
    if (!isSharedPurchaseDraftRecovery(value)) return undefined;
    if (value.draftId !== draftId || value.householdId !== householdId) return undefined;
    return value;
  } catch {
    return undefined;
  }
}

export function writeSharedPurchaseDraftRecovery(
  recovery: Omit<SharedPurchaseDraftRecovery, "version" | "updatedAt">,
  storage?: Pick<Storage, "setItem">,
) {
  try {
    (storage ?? window.localStorage).setItem(
      storageKey(recovery.draftId),
      JSON.stringify({ ...recovery, version: 1, updatedAt: Date.now() }),
    );
    return true;
  } catch {
    return false;
  }
}

export function clearSharedPurchaseDraftRecovery(
  draftId: string,
  householdId: string,
  expectedPayload: string,
  storage?: Pick<Storage, "getItem" | "removeItem">,
) {
  try {
    const target = storage ?? window.localStorage;
    const current = readSharedPurchaseDraftRecovery(draftId, householdId, target);
    if (current?.payload !== expectedPayload) return false;
    target.removeItem(storageKey(draftId));
    return true;
  } catch {
    return false;
  }
}

export function rebaseSharedPurchaseDraftRecovery(
  draftId: string,
  householdId: string,
  expectedPayload: string,
  expectedRevision: number,
  nextRevision: number,
  storage?: Pick<Storage, "getItem" | "setItem">,
) {
  try {
    const target = storage ?? window.localStorage;
    const current = readSharedPurchaseDraftRecovery(draftId, householdId, target);
    if (current?.payload !== expectedPayload || current.baseRevision !== expectedRevision)
      return false;
    return writeSharedPurchaseDraftRecovery(
      {
        draftId,
        householdId,
        baseRevision: nextRevision,
        payload: expectedPayload,
      },
      target,
    );
  } catch {
    return false;
  }
}

export function decideSharedPurchaseDraftRecovery(
  recovery: SharedPurchaseDraftRecovery,
  serverRevision: number,
  serverPayload: string,
): SharedPurchaseDraftRecoveryDecision {
  if (recovery.payload === serverPayload) return "clear";
  if (recovery.baseRevision === serverRevision) return "restore";
  return "conflict";
}
