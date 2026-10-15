import { describe, expect, it } from "vitest";

import {
  clearOfflineDraftScope,
  offlineDraftScopeStorageKey,
  readOfflineDraftScope,
  writeOfflineDraftScope,
} from "./offline-draft-scope";

function createStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  };
}

describe("offline draft scope", () => {
  it("replaces the previous member and household scope", () => {
    const storage = createStorage();
    expect(writeOfflineDraftScope("member-1", "household-1", storage)).toBe(true);
    expect(writeOfflineDraftScope("member-2", "household-2", storage)).toBe(true);
    expect(readOfflineDraftScope(storage)).toEqual({
      version: 1,
      memberId: "member-2",
      householdId: "household-2",
    });
    expect(writeOfflineDraftScope(" member-2", "household-2", storage)).toBe(false);
    expect(writeOfflineDraftScope(" member-1", "household-1", storage)).toBe(false);
  });

  it("hides drafts after marker removal and rejects malformed scope data", () => {
    const storage = createStorage();
    storage.setItem(offlineDraftScopeStorageKey, JSON.stringify({ version: 2 }));
    expect(readOfflineDraftScope(storage)).toBeUndefined();

    expect(writeOfflineDraftScope("", "household-1", storage)).toBe(false);
    writeOfflineDraftScope("member-1", "household-1", storage);
    clearOfflineDraftScope(storage);
    expect(readOfflineDraftScope(storage)).toBeUndefined();
  });
});
