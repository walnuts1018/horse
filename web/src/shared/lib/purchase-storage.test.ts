import "fake-indexeddb/auto";
import { beforeEach, describe, expect, it } from "vitest";

const {
  claimPurchaseOutbox,
  completePurchaseOutbox,
  discardPurchaseOutbox,
  enqueuePurchase,
  deleteImportReceiptDraft,
  listPurchaseOutbox,
  loadImportReceiptDraft,
  loadPurchaseDraft,
  markPurchaseOutboxFailed,
  retryPurchaseOutbox,
  savePurchaseDraft,
  saveImportReceiptDraft,
} = await import("./purchase-storage");

const databaseName = "horse-browser-data";

function deleteDatabase() {
  return new Promise<void>((resolve, reject) => {
    const request = indexedDB.deleteDatabase(databaseName);
    request.onsuccess = () => resolve();
    request.onerror = () => reject(request.error);
    request.onblocked = () => reject(new Error("データベースの削除がブロックされました"));
  });
}

function seedUnscopedVersionTwoRecords() {
  return new Promise<void>((resolve, reject) => {
    const request = indexedDB.open(databaseName, 2);
    request.onupgradeneeded = () => {
      const database = request.result;
      database.createObjectStore("purchase-drafts-v2", { keyPath: "householdId" });
      database.createObjectStore("purchase-outbox-v2", { keyPath: "idempotencyKey" });
    };
    request.onsuccess = () => {
      const database = request.result;
      const transaction = database.transaction(
        ["purchase-drafts-v2", "purchase-outbox-v2"],
        "readwrite",
      );
      transaction.objectStore("purchase-drafts-v2").put({
        householdId: "household-1",
        merchant: "所有者不明の下書き",
      });
      transaction.objectStore("purchase-outbox-v2").put({
        idempotencyKey: "legacy-request",
        input: {
          idempotencyKey: "legacy-request",
          householdId: "household-1",
          occurredAt: "2026-10-14T00:00:00Z",
          merchant: "所有者不明の送信待ち",
          items: [{ name: "商品", amountYen: 100 }],
        },
        status: "PENDING",
      });
      transaction.oncomplete = () => {
        database.close();
        resolve();
      };
      transaction.onerror = () => {
        database.close();
        reject(transaction.error);
      };
    };
    request.onerror = () => reject(request.error);
  });
}

function readUnscopedRecords() {
  return new Promise<{ draft: unknown; outbox: unknown }>((resolve, reject) => {
    const request = indexedDB.open(databaseName, 4);
    request.onsuccess = () => {
      const database = request.result;
      const transaction = database.transaction(
        ["purchase-drafts-v2", "purchase-outbox-v2"],
        "readonly",
      );
      const draft = transaction.objectStore("purchase-drafts-v2").get("household-1");
      const outbox = transaction.objectStore("purchase-outbox-v2").get("legacy-request");
      transaction.oncomplete = () => {
        database.close();
        resolve({ draft: draft.result, outbox: outbox.result });
      };
      transaction.onerror = () => {
        database.close();
        reject(transaction.error);
      };
    };
    request.onerror = () => reject(request.error);
  });
}

describe("purchase outbox storage", () => {
  beforeEach(deleteDatabase);

  it("claimと再送で同じ冪等キーを維持し、所有者のclaimだけを完了できる", async () => {
    const memberId = "member-1";
    const idempotencyKey = "purchase-request-1";
    await enqueuePurchase(
      {
        idempotencyKey,
        householdId: "household-1",
        occurredAt: "2026-10-14T00:00:00Z",
        merchant: "テスト店舗",
        items: [{ name: "テスト商品", amountYen: 100 }],
      },
      memberId,
    );

    const [firstClaim, secondClaim] = await Promise.all([
      claimPurchaseOutbox(memberId, "household-1", idempotencyKey, "worker-1"),
      claimPurchaseOutbox(memberId, "household-1", idempotencyKey, "worker-2"),
    ]);
    const claims = [firstClaim, secondClaim].filter((claim) => claim !== undefined);
    expect(claims).toHaveLength(1);
    const claim = claims[0];
    if (!claim.leaseOwner) throw new Error("claimしたownerが見つかりません");

    await markPurchaseOutboxFailed(
      memberId,
      "household-1",
      idempotencyKey,
      claim.leaseOwner,
      "temporary failure",
    );
    expect(await listPurchaseOutbox(memberId)).toMatchObject([
      { idempotencyKey, memberId, status: "FAILED", lastError: "temporary failure" },
    ]);

    await retryPurchaseOutbox(memberId, "household-1", idempotencyKey);
    const retryClaim = await claimPurchaseOutbox(
      memberId,
      "household-1",
      idempotencyKey,
      "worker-3",
    );
    expect(retryClaim).toMatchObject({ idempotencyKey, memberId });
    await completePurchaseOutbox(memberId, "household-1", idempotencyKey, "worker-3");
    expect(await listPurchaseOutbox(memberId)).toEqual([]);
  });

  it("同じ家庭の下書きとOutboxをmemberIdで分離する", async () => {
    const householdId = "household-shared";
    const idempotencyKey = "same-idempotency-key";
    await savePurchaseDraft("member-a", householdId, {
      merchant: "Aの下書き",
      occurredAt: "2026-10-14T00:00:00Z",
      itemName: "",
      itemAmount: "",
      categoryId: "",
      items: [],
    });
    await savePurchaseDraft("member-b", householdId, {
      merchant: "Bの下書き",
      occurredAt: "2026-10-14T00:00:00Z",
      itemName: "",
      itemAmount: "",
      categoryId: "",
      items: [],
    });
    expect(await loadPurchaseDraft("member-a", householdId)).toMatchObject({
      merchant: "Aの下書き",
    });
    expect(await loadPurchaseDraft("member-b", householdId)).toMatchObject({
      merchant: "Bの下書き",
    });

    const input = {
      idempotencyKey,
      householdId,
      occurredAt: "2026-10-14T00:00:00Z",
      merchant: "テスト店舗",
      items: [{ name: "テスト商品", amountYen: 100 }],
    };
    await enqueuePurchase(input, "member-a");
    await enqueuePurchase(input, "member-b");
    await enqueuePurchase({ ...input, householdId: "household-other" }, "member-a");
    expect(await listPurchaseOutbox("member-a", householdId)).toMatchObject([
      { memberId: "member-a", householdId },
    ]);
    expect(await listPurchaseOutbox("member-b", householdId)).toMatchObject([
      { memberId: "member-b", householdId },
    ]);
    expect(await listPurchaseOutbox("member-a", "household-other")).toMatchObject([
      { memberId: "member-a", householdId: "household-other" },
    ]);

    expect(
      await claimPurchaseOutbox("member-a", householdId, idempotencyKey, "owner-a"),
    ).toMatchObject({
      memberId: "member-a",
    });
    expect(
      await claimPurchaseOutbox("member-b", householdId, idempotencyKey, "owner-b"),
    ).toMatchObject({
      memberId: "member-b",
    });
    await completePurchaseOutbox("member-b", householdId, idempotencyKey, "owner-a");
    expect(await listPurchaseOutbox("member-b", householdId)).toHaveLength(1);
    await completePurchaseOutbox("member-a", householdId, idempotencyKey, "owner-a");
    expect(await listPurchaseOutbox("member-a", householdId)).toEqual([]);
    await discardPurchaseOutbox("member-b", householdId, idempotencyKey);
    expect(await listPurchaseOutbox("member-b", householdId)).toEqual([]);
  });

  it("所有者不明の旧データを送信対象から外し、IndexedDBから削除しない", async () => {
    await seedUnscopedVersionTwoRecords();

    expect(await loadPurchaseDraft("member-a", "household-1")).toBeUndefined();
    expect(await listPurchaseOutbox("member-a")).toEqual([]);
    expect(await listPurchaseOutbox("member-b")).toEqual([]);
    expect(
      await claimPurchaseOutbox("member-a", "household-1", "legacy-request", "worker-a"),
    ).toBeUndefined();

    const records = await readUnscopedRecords();
    expect(records.draft).toMatchObject({ merchant: "所有者不明の下書き" });
    expect(records.outbox).toMatchObject({ input: { merchant: "所有者不明の送信待ち" } });
  });
});

describe("import receipt draft storage", () => {
  beforeEach(deleteDatabase);

  it("家庭と原本ごとに編集差分を保存し、削除できる", async () => {
    const draft = {
      householdId: "household-1",
      documentId: "document-1",
      baseResult: { receipt: { merchant: "AI店舗" } },
      editedResult: { receipt: { merchant: "修正店舗" } },
      confirmationState: {
        selectedCandidate: {
          id: "candidate-1",
          amounts: [110],
          computedTotal: 110,
          consistent: true,
          issues: [],
        },
        manualAmountOverrides: { "item-0": 111 },
        manualAmountOverrideReason: "印字額を確認",
        receiptItemReviewStates: {
          "source:milk": "CONFIRMED" as const,
          "manual:new": "UNRESOLVED" as const,
        },
        payments: [{ key: "manual", paymentAccountId: "account-1", amountYen: 111 }],
        itemAllocations: { "item-0": [{ poolId: "pool-1", weight: 100 }] },
      },
    };
    await saveImportReceiptDraft(draft);

    expect(await loadImportReceiptDraft("household-1", "document-1")).toEqual(draft);
    expect(await loadImportReceiptDraft("household-2", "document-1")).toBeUndefined();
    expect(await loadImportReceiptDraft("household-1", "document-2")).toBeUndefined();

    await deleteImportReceiptDraft("household-1", "document-1");
    expect(await loadImportReceiptDraft("household-1", "document-1")).toBeUndefined();
  });
});
