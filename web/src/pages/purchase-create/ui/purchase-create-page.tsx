import { Check, ChevronLeft, Undo2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { EditPurchaseItemsCommands } from "./edit-purchase-items";

import { readHouseholdPurchases } from "../../../entities/purchase";
import { type Household, type Purchase } from "../../../shared/api";
import { errorMessage, usePurchaseOutbox } from "../../../shared/lib";
import { InlineError } from "../../../shared/ui";
import {
  loadPaymentAccountForMerchant,
  loadPurchaseDefaults,
  savePurchaseDefaults,
} from "../model/purchase-defaults";
import { emptyPurchaseItemsValue, type PurchaseItemsValue } from "../model/purchase-items";
import { resolveInitialPaymentAccountId, usePurchasePayments } from "../model/purchase-payments";
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
};

export function PurchaseCreatePage(props: PurchaseCreatePageProps) {
  const scope = JSON.stringify([props.memberId, props.household.id]);
  return <PurchaseCreatePageContent key={scope} {...props} />;
}

function PurchaseCreatePageContent({
  household,
  memberId,
  onBack,
  onQueued,
  onUnsavedChanges,
}: PurchaseCreatePageProps) {
  const accountIdsKey = household.accounts.map((account) => account.id).join("\u0000");
  const accountIds = useMemo(() => accountIdsKey.split("\u0000").filter(Boolean), [accountIdsKey]);
  const purchaseDefaults = useMemo(
    () => loadPurchaseDefaults(memberId, household.id, accountIds),
    [memberId, household.id, accountIds],
  );
  const [merchant, setMerchant] = useState(purchaseDefaults.merchant);
  const [occurredAt, setOccurredAt] = useState(localDateTime);
  const [itemsValue, setItemsValue] = useState<PurchaseItemsValue>(emptyPurchaseItemsValue);
  const [undoAvailable, setUndoAvailable] = useState(false);
  const itemsCommands = useRef<EditPurchaseItemsCommands>(null);
  const [history, setHistory] = useState<Purchase[]>([]);
  const markUnsavedRef = useRef<() => void>(() => {});
  const markUnsaved = useCallback(() => markUnsavedRef.current(), []);
  const [copiedPurchase, setCopiedPurchase] = useState<Purchase | null>(null);
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
  const itemTotal = sum(items.map((item) => Number(item.amount) || 0));
  const payments = usePurchasePayments(
    household,
    itemTotal,
    markUnsaved,
    purchaseDefaults.paymentAccountId,
  );
  const updateMerchant = (value: string, useMerchantDefault = false) => {
    markUnsaved();
    setMerchant(value);
    if (!useMerchantDefault || payments.rows.length !== 1) return;
    const paymentAccountId = loadPaymentAccountForMerchant(
      memberId,
      household.id,
      value,
      accountIds,
    );
    if (paymentAccountId && paymentAccountId !== payments.rows[0].paymentAccountId) {
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
      const restoredCategoryId = isEnabledCategory(loaded.categoryId) ? loaded.categoryId : "";
      setItemsValue({
        itemName: loaded.itemName,
        itemAmount: loaded.itemAmount,
        categoryId: restoredCategoryId,
        items: loaded.items.map((item) => ({
          ...item,
          categoryId: isEnabledCategory(item.categoryId) ? item.categoryId : "",
        })),
      });
    },
    onLoadError: (error) => setMessage(error.message),
    onUnsavedChanges,
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
    return recentPurchases
      .map((purchase) => purchase.merchant.trim())
      .filter((name) => name && !seen.has(name) && Boolean(seen.add(name)))
      .slice(0, 5);
  }, [recentPurchases]);
  const paymentAmount = payments.amount;
  const paymentDifference = payments.difference;
  const submission = usePurchaseSubmission({
    memberId,
    household,
    merchant,
    occurredAt,
    items,
    paymentRows,
    paymentAmount,
    paymentDifference,
    cancelScheduledSave,
    onQueued,
    onSaved: () => {
      const paymentAccountId = paymentRows[0].paymentAccountId;
      savePurchaseDefaults(memberId, household.id, {
        merchant: merchant.trim(),
        paymentAccountId,
      });
      queued();
      setMerchant(merchant.trim());
      setOccurredAt(localDateTime());
      resetPayments(paymentAccountId);
      setItemsValue(emptyPurchaseItemsValue());
      itemsCommands.current?.reset();
    },
  });
  const { pending, message, setMessage, submit } = submission;
  const hasDraftContent = Boolean(merchant.trim() || itemName.trim() || itemAmount || items.length);
  const draftStatus = hasUnsavedChanges
    ? "未保存"
    : hasDraftContent && savedAt
      ? "下書き保存済み"
      : "入力待ち";

  const copyPurchaseItems = (purchase: Purchase) => {
    updateMerchant(purchase.merchant);
    setCopiedPurchase({ ...purchase });
    itemsCommands.current?.copy(purchase);
    setMessage("");
  };
  if (!storageReady)
    return (
      <main className="loading-screen">
        <span>{message || "下書きを読み込んでいます"}</span>
      </main>
    );
  return (
    <form className={`composer ${styles.composer}`} onSubmit={submit}>
      <div className={`composer-heading ${styles.heading}`}>
        <div>
          <button type="button" className="back-link" onClick={onBack}>
            <ChevronLeft size={16} />
            戻る
          </button>
          <h1>買い物を追加</h1>
        </div>
        <span className={`save-status ${styles.saveStatus}`}>
          <span className={`status-dot ${styles.statusDot}`} />
          {draftStatus}
        </span>
      </div>
      <EditPurchaseDetails
        merchant={merchant}
        occurredAt={occurredAt}
        history={history}
        recentMerchants={recentMerchants}
        recentPurchases={recentPurchases}
        copiedPurchase={copiedPurchase}
        classNames={{ details: styles.details, history: styles.history }}
        onMerchantChange={(value) => {
          updateMerchant(value, recentMerchants.includes(value));
        }}
        onOccurredAtChange={(value) => {
          markUnsaved();
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
          rows={paymentRows}
          autoTotal={paymentAutoTotal}
          itemTotal={itemTotal}
          difference={paymentDifference}
          onAdd={payments.add}
          onPatch={payments.patch}
          onRemove={payments.remove}
        />
      </EditPurchaseDetails>
      <PurchaseItemsComposer
        householdId={household.id}
        merchant={merchant}
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
        onUndoAvailabilityChange={setUndoAvailable}
      >
        {undoAvailable && (
          <button
            type="button"
            className="button button-soft"
            onClick={() => itemsCommands.current?.undo()}
          >
            <Undo2 size={16} />
            元に戻す
          </button>
        )}
        <button
          type="submit"
          className="button button-primary"
          disabled={pending || !outbox.ready || Boolean(outbox.storageError) || items.length === 0}
        >
          {pending ? "保存中" : "保存して送信"}
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
      {(outbox.storageError || message || draftSaveError) && (
        <div className={styles.errors}>
          {outbox.storageError && <InlineError message={outbox.storageError} />}
          {message && <InlineError message={message} />}
          {draftSaveError && (
            <div className={styles.draftSaveFailure} role="alert">
              <InlineError message={draftSaveError} />
              <button type="button" className="text-button" onClick={() => void persistDraft()}>
                保存を再試行
              </button>
            </div>
          )}
        </div>
      )}
    </form>
  );
}
