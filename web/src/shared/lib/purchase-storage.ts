import { useCallback, useEffect, useState } from "react";
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
const draftsStore = "purchase-drafts-v2";
const outboxStore = "purchase-outbox-v2";
const outboxChangeEvent = "horse:purchase-outbox-change";
const databaseReloadEvent = "horse:database-reload-required";
const leaseMilliseconds = 30_000;
const databaseVersion = 2;

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

function migrateOutbox(value: unknown, key: IDBValidKey): PurchaseOutboxEntry | undefined {
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
  } as PurchaseOutboxEntry;
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
      if (!database.objectStoreNames.contains(draftsStore))
        database.createObjectStore(draftsStore, { keyPath: "householdId" });
      if (!database.objectStoreNames.contains(outboxStore))
        database.createObjectStore(outboxStore, { keyPath: "idempotencyKey" });
      if (event.oldVersion < databaseVersion) {
        migrateStore(transaction, legacyDraftsStore, draftsStore, migrateDraft);
        migrateStore(transaction, legacyOutboxStore, outboxStore, migrateOutbox);
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

function readAll<T>(storeName: string) {
  return openDatabase().then(
    (database) =>
      new Promise<T[]>((resolve, reject) => {
        const transaction = database.transaction(storeName, "readonly");
        const request = transaction.objectStore(storeName).getAll();
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

export async function loadPurchaseDraft(householdId: string): Promise<PurchaseDraft | undefined> {
  const current = await readValue<PurchaseDraft & { householdId: string; cleared?: boolean }>(
    draftsStore,
    householdId,
  );
  if (current?.cleared) {
    try {
      localStorage.removeItem(`horse:draft:${householdId}`);
    } catch {
      // 保存済みの削除記録を優先し、古い下書きの再移行を防ぐ。
    }
    return undefined;
  }
  if (current) return current;
  const legacyKey = `horse:draft:${householdId}`;
  try {
    const legacy = localStorage.getItem(legacyKey);
    if (!legacy) return undefined;
    const draft = JSON.parse(legacy) as Partial<PurchaseDraft>;
    const migrated: PurchaseDraft & { householdId: string } = {
      householdId,
      merchant: draft.merchant ?? "",
      occurredAt: draft.occurredAt ?? "",
      paymentAccountId: draft.paymentAccountId ?? "",
      itemName: draft.itemName ?? "",
      itemAmount: draft.itemAmount ?? "",
      categoryId: draft.categoryId ?? "",
      items: draft.items ?? [],
    };
    await writeValue(draftsStore, migrated);
    localStorage.removeItem(legacyKey);
    return migrated;
  } catch {
    throw new Error("以前の下書きを移行できませんでした。ブラウザの保存容量を確認してください");
  }
}

export function savePurchaseDraft(householdId: string, draft: PurchaseDraft) {
  return writeValue(draftsStore, { ...draft, householdId });
}

export function deletePurchaseDraft(householdId: string) {
  return writeValue(draftsStore, { householdId, cleared: true });
}

export async function listPurchaseOutbox() {
  return readAll<PurchaseOutboxEntry>(outboxStore);
}

export async function enqueuePurchase(input: CreatePurchaseInput) {
  input = normalizePurchaseInput(input);
  const now = Date.now();
  const database = await openDatabase();
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction([draftsStore, outboxStore], "readwrite");
      transaction.objectStore(outboxStore).put({
        idempotencyKey: input.idempotencyKey,
        input,
        status: "PENDING",
        createdAt: now,
        updatedAt: now,
      } satisfies PurchaseOutboxEntry);
      transaction.objectStore(draftsStore).put({ householdId: input.householdId, cleared: true });
      transaction.oncomplete = () => resolve();
      transaction.onerror = () =>
        reject(transaction.error ?? new Error("送信待ちを保存できません"));
      transaction.onabort = () =>
        reject(transaction.error ?? new Error("送信待ちを保存できません"));
    });
  } finally {
    database.close();
  }
  try {
    localStorage.removeItem(`horse:draft:${input.householdId}`);
  } catch {
    // IndexedDBに移行済みのため、削除に失敗しても送信データを保持する。
  }
  notifyOutboxChanged();
}

export async function claimPurchaseOutbox(
  idempotencyKey: string,
  owner: string,
): Promise<PurchaseOutboxEntry | undefined> {
  const database = await openDatabase();
  return new Promise((resolve, reject) => {
    const transaction = database.transaction(outboxStore, "readwrite");
    const store = transaction.objectStore(outboxStore);
    const request = store.get(idempotencyKey);
    let claimed: PurchaseOutboxEntry | undefined;
    request.onsuccess = () => {
      const entry = request.result as PurchaseOutboxEntry | undefined;
      if (!entry || entry.status !== "PENDING" || (entry.leaseUntil ?? 0) > Date.now()) return;
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
  idempotencyKey: string,
  owner: string,
  message: string,
) {
  const database = await openDatabase();
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(outboxStore, "readwrite");
      const store = transaction.objectStore(outboxStore);
      const request = store.get(idempotencyKey);
      request.onsuccess = () => {
        const entry = request.result as PurchaseOutboxEntry | undefined;
        if (!entry || entry.leaseOwner !== owner) return;
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

export async function completePurchaseOutbox(idempotencyKey: string, owner: string) {
  const database = await openDatabase();
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(outboxStore, "readwrite");
      const store = transaction.objectStore(outboxStore);
      const request = store.get(idempotencyKey);
      request.onsuccess = () => {
        const entry = request.result as PurchaseOutboxEntry | undefined;
        if (entry?.leaseOwner === owner) store.delete(idempotencyKey);
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

export async function renewPurchaseOutboxLease(idempotencyKey: string, owner: string) {
  const database = await openDatabase();
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(outboxStore, "readwrite");
      const store = transaction.objectStore(outboxStore);
      const request = store.get(idempotencyKey);
      request.onsuccess = () => {
        const entry = request.result as PurchaseOutboxEntry | undefined;
        if (!entry || entry.status !== "PENDING" || entry.leaseOwner !== owner) return;
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

export async function retryPurchaseOutbox(idempotencyKey: string) {
  const database = await openDatabase();
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(outboxStore, "readwrite");
      const store = transaction.objectStore(outboxStore);
      const request = store.get(idempotencyKey);
      request.onsuccess = () => {
        const entry = request.result as PurchaseOutboxEntry | undefined;
        if (!entry) return;
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

export async function discardPurchaseOutbox(idempotencyKey: string) {
  await deleteValue(outboxStore, idempotencyKey);
  notifyOutboxChanged();
}

export function usePurchaseOutbox() {
  const [entries, setEntries] = useState<PurchaseOutboxEntry[]>([]);
  const [online, setOnline] = useState<boolean | null>(null);
  const [ready, setReady] = useState(false);
  const [storageError, setStorageError] = useState("");
  const [reloadMessage, setReloadMessage] = useState("");
  const refresh = useCallback(async () => {
    try {
      setEntries(await listPurchaseOutbox());
      setStorageError("");
    } catch (reason) {
      setStorageError((reason as Error).message);
    } finally {
      setReady(true);
    }
  }, []);
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
      await retryPurchaseOutbox(idempotencyKey);
      await refresh();
    },
    [refresh],
  );
  const discard = useCallback(
    async (idempotencyKey: string) => {
      await discardPurchaseOutbox(idempotencyKey);
      await refresh();
    },
    [refresh],
  );
  return { entries, online, ready, storageError, reloadMessage, refresh, retry, discard };
}
