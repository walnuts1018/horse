import "fake-indexeddb/auto";
import { beforeEach, describe, expect, it } from "vitest";

const {
  claimPurchaseOutbox,
  completePurchaseOutbox,
  enqueuePurchase,
  listPurchaseOutbox,
  markPurchaseOutboxFailed,
  retryPurchaseOutbox,
} = await import("./purchase-storage");

function deleteDatabase() {
  return new Promise<void>((resolve, reject) => {
    const request = indexedDB.deleteDatabase("horse-browser-data");
    request.onsuccess = () => resolve();
    request.onerror = () => reject(request.error);
    request.onblocked = () => reject(new Error("データベースの削除がブロックされました"));
  });
}

describe("purchase outbox storage", () => {
  beforeEach(deleteDatabase);

  it("並行claimを一方に限定し、failure後の再送でも同じキーを使って完了時に削除する", async () => {
    const idempotencyKey = "purchase-request-1";
    const input = {
      idempotencyKey,
      householdId: "household-1",
      occurredAt: "2026-10-14T00:00:00Z",
      merchant: "テスト店舗",
      items: [{ name: "テスト商品", amountYen: 100 }],
    };
    await enqueuePurchase(input);

    const [firstClaim, secondClaim] = await Promise.all([
      claimPurchaseOutbox(idempotencyKey, "worker-1"),
      claimPurchaseOutbox(idempotencyKey, "worker-2"),
    ]);
    const claims = [firstClaim, secondClaim].filter((claim) => claim !== undefined);
    expect(claims).toHaveLength(1);
    const claim = claims[0];
    if (!claim?.leaseOwner) throw new Error("claimしたownerが見つかりません");

    await markPurchaseOutboxFailed(idempotencyKey, claim.leaseOwner, "temporary failure");
    const failed = await listPurchaseOutbox();
    expect(failed).toHaveLength(1);
    expect(failed[0]).toMatchObject({
      idempotencyKey,
      input: { idempotencyKey },
      status: "FAILED",
      lastError: "temporary failure",
    });

    await retryPurchaseOutbox(idempotencyKey);
    const retried = await listPurchaseOutbox();
    expect(retried).toHaveLength(1);
    expect(retried[0]).toMatchObject({
      idempotencyKey,
      input: { idempotencyKey },
      status: "PENDING",
    });

    const retryClaim = await claimPurchaseOutbox(idempotencyKey, "worker-3");
    expect(retryClaim).toMatchObject({ idempotencyKey, input: { idempotencyKey } });
    await completePurchaseOutbox(idempotencyKey, "worker-3");
    expect(await listPurchaseOutbox()).toEqual([]);
  });
});
