import { openDB, unwrap, type IDBPDatabase, type IDBPTransaction } from "idb";
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";

import type { CreatePurchaseInput } from "../api";

import { errorMessage } from "./errors";
import { isRecord } from "./guard";

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
  paymentAmountManuallyEdited?: boolean;
  itemName: string;
  itemAmount: string;
  selectedProductVariantId?: string;
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

export type ImportReceiptDraft = {
  householdId: string;
  documentId: string;
  baseResult: Record<string, unknown>;
  editedResult: Record<string, unknown>;
  confirmationState?: ImportReceiptConfirmationState;
};

export type ImportReceiptConfirmationState = {
  selectedCandidate?: {
    id: string;
    amounts: number[];
    computedTotal: number;
    consistent: boolean;
    issues: string[];
  };
  manualAmountOverrides?: Record<string, number>;
  manualAmountOverrideReason?: string;
  receiptItemReviewStates?: Record<string, ImportReceiptItemReviewState>;
  payments?: Array<{
    key: string;
    method?: string;
    cardLast4?: string;
    paymentAccountId: string;
    amountYen: number;
  }>;
  itemAllocations?: Record<string, Array<{ poolId: string; weight: number }>>;
};

export type ImportReceiptItemReviewState = "UNREVIEWED" | "CONFIRMED" | "CORRECTED" | "UNRESOLVED";

function isPurchaseDraft(
  value: unknown,
): value is PurchaseDraft & { storageKey: string; cleared?: boolean } {
  return (
    isRecord(value) &&
    typeof value.storageKey === "string" &&
    typeof value.merchant === "string" &&
    typeof value.occurredAt === "string" &&
    typeof value.itemName === "string" &&
    typeof value.itemAmount === "string" &&
    (value.selectedProductVariantId === undefined ||
      typeof value.selectedProductVariantId === "string") &&
    typeof value.categoryId === "string" &&
    Array.isArray(value.items)
  );
}

function isPurchaseOutboxEntry(value: unknown): value is PurchaseOutboxEntry {
  return (
    isRecord(value) &&
    typeof value.storageKey === "string" &&
    typeof value.memberId === "string" &&
    typeof value.householdId === "string" &&
    typeof value.idempotencyKey === "string" &&
    typeof value.createdAt === "number" &&
    typeof value.updatedAt === "number" &&
    (value.status === "PENDING" || value.status === "FAILED") &&
    isRecord(value.input) &&
    Array.isArray(value.input.items)
  );
}

const databaseName = "horse-browser-data";
const legacyDraftsStore = "purchase-drafts";
const legacyOutboxStore = "purchase-outbox";
const unscopedDraftsStore = "purchase-drafts-v2";
const unscopedOutboxStore = "purchase-outbox-v2";
const draftsStore = "purchase-drafts-v3";
const outboxStore = "purchase-outbox-v3";
const importReceiptDraftsStore = "import-receipt-drafts-v1";
const outboxChangeEvent = "horse:purchase-outbox-change";
const databaseReloadEvent = "horse:database-reload-required";
const leaseMilliseconds = 30_000;
const databaseVersion = 4;

function importReceiptDraftStorageKey(householdId: string, documentId: string) {
  return JSON.stringify([householdId, documentId]);
}

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

function migrateStore(
  transaction: IDBTransaction,
  sourceName: string,
  targetName: string,
  normalize: (value: unknown, key: IDBValidKey) => unknown,
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
  if (!isRecord(value)) return undefined;
  const draft = value;
  const householdId = [draft.householdId, draft.id, key].find(
    (candidate): candidate is string => typeof candidate === "string" && candidate.length > 0,
  );
  if (!householdId) return undefined;
  return { ...draft, householdId };
}

function migrateOutbox(value: unknown, key: IDBValidKey): Record<string, unknown> | undefined {
  if (!isRecord(value)) return undefined;
  const entry = value;
  const rawInput = entry.input ?? entry.request ?? entry;
  const input = isRecord(rawInput) ? rawInput : undefined;
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

function openDatabase(): Promise<IDBPDatabase> {
  if (typeof indexedDB === "undefined")
    return Promise.reject(new Error("このブラウザでは下書きを保存できません"));
  let rejectBlocked: ((reason: Error) => void) | undefined;
  let wasBlocked = false;
  const blocked = new Promise<never>((_, reject) => {
    rejectBlocked = reject;
  });
  const opening = openDB(databaseName, databaseVersion, {
    upgrade(database, oldVersion, _newVersion, transaction) {
      if (oldVersion < 2) {
        if (!database.objectStoreNames.contains(unscopedDraftsStore))
          database.createObjectStore(unscopedDraftsStore, { keyPath: "householdId" });
        if (!database.objectStoreNames.contains(unscopedOutboxStore))
          database.createObjectStore(unscopedOutboxStore, { keyPath: "idempotencyKey" });
        const nativeTransaction = unwrap(transaction);
        migrateStore(nativeTransaction, legacyDraftsStore, unscopedDraftsStore, migrateDraft);
        migrateStore(nativeTransaction, legacyOutboxStore, unscopedOutboxStore, migrateOutbox);
      }
      if (oldVersion < 3) {
        if (!database.objectStoreNames.contains(draftsStore))
          database.createObjectStore(draftsStore, { keyPath: "storageKey" });
        if (!database.objectStoreNames.contains(outboxStore)) {
          const store = database.createObjectStore(outboxStore, { keyPath: "storageKey" });
          store.createIndex("memberId", "memberId");
          store.createIndex("memberHousehold", ["memberId", "householdId"]);
        }
      }
      if (oldVersion < 4 && !database.objectStoreNames.contains(importReceiptDraftsStore))
        database.createObjectStore(importReceiptDraftsStore, { keyPath: "storageKey" });
    },
    blocked() {
      wasBlocked = true;
      const message = "別のhorseタブを再読み込みしてください";
      requestDatabaseReload(message);
      rejectBlocked?.(new Error(message));
    },
  });
  void opening.then(
    (database) => {
      if (wasBlocked) {
        database.close();
        return;
      }
      database.onversionchange = () => {
        database.close();
        requestDatabaseReload("保存データが更新されました。再読み込みしてください");
      };
    },
    () => {},
  );
  return Promise.race([opening, blocked]).catch((reason: unknown) => {
    if (reason instanceof Error) throw reason;
    throw new Error("下書きデータベースを開けません", { cause: reason });
  });
}

function notifyOutboxChanged() {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new Event(outboxChangeEvent));
  if (typeof BroadcastChannel !== "undefined") {
    outboxBroadcast ??= new BroadcastChannel(outboxChangeEvent);
    // BroadcastChannelのpostMessageにはtargetOrigin引数が存在しないため、このルールの対象外
    // oxlint-disable-next-line unicorn/require-post-message-target-origin
    outboxBroadcast.postMessage("changed");
  }
}

let outboxBroadcast: BroadcastChannel | undefined;

async function withTransaction<T, Mode extends "readonly" | "readwrite">(
  storeNames: string | string[],
  mode: Mode,
  operation: (transaction: IDBPTransaction<unknown, string[], Mode>) => Promise<T>,
): Promise<T> {
  const database = await openDatabase();
  const transaction = database.transaction(storeNames, mode);
  try {
    const operationResult = operation(transaction);
    const [result] = await Promise.all([operationResult, transaction.done]);
    return result;
  } finally {
    database.close();
  }
}

function writeValue(storeName: string, value: unknown) {
  return withTransaction(storeName, "readwrite", async (transaction) => {
    await transaction.objectStore(storeName).put(value);
  });
}

function deleteValue(storeName: string, key: IDBValidKey) {
  return withTransaction(storeName, "readwrite", async (transaction) => {
    await transaction.objectStore(storeName).delete(key);
  });
}

function readValue(storeName: string, key: IDBValidKey) {
  return withTransaction(storeName, "readonly", (transaction) =>
    transaction.objectStore(storeName).get(key),
  );
}

function readAllByIndex(storeName: string, indexName: string, key: IDBValidKey) {
  return withTransaction(storeName, "readonly", (transaction) =>
    transaction.objectStore(storeName).index(indexName).getAll(key),
  );
}

export async function loadPurchaseDraft(
  memberId: string,
  householdId: string,
): Promise<PurchaseDraft | undefined> {
  const storageKey = draftStorageKey(memberId, householdId);
  const current = await readValue(draftsStore, storageKey);
  if (!isPurchaseDraft(current)) return undefined;
  if (current.cleared) return undefined;
  return current;
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

export async function loadImportReceiptDraft(householdId: string, documentId: string) {
  const value: unknown = await readValue(
    importReceiptDraftsStore,
    importReceiptDraftStorageKey(householdId, documentId),
  );
  if (
    !isRecord(value) ||
    value.householdId !== householdId ||
    value.documentId !== documentId ||
    !isRecord(value.baseResult) ||
    !isRecord(value.editedResult)
  )
    return undefined;
  return {
    householdId,
    documentId,
    baseResult: value.baseResult,
    editedResult: value.editedResult,
    ...(isImportReceiptConfirmationState(value.confirmationState)
      ? { confirmationState: value.confirmationState }
      : {}),
  } satisfies ImportReceiptDraft;
}

function isImportReceiptConfirmationState(value: unknown): value is ImportReceiptConfirmationState {
  if (!isRecord(value)) return false;
  if (
    value.selectedCandidate !== undefined &&
    (!isRecord(value.selectedCandidate) ||
      typeof value.selectedCandidate.id !== "string" ||
      !Array.isArray(value.selectedCandidate.amounts) ||
      !value.selectedCandidate.amounts.every(Number.isSafeInteger) ||
      typeof value.selectedCandidate.computedTotal !== "number" ||
      typeof value.selectedCandidate.consistent !== "boolean" ||
      !Array.isArray(value.selectedCandidate.issues) ||
      !value.selectedCandidate.issues.every((issue) => typeof issue === "string"))
  )
    return false;
  if (
    value.manualAmountOverrides !== undefined &&
    (!isRecord(value.manualAmountOverrides) ||
      !Object.values(value.manualAmountOverrides).every(Number.isSafeInteger))
  )
    return false;
  if (
    value.manualAmountOverrideReason !== undefined &&
    typeof value.manualAmountOverrideReason !== "string"
  )
    return false;
  if (
    value.receiptItemReviewStates !== undefined &&
    (!isRecord(value.receiptItemReviewStates) ||
      !Object.values(value.receiptItemReviewStates).every(
        (state) =>
          state === "UNREVIEWED" ||
          state === "CONFIRMED" ||
          state === "CORRECTED" ||
          state === "UNRESOLVED",
      ))
  )
    return false;
  if (
    value.payments !== undefined &&
    (!Array.isArray(value.payments) ||
      !value.payments.every(
        (payment) =>
          isRecord(payment) &&
          typeof payment.key === "string" &&
          (payment.method === undefined || typeof payment.method === "string") &&
          (payment.cardLast4 === undefined || typeof payment.cardLast4 === "string") &&
          typeof payment.paymentAccountId === "string" &&
          Number.isSafeInteger(payment.amountYen),
      ))
  )
    return false;
  if (
    value.itemAllocations !== undefined &&
    (!isRecord(value.itemAllocations) ||
      !Object.values(value.itemAllocations).every(
        (allocations) =>
          Array.isArray(allocations) &&
          allocations.every(
            (allocation) =>
              isRecord(allocation) &&
              typeof allocation.poolId === "string" &&
              typeof allocation.weight === "number",
          ),
      ))
  )
    return false;
  return true;
}

export function saveImportReceiptDraft(draft: ImportReceiptDraft) {
  return writeValue(importReceiptDraftsStore, {
    ...draft,
    storageKey: importReceiptDraftStorageKey(draft.householdId, draft.documentId),
  });
}

export function deleteImportReceiptDraft(householdId: string, documentId: string) {
  return deleteValue(
    importReceiptDraftsStore,
    importReceiptDraftStorageKey(householdId, documentId),
  );
}

export async function listPurchaseOutbox(memberId: string, householdId?: string) {
  const values = await readAllByIndex(
    outboxStore,
    householdId ? "memberHousehold" : "memberId",
    householdId ? [memberId, householdId] : memberId,
  );
  return values.filter(isPurchaseOutboxEntry);
}

export async function enqueuePurchase(
  input: CreatePurchaseInput,
  memberId: string,
  options: { preserveLocalDraft?: boolean } = {},
) {
  input = normalizePurchaseInput(input);
  const now = Date.now();
  const storageKey = outboxStorageKey(memberId, input.householdId, input.idempotencyKey);
  const stores = options.preserveLocalDraft ? [outboxStore] : [draftsStore, outboxStore];
  await withTransaction(stores, "readwrite", async (transaction) => {
    const store = transaction.objectStore(outboxStore);
    const existing: unknown = await store.get(storageKey);
    if (isPurchaseOutboxEntry(existing)) {
      if (JSON.stringify(existing.input) !== JSON.stringify(input)) {
        throw new Error(
          "同じ共有下書きから異なる内容を送信待ちにできません。送信待ちの購入を確認してください",
        );
      }
      return;
    }
    await store.put({
      storageKey,
      memberId,
      householdId: input.householdId,
      idempotencyKey: input.idempotencyKey,
      input,
      status: "PENDING",
      createdAt: now,
      updatedAt: now,
    } satisfies PurchaseOutboxEntry);
    if (!options.preserveLocalDraft) {
      await transaction.objectStore(draftsStore).put({
        storageKey: draftStorageKey(memberId, input.householdId),
        cleared: true,
      });
    }
  });
  notifyOutboxChanged();
}

export async function claimPurchaseOutbox(
  memberId: string,
  householdId: string,
  idempotencyKey: string,
  owner: string,
): Promise<PurchaseOutboxEntry | undefined> {
  const claimed = await withTransaction(outboxStore, "readwrite", async (transaction) => {
    const store = transaction.objectStore(outboxStore);
    const result: unknown = await store.get(
      outboxStorageKey(memberId, householdId, idempotencyKey),
    );
    const entry = isPurchaseOutboxEntry(result) ? result : undefined;
    if (
      !entry ||
      entry.memberId !== memberId ||
      entry.householdId !== householdId ||
      entry.status !== "PENDING" ||
      (entry.leaseUntil ?? 0) > Date.now()
    )
      return undefined;
    const claimedEntry = {
      ...entry,
      input: normalizePurchaseInput(entry.input),
      leaseOwner: owner,
      leaseUntil: Date.now() + leaseMilliseconds,
      updatedAt: Date.now(),
    };
    await store.put(claimedEntry);
    return claimedEntry;
  });
  if (claimed) notifyOutboxChanged();
  return claimed;
}

export async function markPurchaseOutboxFailed(
  memberId: string,
  householdId: string,
  idempotencyKey: string,
  owner: string,
  message: string,
) {
  await withTransaction(outboxStore, "readwrite", async (transaction) => {
    const store = transaction.objectStore(outboxStore);
    const result: unknown = await store.get(
      outboxStorageKey(memberId, householdId, idempotencyKey),
    );
    const entry = isPurchaseOutboxEntry(result) ? result : undefined;
    if (
      !entry ||
      entry.memberId !== memberId ||
      entry.householdId !== householdId ||
      entry.leaseOwner !== owner
    )
      return;
    await store.put({
      ...entry,
      status: "FAILED",
      lastError: message,
      leaseOwner: undefined,
      leaseUntil: undefined,
      updatedAt: Date.now(),
    });
  });
  notifyOutboxChanged();
}

export async function completePurchaseOutbox(
  memberId: string,
  householdId: string,
  idempotencyKey: string,
  owner: string,
) {
  await withTransaction(outboxStore, "readwrite", async (transaction) => {
    const store = transaction.objectStore(outboxStore);
    const result: unknown = await store.get(
      outboxStorageKey(memberId, householdId, idempotencyKey),
    );
    const entry = isPurchaseOutboxEntry(result) ? result : undefined;
    if (
      entry?.memberId === memberId &&
      entry.householdId === householdId &&
      entry.leaseOwner === owner
    )
      await store.delete(outboxStorageKey(memberId, householdId, idempotencyKey));
  });
  notifyOutboxChanged();
}

export async function renewPurchaseOutboxLease(
  memberId: string,
  householdId: string,
  idempotencyKey: string,
  owner: string,
) {
  await withTransaction(outboxStore, "readwrite", async (transaction) => {
    const store = transaction.objectStore(outboxStore);
    const result: unknown = await store.get(
      outboxStorageKey(memberId, householdId, idempotencyKey),
    );
    const entry = isPurchaseOutboxEntry(result) ? result : undefined;
    if (
      !entry ||
      entry.memberId !== memberId ||
      entry.householdId !== householdId ||
      entry.status !== "PENDING" ||
      entry.leaseOwner !== owner
    )
      return;
    await store.put({
      ...entry,
      leaseUntil: Date.now() + leaseMilliseconds,
      updatedAt: Date.now(),
    });
  });
  notifyOutboxChanged();
}

export async function retryPurchaseOutbox(
  memberId: string,
  householdId: string,
  idempotencyKey: string,
) {
  await withTransaction(outboxStore, "readwrite", async (transaction) => {
    const store = transaction.objectStore(outboxStore);
    const result: unknown = await store.get(
      outboxStorageKey(memberId, householdId, idempotencyKey),
    );
    const entry = isPurchaseOutboxEntry(result) ? result : undefined;
    if (!entry || entry.memberId !== memberId || entry.householdId !== householdId) return;
    await store.put({
      ...entry,
      status: "PENDING",
      lastError: undefined,
      leaseOwner: undefined,
      leaseUntil: undefined,
      updatedAt: Date.now(),
    });
  });
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

// onlineはnavigatorを外部ストアとして扱い、購読はイベントリスナーで賄う
const subscribeOnline = (onStoreChange: () => void) => {
  window.addEventListener("online", onStoreChange);
  window.addEventListener("offline", onStoreChange);
  return () => {
    window.removeEventListener("online", onStoreChange);
    window.removeEventListener("offline", onStoreChange);
  };
};
const getOnlineSnapshot = () => typeof navigator === "undefined" || navigator.onLine;
const getServerOnlineSnapshot = () => true;

export function usePurchaseOutbox(memberId?: string, householdId?: string) {
  const [entries, setEntries] = useState<PurchaseOutboxEntry[]>([]);
  const [entriesMemberId, setEntriesMemberId] = useState<string>();
  const online = useSyncExternalStore(subscribeOnline, getOnlineSnapshot, getServerOnlineSnapshot);
  const [readyMemberId, setReadyMemberId] = useState<string>();
  const [storageError, setStorageError] = useState("");
  const [reloadMessage, setReloadMessage] = useState("");
  const memberIdRef = useRef(memberId);
  useEffect(() => {
    memberIdRef.current = memberId;
  }, [memberId]);
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
      setStorageError(errorMessage(reason));
    } finally {
      if (memberIdRef.current === memberId) setReadyMemberId(memberId);
    }
  }, [memberId, householdId]);
  useEffect(() => {
    const updateOutbox = () => void refresh();
    const showReloadMessage = (event: Event) => {
      if (!(event instanceof CustomEvent)) return;
      const detail: unknown = event.detail;
      if (typeof detail === "string") setReloadMessage(detail);
    };
    // refreshはmemberId未設定時に同期でsetStateするため、effect直後ではなくマイクロタスクで走らせる
    queueMicrotask(() => void refresh());
    window.addEventListener(outboxChangeEvent, updateOutbox);
    window.addEventListener(databaseReloadEvent, showReloadMessage);
    const channel =
      typeof BroadcastChannel === "undefined" ? undefined : new BroadcastChannel(outboxChangeEvent);
    if (channel) channel.onmessage = updateOutbox;
    return () => {
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
  const ready = !memberId || readyMemberId === memberId;
  // 戻り値を安定させることで、呼び出し側がeffectの依存にoutboxそのものを指定できる
  return useMemo(
    () => ({
      entries: memberId && entriesMemberId === memberId ? entries : [],
      online,
      ready,
      storageError,
      reloadMessage,
      refresh,
      retry,
      discard,
    }),
    [
      entries,
      memberId,
      entriesMemberId,
      online,
      ready,
      storageError,
      reloadMessage,
      refresh,
      retry,
      discard,
    ],
  );
}
