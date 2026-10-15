export const offlineDraftScopeStorageKey = "horse:offline-draft-scope:v1";

export type OfflineDraftScope = {
  version: 1;
  memberId: string;
  householdId: string;
};

type KeyValueStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

function isId(value: unknown): value is string {
  return (
    typeof value === "string" && value === value.trim() && value.length > 0 && value.length <= 200
  );
}

export function parseOfflineDraftScope(value: string | null): OfflineDraftScope | undefined {
  if (!value) return undefined;
  try {
    const parsed: unknown = JSON.parse(value);
    if (
      !parsed ||
      typeof parsed !== "object" ||
      !("version" in parsed) ||
      parsed.version !== 1 ||
      !("memberId" in parsed) ||
      !isId(parsed.memberId) ||
      !("householdId" in parsed) ||
      !isId(parsed.householdId)
    )
      return undefined;
    return { version: 1, memberId: parsed.memberId, householdId: parsed.householdId };
  } catch {
    return undefined;
  }
}

export function readOfflineDraftScope(storage: KeyValueStorage = window.localStorage) {
  try {
    return parseOfflineDraftScope(storage.getItem(offlineDraftScopeStorageKey));
  } catch {
    return undefined;
  }
}

export function writeOfflineDraftScope(
  memberId: string,
  householdId: string,
  storage: KeyValueStorage = window.localStorage,
) {
  if (!isId(memberId) || !isId(householdId)) return false;
  try {
    storage.setItem(
      offlineDraftScopeStorageKey,
      JSON.stringify({ version: 1, memberId, householdId } satisfies OfflineDraftScope),
    );
    return true;
  } catch {
    return false;
  }
}

export function clearOfflineDraftScope(storage: KeyValueStorage = window.localStorage) {
  try {
    storage.removeItem(offlineDraftScopeStorageKey);
  } catch {
    // Storageが利用できない場合はオフライン下書きを開かない。
  }
}
