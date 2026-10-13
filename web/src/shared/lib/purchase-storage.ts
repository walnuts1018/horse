import { useCallback, useEffect, useRef, useState } from "react";

import type { CreatePurchaseInput } from "../api";

export type PurchaseDraftItem = {
  key: string;
  name: string;
  amount: string;
  quantity?: string;
  quantityUnit?: string;
  categoryId: string;
  productVariantId?: string;
  allocations: Array<{ poolId: string; weight: number }>;
};

export type PurchaseDraft = {
  merchant: string;
  occurredAt: string;
  paymentAccountId?: string;
  paymentAllocations?: Array<{ paymentAccountId: string; amountYen: string }>;
  paymentAutoTotal?: boolean;
  itemName: string;
  itemAmount: string;
  categoryId: string;
  items: PurchaseDraftItem[];
};

export type PurchaseOutboxEntry = {
  storageKey: string;
  memberId: string;
  householdId: string;
  idempotencyKey: string;
  input: CreatePurchaseInput;
  status: "PENDING" | "FAILED";
  createdAt: number;
  updatedAt: number;
  lastError?: string;
  leaseOwner?: string;
  leaseUntil?: number;
};

const databaseName = "horse-browser-data";
const legacyDraftsStore = "purchase-drafts";
const legacyOutboxStore = "purchase-outbox";
const unscopedDraftsStore = "purchase-drafts-v2";
const unscopedOutboxStore = "purchase-outbox-v2";
const draftsStore = "purchase-drafts-v3";
const outboxStore = "purchase-outbox-v3";
const outboxChangeEvent = "horse:purchase-outbox-change";
const databaseReloadEvent = "horse:database-reload-required";
const leaseMilliseconds = 30_000;
const databaseVersion = 3;

function draftStorageKey(memberId: string, householdId: string) {
  if (!memberId) throw new Error("ログイン中のメンバーを確認できません");
  return JSON.stringify([memberId, householdId]);
}

function outboxStorageKey(memberId: string, householdId: string, idempotencyKey: string) {
  if (!memberId) throw new Error("ログイン中のメンバーを確認できません");
  return JSON.stringify([memberId, householdId, idempotencyKey]);
}

function requestDatabaseReload(message: string) {
  if (typeof window !== "undefined")
    window.dispatchEvent(new CustomEvent(databaseReloadEvent, { detail: message }));
}

function migrateStore<T>(
  transaction: IDBTransaction,
  sourceName: string,
  targetName: string,
  normalize: (value: unknown, key: IDBValidKey) => T | undefined,
) {
  const database = transaction.db;
  if (!database.objectStoreNames.contains(sourceName)) return;
  const source = transaction.objectStore(sourceName);
  const target = transaction.objectStore(targetName);
  const request = source.openCursor();
  request.onsuccess = () => {
    const cursor = request.result;
    if (!cursor) return;
    const value = normalize(cursor.value, cursor.primaryKey);
    if (value) target.put(value);
    cursor.continue();
  };
}

function migrateDraft(value: unknown, key: IDBValidKey) {
  if (!value || typeof value !== "object") return undefined;
  const draft = value as Record<string, unknown>;
  const householdId = [draft.householdId, draft.id, key].find(
    (candidate): candidate is string => typeof candidate === "string" && candidate.length > 0,
  );
  if (!householdId) return undefined;
  return { ...draft, householdId };
}

function migrateOutbox(value: unknown, key: IDBValidKey): Record<string, unknown> | undefined {
  if (!value || typeof value !== "object") return undefined;
  const entry = value as Record<string, unknown>;
  const input = (entry.input ?? entry.request ?? entry) as CreatePurchaseInput | undefined;
  const idempotencyKey = [
    entry.idempotencyKey,
    entry.key,
    input?.idempotencyKey,
    entry.id,
    key,
  ].find((candidate): candidate is string => typeof candidate === "string" && candidate.length > 0);
  if (!idempotencyKey || !input || !Array.isArray(input.items)) return undefined;
  return {
    ...entry,
    idempotencyKey,
    input: { ...input, idempotencyKey },
    status: entry.status === "FAILED" ? "FAILED" : "PENDING",
    createdAt: typeof entry.createdAt === "number" ? entry.createdAt : Date.now(),
    updatedAt: typeof entry.updatedAt === "number" ? entry.updatedAt : Date.now(),
  };
}

function normalizePurchaseInput(input: CreatePurchaseInput): CreatePurchaseInput {
  const legacy = input as CreatePurchaseInput & { paymentAccountId?: string | null };
  const { paymentAccountId, ...canonical } = legacy;
  return {
    ...canonical,
    origin: input.origin ?? "MANUAL",
    paymentAllocations:
      input.paymentAllocations?.length || !paymentAccountId
        ? (input.paymentAllocations ?? [])
        : [
            {
              paymentAccountId,
              amountYen: input.items.reduce((total, item) => total + item.amountYen, 0),
            },
          ],
  };
}

function openDatabase() {
  if (typeof indexedDB === "undefined")
    return Promise.reject(new Error("このブラウザでは下書きを保存できません"));
  return new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open(databaseName, databaseVersion);
    let settled = false;
    request.onupgradeneeded = (event) => {
      const database = request.result;
      const transaction = request.transaction;
      if (!transaction) return;
      if (event.oldVersion < 2) {
        if (!database.objectStoreNames.contains(unscopedDraftsStore))
          database.createObjectStore(unscopedDraftsStore, { keyPath: "householdId" });
        if (!database.objectStoreNames.contains(unscopedOutboxStore))
          database.createObjectStore(unscopedOutboxStore, { keyPath: "idempotencyKey" });
        migrateStore(transaction, legacyDraftsStore, unscopedDraftsStore, migrateDraft);
        migrateStore(transaction, legacyOutboxStore, unscopedOutboxStore, migrateOutbox);
      }
      if (event.oldVersion < 3) {
        if (!database.objectStoreNames.contains(draftsStore))
          database.createObjectStore(draftsStore, { keyPath: "storageKey" });
        if (!database.objectStoreNames.contains(outboxStore)) {
          const store = database.createObjectStore(outboxStore, { keyPath: "storageKey" });
          store.createIndex("memberId", "memberId");
          store.createIndex("memberHousehold", ["memberId", "householdId"]);
        }
      }
    };
    request.onsuccess = () => {
      const database = request.result;
      if (settled) {
        database.close();
        return;
      }
      database.onversionchange = () => {
        database.close();
        requestDatabaseReload("保存データが更新されました。再読み込みしてください");
      };
      settled = true;
      resolve(database);
    };
    request.onerror = () => {
      if (settled) return;
      settled = true;
      reject(request.error ?? new Error("下書きデータベースを開けません"));
    };
    request.onblocked = () => {
      if (settled) return;
      settled = true;
      const message = "別のhorseタブを再読み込みしてください";
      requestDatabaseReload(message);
      reject(new Error(message));
    };
  });
}

function notifyOutboxChanged() {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new Event(outboxChangeEvent));
  if (typeof BroadcastChannel !== "undefined") {
    outboxBroadcast ??= new BroadcastChannel(outboxChangeEvent);
    outboxBroadcast.postMessage("changed");
  }
}

let outboxBroadcast: BroadcastChannel | undefined;

function writeValue<T>(storeName: string, value: T) {
  return openDatabase().then(
    (database) =>
      new Promise<void>((resolve, reject) => {
        const transaction = database.transaction(storeName, "readwrite");
        transaction.objectStore(storeName).put(value);
        transaction.oncomplete = () => {
          database.close();
          resolve();
        };
        transaction.onerror = () => {
          database.close();
          reject(transaction.error ?? new Error("ローカル保存に失敗しました"));
        };
        transaction.onabort = () => {
          database.close();
          reject(transaction.error ?? new Error("ローカル保存に失敗しました"));
        };
      }),
  );
}

function deleteValue(storeName: string, key: IDBValidKey) {
  return openDatabase().then(
    (database) =>
      new Promise<void>((resolve, reject) => {
        const transaction = database.transaction(storeName, "readwrite");
        transaction.objectStore(storeName).delete(key);
        transaction.oncomplete = () => {
          database.close();
          resolve();
        };
        transaction.onerror = () => {
          database.close();
          reject(transaction.error ?? new Error("ローカルデータを削除できません"));
        };
        transaction.onabort = () => {
          database.close();
          reject(transaction.error ?? new Error("ローカルデータを削除できません"));
        };
      }),
  );
}

function readValue<T>(storeName: string, key: IDBValidKey) {
  return openDatabase().then(
    (database) =>
      new Promise<T | undefined>((resolve, reject) => {
        const transaction = database.transaction(storeName, "readonly");
        const request = transaction.objectStore(storeName).get(key);
        let value: T | undefined;
        request.onsuccess = () => {
          value = request.result as T | undefined;
        };
        transaction.oncomplete = () => {
          database.close();
          resolve(value);
        };
        transaction.onerror = () => {
          database.close();
          reject(transaction.error ?? new Error("ローカルデータを読み込めません"));
        };
      }),
  );
}

function readAllByIndex<T>(storeName: string, indexName: string, key: IDBValidKey) {
  return openDatabase().then(
    (database) =>
      new Promise<T[]>((resolve, reject) => {
        const transaction = database.transaction(storeName, "readonly");
        const request = transaction.objectStore(storeName).index(indexName).getAll(key);
        let values: T[] = [];
        request.onsuccess = () => {
          values = request.result as T[];
        };
        transaction.oncomplete = () => {
          database.close();
          resolve(values);
        };
        transaction.onerror = () => {
          database.close();
          reject(transaction.error ?? new Error("ローカルデータを読み込めません"));
        };
      }),
  );
}

export async function loadPurchaseDraft(
  memberId: string,
  householdId: string,
): Promise<PurchaseDraft | undefined> {
  const storageKey = draftStorageKey(memberId, householdId);
  const current = await readValue<PurchaseDraft & { storageKey: string; cleared?: boolean }>(
    draftsStore,
    storageKey,
  );
  if (current?.cleared) {
    return undefined;
  }
  if (current) return current;
  return undefined;
}

export function savePurchaseDraft(memberId: string, householdId: string, draft: PurchaseDraft) {
  return writeValue(draftsStore, { ...draft, storageKey: draftStorageKey(memberId, householdId) });
}

export function deletePurchaseDraft(memberId: string, householdId: string) {
  return writeValue(draftsStore, {
    storageKey: draftStorageKey(memberId, householdId),
    cleared: true,
  });
}

export async function listPurchaseOutbox(memberId: string, householdId?: string) {
  return readAllByIndex<PurchaseOutboxEntry>(
    outboxStore,
    householdId ? "memberHousehold" : "memberId",
    householdId ? [memberId, householdId] : memberId,
  );
}

export async function enqueuePurchase(input: CreatePurchaseInput, memberId: string) {
  input = normalizePurchaseInput(input);
  const now = Date.now();
  const storageKey = outboxStorageKey(memberId, input.householdId, input.idempotencyKey);
  const database = await openDatabase();
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction([draftsStore, outboxStore], "readwrite");
      transaction.objectStore(outboxStore).put({
        storageKey,
        memberId,
        householdId: input.householdId,
        idempotencyKey: input.idempotencyKey,
        input,
        status: "PENDING",
        createdAt: now,
        updatedAt: now,
      } satisfies PurchaseOutboxEntry);
      transaction.objectStore(draftsStore).put({
        storageKey: draftStorageKey(memberId, input.householdId),
        cleared: true,
      });
      transaction.oncomplete = () => resolve();
      transaction.onerror = () =>
        reject(transaction.error ?? new Error("送信待ちを保存できません"));
      transaction.onabort = () =>
        reject(transaction.error ?? new Error("送信待ちを保存できません"));
    });
  } finally {
    database.close();
  }
  notifyOutboxChanged();
}

export async function claimPurchaseOutbox(
  memberId: string,
  householdId: string,
  idempotencyKey: string,
  owner: string,
): Promise<PurchaseOutboxEntry | undefined> {
  const database = await openDatabase();
  return new Promise((resolve, reject) => {
    const transaction = database.transaction(outboxStore, "readwrite");
    const store = transaction.objectStore(outboxStore);
    const request = store.get(outboxStorageKey(memberId, householdId, idempotencyKey));
    let claimed: PurchaseOutboxEntry | undefined;
    request.onsuccess = () => {
      const entry = request.result as PurchaseOutboxEntry | undefined;
      if (
        !entry ||
        entry.memberId !== memberId ||
        entry.householdId !== householdId ||
        entry.status !== "PENDING" ||
        (entry.leaseUntil ?? 0) > Date.now()
      )
        return;
      claimed = {
        ...entry,
        input: normalizePurchaseInput(entry.input),
        leaseOwner: owner,
        leaseUntil: Date.now() + leaseMilliseconds,
        updatedAt: Date.now(),
      };
      store.put(claimed);
    };
    transaction.oncomplete = () => {
      database.close();
      if (claimed) notifyOutboxChanged();
      resolve(claimed);
    };
    transaction.onerror = () => {
      database.close();
      reject(transaction.error ?? new Error("送信待ちを確認できません"));
    };
    transaction.onabort = () => {
      database.close();
      reject(transaction.error ?? new Error("送信待ちを確認できません"));
    };
  });
}

export async function markPurchaseOutboxFailed(
  memberId: string,
  householdId: string,
  idempotencyKey: string,
  owner: string,
  message: string,
) {
  const database = await openDatabase();
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(outboxStore, "readwrite");
      const store = transaction.objectStore(outboxStore);
      const request = store.get(outboxStorageKey(memberId, householdId, idempotencyKey));
      request.onsuccess = () => {
        const entry = request.result as PurchaseOutboxEntry | undefined;
        if (
          !entry ||
          entry.memberId !== memberId ||
          entry.householdId !== householdId ||
          entry.leaseOwner !== owner
        )
          return;
        store.put({
          ...entry,
          status: "FAILED",
          lastError: message,
          leaseOwner: undefined,
          leaseUntil: undefined,
          updatedAt: Date.now(),
        });
      };
      transaction.oncomplete = () => resolve();
      transaction.onerror = () =>
        reject(transaction.error ?? new Error("送信結果を保存できません"));
      transaction.onabort = () =>
        reject(transaction.error ?? new Error("送信結果を保存できません"));
    });
  } finally {
    database.close();
  }
  notifyOutboxChanged();
}

export async function completePurchaseOutbox(
  memberId: string,
  householdId: string,
  idempotencyKey: string,
  owner: string,
) {
  const database = await openDatabase();
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(outboxStore, "readwrite");
      const store = transaction.objectStore(outboxStore);
      const request = store.get(outboxStorageKey(memberId, householdId, idempotencyKey));
      request.onsuccess = () => {
        const entry = request.result as PurchaseOutboxEntry | undefined;
        if (
          entry?.memberId === memberId &&
          entry.householdId === householdId &&
          entry.leaseOwner === owner
        )
          store.delete(outboxStorageKey(memberId, householdId, idempotencyKey));
      };
      transaction.oncomplete = () => resolve();
      transaction.onerror = () =>
        reject(transaction.error ?? new Error("送信済み記録を削除できません"));
      transaction.onabort = () =>
        reject(transaction.error ?? new Error("送信済み記録を削除できません"));
    });
  } finally {
    database.close();
  }
  notifyOutboxChanged();
}

export async function renewPurchaseOutboxLease(
  memberId: string,
  householdId: string,
  idempotencyKey: string,
  owner: string,
) {
  const database = await openDatabase();
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(outboxStore, "readwrite");
      const store = transaction.objectStore(outboxStore);
      const request = store.get(outboxStorageKey(memberId, householdId, idempotencyKey));
      request.onsuccess = () => {
        const entry = request.result as PurchaseOutboxEntry | undefined;
        if (
          !entry ||
          entry.memberId !== memberId ||
          entry.householdId !== householdId ||
          entry.status !== "PENDING" ||
          entry.leaseOwner !== owner
        )
          return;
        store.put({ ...entry, leaseUntil: Date.now() + leaseMilliseconds, updatedAt: Date.now() });
      };
      transaction.oncomplete = () => resolve();
      transaction.onerror = () =>
        reject(transaction.error ?? new Error("送信状態を更新できません"));
      transaction.onabort = () =>
        reject(transaction.error ?? new Error("送信状態を更新できません"));
    });
  } finally {
    database.close();
  }
  notifyOutboxChanged();
}

export async function retryPurchaseOutbox(
  memberId: string,
  householdId: string,
  idempotencyKey: string,
) {
  const database = await openDatabase();
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(outboxStore, "readwrite");
      const store = transaction.objectStore(outboxStore);
      const request = store.get(outboxStorageKey(memberId, householdId, idempotencyKey));
      request.onsuccess = () => {
        const entry = request.result as PurchaseOutboxEntry | undefined;
        if (!entry || entry.memberId !== memberId || entry.householdId !== householdId) return;
        store.put({
          ...entry,
          status: "PENDING",
          lastError: undefined,
          leaseOwner: undefined,
          leaseUntil: undefined,
          updatedAt: Date.now(),
        });
      };
      transaction.oncomplete = () => resolve();
      transaction.onerror = () =>
        reject(transaction.error ?? new Error("再送の準備に失敗しました"));
      transaction.onabort = () =>
        reject(transaction.error ?? new Error("再送の準備に失敗しました"));
    });
  } finally {
    database.close();
  }
  notifyOutboxChanged();
}

export async function discardPurchaseOutbox(
  memberId: string,
  householdId: string,
  idempotencyKey: string,
) {
  await deleteValue(outboxStore, outboxStorageKey(memberId, householdId, idempotencyKey));
  notifyOutboxChanged();
}

export function usePurchaseOutbox(memberId?: string, householdId?: string) {
  const [entries, setEntries] = useState<PurchaseOutboxEntry[]>([]);
  const [entriesMemberId, setEntriesMemberId] = useState<string>();
  const [online, setOnline] = useState<boolean | null>(null);
  const [readyMemberId, setReadyMemberId] = useState<string>();
  const [storageError, setStorageError] = useState("");
  const [reloadMessage, setReloadMessage] = useState("");
  const memberIdRef = useRef(memberId);
  memberIdRef.current = memberId;
  const refresh = useCallback(async () => {
    if (!memberId) {
      setEntries([]);
      setEntriesMemberId(undefined);
      setStorageError("");
      setReadyMemberId(undefined);
      return;
    }
    try {
      const currentEntries = await listPurchaseOutbox(memberId, householdId);
      if (memberIdRef.current !== memberId) return;
      setEntries(currentEntries);
      setEntriesMemberId(memberId);
      setStorageError("");
    } catch (reason) {
      if (memberIdRef.current !== memberId) return;
      setStorageError((reason as Error).message);
    } finally {
      if (memberIdRef.current === memberId) setReadyMemberId(memberId);
    }
  }, [memberId, householdId]);
  useEffect(() => {
    const updateOnline = () => setOnline(navigator.onLine);
    const updateOutbox = () => void refresh();
    const showReloadMessage = (event: Event) =>
      setReloadMessage((event as CustomEvent<string>).detail);
    updateOnline();
    void refresh();
    window.addEventListener("online", updateOnline);
    window.addEventListener("offline", updateOnline);
    window.addEventListener(outboxChangeEvent, updateOutbox);
    window.addEventListener(databaseReloadEvent, showReloadMessage);
    const channel =
      typeof BroadcastChannel === "undefined" ? undefined : new BroadcastChannel(outboxChangeEvent);
    if (channel) channel.onmessage = updateOutbox;
    return () => {
      window.removeEventListener("online", updateOnline);
      window.removeEventListener("offline", updateOnline);
      window.removeEventListener(outboxChangeEvent, updateOutbox);
      window.removeEventListener(databaseReloadEvent, showReloadMessage);
      channel?.close();
    };
  }, [refresh]);
  const hasPendingEntries = entries.some((entry) => entry.status === "PENDING");
  useEffect(() => {
    if (!hasPendingEntries) return;
    const interval = window.setInterval(() => void refresh(), 5000);
    return () => window.clearInterval(interval);
  }, [hasPendingEntries, refresh]);
  const retry = useCallback(
    async (idempotencyKey: string) => {
      if (!memberId) return;
      if (!householdId) return;
      await retryPurchaseOutbox(memberId, householdId, idempotencyKey);
      await refresh();
    },
    [memberId, householdId, refresh],
  );
  const discard = useCallback(
    async (idempotencyKey: string) => {
      if (!memberId) return;
      if (!householdId) return;
      await discardPurchaseOutbox(memberId, householdId, idempotencyKey);
      await refresh();
    },
    [memberId, householdId, refresh],
  );
  return {
    entries: memberId && entriesMemberId === memberId ? entries : [],
    online,
    ready: !memberId || readyMemberId === memberId,
    storageError,
    reloadMessage,
    refresh,
    retry,
    discard,
  };
}
