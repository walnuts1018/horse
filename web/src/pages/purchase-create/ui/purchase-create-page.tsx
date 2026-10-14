import { Check, ChevronLeft, Redo2, Undo2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type SyntheticEvent } from "react";

import type { EditPurchaseItemsCommands } from "./edit-purchase-items";

import { readHouseholdPurchases } from "../../../entities/purchase";
import { type Household, type Purchase } from "../../../shared/api";
import { errorMessage, usePurchaseOutbox, type PurchaseDraft } from "../../../shared/lib";
import { InlineError } from "../../../shared/ui";
import {
  loadPaymentAccountForMerchant,
  loadPurchaseDefaults,
  savePurchaseDefaults,
} from "../model/purchase-defaults";
import { pendingPurchaseEntryAmount } from "../model/purchase-entry-total";
import {
  emptyPurchaseItemsValue,
  restorePurchaseItemsValue,
  type PurchaseItemsValue,
} from "../model/purchase-items";
import { resolveInitialPaymentAccountId, usePurchasePayments } from "../model/purchase-payments";
import { usePurchaseDefaultHistory } from "../model/use-purchase-default-history";
import { usePurchaseDraft } from "../model/use-purchase-draft";
import { usePurchaseSubmission } from "../model/use-purchase-submission";
import { EditPurchaseDetails } from "./edit-purchase-details";
import { EditPurchasePayments } from "./edit-purchase-payments";
import { PurchaseItemsComposer } from "./purchase-items-composer";

import "./purchase-create-page.css";
import styles from "./purchase-create-page.module.css";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const localDateTime = () => {
  const now = new Date();
  now.setMinutes(now.getMinutes() - now.getTimezoneOffset());
  return now.toISOString().slice(0, 16);
};
const sum = (numbers: number[]) => numbers.reduce((total, value) => total + value, 0);

type PurchaseCreatePageProps = {
  household: Household;
  memberId: string;
  onBack: () => void;
  onQueued: (idempotencyKey: string) => void;
  onUnsavedChanges: (unsaved: boolean) => void;
  sharedDraft?: PurchaseDraft;
  sharedDraftKey?: number;
  onDraftChange?: (draft: PurchaseDraft) => void;
  submissionQueued?: boolean;
  sharedDraftId?: string;
  onShareDraft?: (draft: PurchaseDraft) => Promise<void>;
  sharedSync?: {
    status: "saved" | "saving" | "conflict" | "error";
    serverDraft?: PurchaseDraft;
    error?: string;
    recoveryError?: string;
    onUseServer: () => void;
    onKeepMine: () => void;
    onRetry: () => void;
  };
};

export function PurchaseCreatePage(props: PurchaseCreatePageProps) {
  const scope = JSON.stringify([props.memberId, props.household.id]);
  return (
    <PurchaseCreatePageContent
      key={`${scope}:${props.sharedDraft ? `shared:${props.sharedDraftKey ?? 0}` : "local"}`}
      {...props}
    />
  );
}

function PurchaseCreatePageContent({
  household,
  memberId,
  onBack,
  onQueued,
  onUnsavedChanges,
  sharedDraft,
  onDraftChange,
  submissionQueued = false,
  sharedDraftId,
  onShareDraft,
  sharedSync,
}: PurchaseCreatePageProps) {
  const accountIdsKey = household.accounts.map((account) => account.id).join("\u0000");
  const accountIds = useMemo(() => accountIdsKey.split("\u0000").filter(Boolean), [accountIdsKey]);
  const purchaseDefaults = useMemo(
    () => loadPurchaseDefaults(memberId, household.id, accountIds),
    [memberId, household.id, accountIds],
  );
  const purchaseDefaultHistory = usePurchaseDefaultHistory(household.id, accountIds);
  const [merchantOverride, setMerchant] = useState<string | null>(null);
  const merchant =
    merchantOverride ??
    (purchaseDefaults.merchant ||
      (purchaseDefaultHistory.loaded ? purchaseDefaultHistory.initialDefaults.merchant : ""));
  const [occurredAt, setOccurredAt] = useState(localDateTime);
  const [detailsExpanded, setDetailsExpanded] = useState(false);
  const [itemsValue, setItemsValue] = useState<PurchaseItemsValue>(emptyPurchaseItemsValue);
  const [historyAvailability, setHistoryAvailability] = useState({ undo: false, redo: false });
  const itemsCommands = useRef<EditPurchaseItemsCommands>(null);
  const composerRef = useRef<HTMLFormElement>(null);
  const [history, setHistory] = useState<Purchase[]>([]);
  const markUnsavedRef = useRef<() => void>(() => {});
  const markUnsaved = useCallback(() => markUnsavedRef.current(), []);
  const [copiedPurchase, setCopiedPurchase] = useState<Purchase | null>(null);
  const [copiedAt, setCopiedAt] = useState<number | null>(null);
  useEffect(() => {
    const viewport = window.visualViewport;
    if (!viewport) return;
    const updateKeyboardInset = () => {
      const activeElement = document.activeElement;
      const editingText =
        activeElement instanceof HTMLElement &&
        activeElement.matches("input:not([type=checkbox]), textarea");
      const keyboardOpen = editingText && viewport.height < window.innerHeight * 0.8;
      const inset =
        window.matchMedia("(max-width: 760px)").matches && keyboardOpen
          ? Math.max(0, window.innerHeight - viewport.height - viewport.offsetTop)
          : 0;
      composerRef.current?.style.setProperty("--purchase-keyboard-inset", `${inset}px`);
    };
    updateKeyboardInset();
    viewport.addEventListener("resize", updateKeyboardInset);
    viewport.addEventListener("scroll", updateKeyboardInset);
    window.addEventListener("resize", updateKeyboardInset);
    document.addEventListener("focusin", updateKeyboardInset);
    document.addEventListener("focusout", updateKeyboardInset);
    return () => {
      viewport.removeEventListener("resize", updateKeyboardInset);
      viewport.removeEventListener("scroll", updateKeyboardInset);
      window.removeEventListener("resize", updateKeyboardInset);
      document.removeEventListener("focusin", updateKeyboardInset);
      document.removeEventListener("focusout", updateKeyboardInset);
    };
  }, []);
  const outbox = usePurchaseOutbox(memberId, household.id);
  const householdOutbox = outbox.entries.filter(
    (entry) => entry.input.householdId === household.id,
  );
  // 送信中かどうかはリースの期限から判定するため、レンダー中ではなくタイマーで現在時刻を更新する
  const [now, setNow] = useState(() => Date.now());
  const hasLeasedEntry = householdOutbox.some((entry) => (entry.leaseUntil ?? 0) > now);
  useEffect(() => {
    if (!hasLeasedEntry) return;
    const timer = window.setInterval(() => setNow(Date.now()), 5000);
    return () => window.clearInterval(timer);
  }, [hasLeasedEntry]);
  const enabledCategories = household.categories.filter((category) => !category.archived);
  const isEnabledCategory = (id: string | null | undefined): id is string =>
    Boolean(id && enabledCategories.some((category) => category.id === id));
  const { itemName, itemAmount, items } = itemsValue;
  const itemTotal =
    sum(items.map((item) => Number(item.amount) || 0)) +
    pendingPurchaseEntryAmount(itemName, itemAmount);
  const payments = usePurchasePayments(
    household,
    itemTotal,
    markUnsaved,
    purchaseDefaults.paymentAccountId,
  );
  useEffect(() => {
    if (
      !purchaseDefaultHistory.loaded ||
      purchaseDefaults.merchant ||
      merchantOverride !== null ||
      !purchaseDefaultHistory.initialDefaults.merchant
    ) {
      return;
    }
    const paymentAccountId = purchaseDefaultHistory.initialDefaults.paymentAccountId;
    if (
      paymentAccountId &&
      payments.rows.length === 1 &&
      !payments.rows[0].paymentAccountId &&
      !payments.rows[0].amountYen
    ) {
      payments.reset(paymentAccountId);
    }
  }, [
    merchantOverride,
    payments,
    purchaseDefaultHistory.initialDefaults,
    purchaseDefaultHistory.loaded,
    purchaseDefaults.merchant,
  ]);
  const updateMerchant = (value: string, useMerchantDefault = false, preserveRedo = false) => {
    if (!preserveRedo) itemsCommands.current?.invalidateRedo();
    markUnsaved();
    setMerchant(value);
    if (!useMerchantDefault || payments.rows.length !== 1) return;
    const serverPaymentAccountId = purchaseDefaultHistory.paymentAccountForMerchant(value);
    const paymentAccountId =
      serverPaymentAccountId ??
      loadPaymentAccountForMerchant(memberId, household.id, value, accountIds);
    if (
      paymentAccountId !== payments.rows[0].paymentAccountId &&
      (serverPaymentAccountId !== undefined || paymentAccountId)
    ) {
      payments.patch(payments.rows[0].key, { paymentAccountId });
    }
  };
  const resetPayments = payments.reset;
  const { rows: paymentRows, autoTotal: paymentAutoTotal } = payments;
  const draft = useMemo(
    () => ({
      merchant,
      occurredAt,
      paymentAllocations: paymentRows.map((payment) => ({
        paymentAccountId: payment.paymentAccountId,
        amountYen:
          paymentAutoTotal && paymentRows.length === 1 ? String(itemTotal) : payment.amountYen,
      })),
      paymentAutoTotal,
      paymentAmountManuallyEdited: payments.manualAmountEdited,
      ...itemsValue,
    }),
    [
      merchant,
      occurredAt,
      paymentRows,
      paymentAutoTotal,
      payments.manualAmountEdited,
      itemTotal,
      itemsValue,
    ],
  );
  const draftPersistence = usePurchaseDraft({
    memberId,
    householdId: household.id,
    draft,
    restore: (loaded) => {
      setMerchant(loaded.merchant);
      setOccurredAt(loaded.occurredAt || localDateTime());
      if (loaded.paymentAllocations?.length) {
        payments.setRows(
          loaded.paymentAllocations.map((payment) => ({
            key: crypto.randomUUID(),
            paymentAccountId: payment.paymentAccountId,
            amountYen: payment.amountYen,
          })),
        );
        payments.setAutoTotal(loaded.paymentAutoTotal ?? false);
        payments.setManualAmountEdited(
          loaded.paymentAmountManuallyEdited ??
            (loaded.paymentAllocations.length === 1 && loaded.paymentAutoTotal === false),
        );
      } else {
        payments.setRows([
          {
            key: crypto.randomUUID(),
            paymentAccountId: resolveInitialPaymentAccountId(
              accountIds,
              loaded.paymentAccountId || undefined,
            ),
            amountYen: "",
          },
        ]);
        payments.setAutoTotal(true);
        payments.setManualAmountEdited(false);
      }
      setItemsValue(restorePurchaseItemsValue(loaded, isEnabledCategory));
    },
    onLoadError: (error) => setMessage(error.message),
    onUnsavedChanges,
    mode: sharedDraft ? "shared" : "local",
    initialDraft: sharedDraft,
  });
  // refはレンダー中に書き換えると再レンダーで巻き戻るため、コミット後に最新値を反映する
  useEffect(() => {
    markUnsavedRef.current = draftPersistence.markUnsaved;
  }, [draftPersistence.markUnsaved]);
  const {
    storageReady,
    hasUnsavedChanges,
    savedAt,
    saveError: draftSaveError,
    persistDraft,
    queued,
    cancelScheduledSave,
  } = draftPersistence;
  useEffect(() => {
    if (storageReady) onDraftChange?.(draft);
  }, [draft, onDraftChange, storageReady]);
  useEffect(() => {
    onUnsavedChanges(hasUnsavedChanges);
    return () => onUnsavedChanges(false);
  }, [hasUnsavedChanges, onUnsavedChanges]);
  useEffect(() => {
    void readHouseholdPurchases(household.id, 80)
      .then(setHistory)
      .catch(() => {});
  }, [household.id]);
  const recentPurchases = useMemo(
    () => history.filter((purchase) => purchase.status !== "DRAFT" && purchase.items.length > 0),
    [history],
  );
  const recentMerchants = useMemo(() => {
    const seen = new Set<string>();
    return purchaseDefaultHistory.recentMerchants
      .concat(recentPurchases.map((purchase) => purchase.merchant.trim()))
      .filter((name) => name && !seen.has(name) && Boolean(seen.add(name)))
      .slice(0, 5);
  }, [purchaseDefaultHistory.recentMerchants, recentPurchases]);
  const paymentAmount = payments.amount;
  const paymentDifference = payments.difference;
  const submission = usePurchaseSubmission({
    memberId,
    household,
    merchant,
    occurredAt,
    items,
    paymentRows,
    paymentAutoTotal,
    paymentAmount,
    paymentDifference,
    cancelScheduledSave,
    onQueued,
    idempotencyKey: sharedDraftId,
    onSaved: () => {
      const paymentAccountId = paymentRows[0].paymentAccountId;
      savePurchaseDefaults(memberId, household.id, {
        merchant: merchant.trim(),
        paymentAccountId,
      });
      if (sharedDraft) return;
      queued();
      setMerchant(merchant.trim());
      setOccurredAt(localDateTime());
      resetPayments(paymentAccountId);
      setItemsValue(emptyPurchaseItemsValue());
      itemsCommands.current?.reset();
    },
  });
  const { pending, message, setMessage, fieldError, clearFieldError, submit } = submission;
  const [sharing, setSharing] = useState(false);
  const shareDraft = async () => {
    if (!onShareDraft || sharing) return;
    setSharing(true);
    try {
      await persistDraft();
      await onShareDraft(draft);
    } catch (reason) {
      setMessage(errorMessage(reason));
    } finally {
      setSharing(false);
    }
  };
  const paymentSummary = paymentRows
    .map(
      (payment) =>
        household.accounts.find((account) => account.id === payment.paymentAccountId)?.name ??
        "未選択",
    )
    .join("・");
  const purchaseDate = new Date(occurredAt);
  const dateSummary = Number.isNaN(purchaseDate.getTime())
    ? "日時未設定"
    : purchaseDate.toLocaleString("ja-JP", {
        month: "numeric",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      });
  const handleSubmit = (event: SyntheticEvent<HTMLFormElement>) => {
    const submittedItems = itemsCommands.current?.prepareForSubmit();
    if (!submittedItems) {
      event.preventDefault();
      return;
    }
    const submittedItemTotal = sum(submittedItems.map((item) => Number(item.amount) || 0));
    const submittedPaymentAmount = (payment: (typeof paymentRows)[number]) =>
      paymentAutoTotal && paymentRows.at(-1)?.key === payment.key
        ? submittedItemTotal -
          paymentRows.slice(0, -1).reduce((total, row) => total + (Number(row.amountYen) || 0), 0)
        : payments.amount(payment);
    const submittedAutoRemainder =
      paymentAutoTotal && paymentRows.length > 1
        ? submittedPaymentAmount(paymentRows[paymentRows.length - 1])
        : undefined;
    const submittedPaymentDifference =
      submittedAutoRemainder !== undefined && submittedAutoRemainder < 0
        ? submittedAutoRemainder
        : submittedItemTotal -
          paymentRows.reduce((total, payment) => total + submittedPaymentAmount(payment), 0);
    void submit(event, {
      items: submittedItems,
      paymentAmount: submittedPaymentAmount,
      paymentDifference: submittedPaymentDifference,
    });
  };
  const hasDraftContent = Boolean(
    merchant.trim() ||
    itemName.trim() ||
    itemAmount ||
    items.length ||
    paymentRows.some((payment) => payment.paymentAccountId || payment.amountYen.trim()),
  );
  const draftStatus = sharedSync
    ? sharedSync.status === "saving"
      ? "同期中"
      : sharedSync.status === "conflict"
        ? "競合"
        : sharedSync.status === "error"
          ? "同期失敗"
          : "共有済み"
    : hasUnsavedChanges
      ? "未保存"
      : hasDraftContent && savedAt
        ? "下書き保存済み"
        : "入力待ち";

  const copyPurchaseItems = (purchase: Purchase) => {
    itemsCommands.current?.copy(purchase);
    requestAnimationFrame(() => {
      const input = composerRef.current?.querySelector<HTMLInputElement>(
        '[data-grid-row="0"][data-grid-column="name"]',
      );
      if (!input) return;
      input.focus({ preventScroll: true });
      input.scrollIntoView({
        block: "center",
        behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth",
      });
    });
    updateMerchant(purchase.merchant, false, true);
    setCopiedPurchase({ ...purchase });
    setCopiedAt(Date.now());
    setMessage("");
  };
  if (!storageReady)
    return (
      <main className="loading-screen">
        <span>{message || "下書きを読み込んでいます"}</span>
      </main>
    );
  return (
    <form ref={composerRef} className={`composer ${styles.composer}`} onSubmit={handleSubmit}>
      <div className={`composer-heading ${styles.heading}`}>
        <div>
          <button type="button" className="back-link" onClick={onBack}>
            <ChevronLeft size={16} />
            戻る
          </button>
          <h1>{sharedDraft ? "共有下書き" : "買い物を追加"}</h1>
        </div>
        <div className={styles.headingActions}>
          {!sharedDraft && onShareDraft && (
            <button
              type="button"
              className="button button-soft"
              disabled={sharing || !hasDraftContent}
              onClick={() => void shareDraft()}
            >
              {sharing ? "共有中" : "共有する"}
            </button>
          )}
          <span className={`save-status ${styles.saveStatus}`}>
            <span className={`status-dot ${styles.statusDot}`} />
            {draftStatus}
          </span>
        </div>
      </div>
      {sharedSync?.status === "conflict" && sharedSync.serverDraft && (
        <section className={`panel ${styles.sharedConflict}`} aria-live="polite">
          <h2>別の編集があります</h2>
          <p>
            {sharedSync.serverDraft.merchant || "店名未入力"} ·{" "}
            {sharedSync.serverDraft.items.length}件 ·{" "}
            {yen(
              sharedSync.serverDraft.items.reduce(
                (total, item) => total + (Number(item.amount) || 0),
                0,
              ),
            )}
          </p>
          <div>
            <button type="button" className="button button-soft" onClick={sharedSync.onUseServer}>
              サーバー版を使う
            </button>
            <button type="button" className="button button-primary" onClick={sharedSync.onKeepMine}>
              手元の内容を保存
            </button>
          </div>
        </section>
      )}
      {sharedSync?.status === "error" && sharedSync.error && (
        <section className={styles.sharedConflict} role="alert">
          <InlineError message={sharedSync.error} />
          <button type="button" className="text-button" onClick={sharedSync.onRetry}>
            再試行
          </button>
        </section>
      )}
      <EditPurchaseDetails
        merchant={merchant}
        occurredAt={occurredAt}
        history={history}
        recentMerchants={recentMerchants}
        recentPurchases={recentPurchases}
        copiedPurchase={copiedPurchase}
        copiedAt={copiedAt}
        occurredAtError={fieldError?.field === "occurredAt"}
        summary={
          <span className={styles.summarySecondary}>
            {dateSummary} · {paymentSummary}
          </span>
        }
        detailsExpanded={detailsExpanded || Boolean(fieldError)}
        onToggleDetails={() => setDetailsExpanded((expanded) => !expanded)}
        classNames={{
          details: styles.details,
          history: styles.history,
          summary: styles.detailsSummary,
          summaryMerchant: styles.summaryMerchant,
          summaryToggle: styles.detailsSummaryToggle,
          detailsFields: styles.detailsFields,
        }}
        onMerchantChange={(value) => {
          updateMerchant(value, recentMerchants.includes(value));
        }}
        onOccurredAtChange={(value) => {
          markUnsaved();
          clearFieldError("occurredAt");
          setOccurredAt(value);
        }}
        onSelectMerchant={(value) => {
          updateMerchant(value, true);
          setCopiedPurchase(null);
        }}
        onCopyPurchase={copyPurchaseItems}
      >
        <EditPurchasePayments
          household={household}
          history={recentPurchases}
          rows={paymentRows}
          fieldError={fieldError?.field === "occurredAt" ? null : fieldError}
          autoTotal={paymentAutoTotal}
          itemTotal={itemTotal}
          difference={paymentDifference}
          onSetAutoTotal={(enabled, amount) => {
            markUnsaved();
            const lastPaymentKey = paymentRows.at(-1)?.key;
            if (lastPaymentKey) clearFieldError("paymentAmount", lastPaymentKey);
            payments.setAutoTotal(enabled, amount);
          }}
          onAdd={payments.add}
          onPatch={(key, patch) => {
            if (patch.paymentAccountId !== undefined) clearFieldError("paymentAccount", key);
            if (patch.amountYen !== undefined) clearFieldError("paymentAmount", key);
            payments.patch(key, patch);
          }}
          onRemove={(key) => {
            clearFieldError("paymentAccount", key);
            clearFieldError("paymentAmount", key);
            payments.remove(key);
          }}
        />
      </EditPurchaseDetails>
      <PurchaseItemsComposer
        householdId={household.id}
        merchant={merchant}
        copiedPurchase={copiedPurchase}
        household={household}
        history={history}
        value={itemsValue}
        commandsRef={itemsCommands}
        itemTotal={itemTotal}
        hasDraftContent={hasDraftContent}
        hasUnsavedChanges={hasUnsavedChanges}
        savedAt={savedAt}
        classNames={{
          items: styles.items,
          itemHeading: styles.itemHeading,
          itemTable: styles.itemTable,
          itemList: styles.itemList,
          entryForm: styles.entryForm,
          entryHint: styles.entryHint,
          entryInputTarget: styles.entryInputTarget,
          bottom: styles.bottom,
        }}
        onChange={setItemsValue}
        onDirty={markUnsaved}
        onHistoryAvailabilityChange={setHistoryAvailability}
      >
        {historyAvailability.undo && (
          <button
            type="button"
            className="button button-soft"
            onClick={() => {
              const snapshot = itemsCommands.current?.undo();
              if (snapshot && Object.hasOwn(snapshot, "merchant"))
                setMerchant(snapshot.merchant ?? "");
              if (snapshot && Object.hasOwn(snapshot, "copiedPurchase"))
                setCopiedPurchase(snapshot.copiedPurchase ?? null);
            }}
          >
            <Undo2 size={16} />
            元に戻す
          </button>
        )}
        {historyAvailability.redo && (
          <button
            type="button"
            className="button button-soft"
            onClick={() => {
              const snapshot = itemsCommands.current?.redo();
              if (snapshot && Object.hasOwn(snapshot, "merchant"))
                setMerchant(snapshot.merchant ?? "");
              if (snapshot && Object.hasOwn(snapshot, "copiedPurchase"))
                setCopiedPurchase(snapshot.copiedPurchase ?? null);
            }}
          >
            <Redo2 size={16} />
            やり直す
          </button>
        )}
        <button
          type="submit"
          className="button button-primary"
          disabled={
            pending ||
            submissionQueued ||
            !outbox.ready ||
            Boolean(outbox.storageError) ||
            (!items.length && !itemName.trim() && !itemAmount.trim())
          }
        >
          {pending ? "保存中" : submissionQueued ? "送信待ち" : "保存して送信"}
          <Check size={16} />
        </button>
      </PurchaseItemsComposer>
      {householdOutbox.length > 0 && (
        <section className={`panel purchase-outbox ${styles.outbox}`}>
          <div className={`panel-heading ${styles.outboxHeading}`}>
            <div>
              <span className="eyebrow">送信待ち</span>
              <h2 className={styles.outboxCount}>{householdOutbox.length}件</h2>
            </div>
          </div>
          {householdOutbox.map((entry) => {
            const failed = entry.status === "FAILED";
            const sending = (entry.leaseUntil ?? 0) > now;
            const status = failed
              ? "送信失敗"
              : sending
                ? "送信中"
                : !outbox.online
                  ? "オフライン保存済み"
                  : "送信待ち";
            return (
              <div className={styles.outboxRow} key={entry.input.idempotencyKey}>
                <div>
                  <b>{entry.input.merchant || "店名未入力"}</b>
                  <small>
                    {status} ·{" "}
                    {yen(entry.input.items.reduce((total, item) => total + item.amountYen, 0))}
                  </small>
                  {entry.lastError && (
                    <small className={styles.outboxError}>{entry.lastError}</small>
                  )}
                </div>
                <div className={styles.outboxActions}>
                  {failed && (
                    <button
                      type="button"
                      className="text-button"
                      onClick={() =>
                        void outbox
                          .retry(entry.input.idempotencyKey)
                          .catch((reason) => setMessage(errorMessage(reason)))
                      }
                    >
                      再送
                    </button>
                  )}
                  <button
                    type="button"
                    className="text-button"
                    disabled={sending}
                    onClick={() =>
                      void outbox
                        .discard(entry.input.idempotencyKey)
                        .catch((reason) => setMessage(errorMessage(reason)))
                    }
                  >
                    破棄
                  </button>
                </div>
              </div>
            );
          })}
        </section>
      )}
      {(outbox.storageError || message || draftSaveError || sharedSync?.recoveryError) && (
        <div className={styles.errors}>
          {outbox.storageError && <InlineError message={outbox.storageError} />}
          {message && <InlineError message={message} />}
          {sharedSync?.recoveryError && <InlineError message={sharedSync.recoveryError} />}
          {draftSaveError && (
            <div className={styles.draftSaveFailure} role="alert">
              <InlineError message={draftSaveError} />
              <button type="button" className="text-button" onClick={() => void persistDraft()}>
                保存を再試行
              </button>
            </div>
          )}
          {sharedSync?.error && <InlineError message={sharedSync.error} />}
        </div>
      )}
    </form>
  );
}
