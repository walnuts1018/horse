import { afterEach, describe, expect, it, vi } from "vitest";

import {
  clearSharedPurchaseDraftRecovery,
  decideSharedPurchaseDraftRecovery,
  readSharedPurchaseDraftRecovery,
  rebaseSharedPurchaseDraftRecovery,
  writeSharedPurchaseDraftRecovery,
} from "./shared-purchase-draft-recovery";

function createStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  };
}

describe("shared purchase draft recovery", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("同じrevisionの端末内編集を復元対象にする", () => {
    const recovery = {
      version: 1 as const,
      draftId: "draft-1",
      householdId: "household-1",
      baseRevision: 4,
      payload: "local",
      updatedAt: 10,
    };

    expect(decideSharedPurchaseDraftRecovery(recovery, 4, "server")).toBe("restore");
    expect(decideSharedPurchaseDraftRecovery(recovery, 5, "server")).toBe("conflict");
    expect(decideSharedPurchaseDraftRecovery(recovery, 5, "local")).toBe("clear");
  });

  it("mutation成功時に古いbackupを消さず、最新編集だけを新revisionへrebaseする", () => {
    const storage = createStorage();
    writeSharedPurchaseDraftRecovery(
      {
        draftId: "draft-1",
        householdId: "household-1",
        baseRevision: 4,
        payload: "newer-local",
      },
      storage,
    );

    expect(clearSharedPurchaseDraftRecovery("draft-1", "household-1", "sent", storage)).toBe(false);
    expect(rebaseSharedPurchaseDraftRecovery("draft-1", "household-1", "sent", 4, 5, storage)).toBe(
      false,
    );
    expect(
      rebaseSharedPurchaseDraftRecovery("draft-1", "household-1", "newer-local", 4, 5, storage),
    ).toBe(true);
    expect(readSharedPurchaseDraftRecovery("draft-1", "household-1", storage)).toMatchObject({
      baseRevision: 5,
      payload: "newer-local",
    });
  });

  it("別householdのbackupを読み込まない", () => {
    const storage = createStorage();
    writeSharedPurchaseDraftRecovery(
      {
        draftId: "draft-1",
        householdId: "household-1",
        baseRevision: 4,
        payload: "private purchase data",
      },
      storage,
    );

    expect(readSharedPurchaseDraftRecovery("draft-1", "household-2", storage)).toBeUndefined();
    expect(
      clearSharedPurchaseDraftRecovery("draft-1", "household-2", "private purchase data", storage),
    ).toBe(false);
  });

  it("localStorage getterが拒否されてもbackup処理を例外終了しない", () => {
    vi.stubGlobal("window", {
      get localStorage() {
        throw new Error("SecurityError");
      },
    });

    expect(readSharedPurchaseDraftRecovery("draft-1", "household-1")).toBeUndefined();
    expect(
      writeSharedPurchaseDraftRecovery({
        draftId: "draft-1",
        householdId: "household-1",
        baseRevision: 4,
        payload: "local",
      }),
    ).toBe(false);
    expect(clearSharedPurchaseDraftRecovery("draft-1", "household-1", "local")).toBe(false);
    expect(rebaseSharedPurchaseDraftRecovery("draft-1", "household-1", "local", 4, 5)).toBe(false);
  });
});
