import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Check, ChevronLeft, Undo2 } from "lucide-react";
import {
  makeIdempotencyKey,
  type CreatePurchaseInput,
  type Household,
  type Purchase,
} from "../../../shared/api";
import { readHouseholdPurchases } from "../../../entities/purchase";
import {
  enqueuePurchase,
  deletePurchaseDraft,
  loadPurchaseDraft,
  savePurchaseDraft,
  usePurchaseOutbox,
  parseQuantity,
} from "../../../shared/lib";
import { InlineError } from "../../../shared/ui";
import { emptyPurchaseItemsValue, type PurchaseItemsValue } from "../model/purchase-items";
import { usePurchasePayments } from "../model/purchase-payments";
import { EditPurchaseDetails } from "./edit-purchase-details";
import { EditPurchasePayments } from "./edit-purchase-payments";
import { PurchaseItemsComposer } from "./purchase-items-composer";
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

export function PurchaseCreatePage({
  household,
  onBack,
  onQueued,
  onUnsavedChanges,
}: {
  household: Household;
  onBack: () => void;
  onQueued: (idempotencyKey: string) => void;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  const [merchant, setMerchant] = useState("");
  const [occurredAt, setOccurredAt] = useState(localDateTime);
  const [itemsValue, setItemsValue] = useState<PurchaseItemsValue>(emptyPurchaseItemsValue);
  const [itemsResetToken, setItemsResetToken] = useState(0);
  const [undoToken, setUndoToken] = useState(0);
  const [undoAvailable, setUndoAvailable] = useState(false);
  const [storageReady, setStorageReady] = useState(false);
  const [draftHouseholdId, setDraftHouseholdId] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");
  const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false);
  const [savedAt, setSavedAt] = useState<Date | null>(null);
  const [draftSaveError, setDraftSaveError] = useState("");
  const [history, setHistory] = useState<Purchase[]>([]);
  const saving = useRef(false);
  const draftTimer = useRef<number | null>(null);
  const skipDraftSave = useRef(false);
  const editVersion = useRef(0);
  const markUnsaved = () => {
    editVersion.current += 1;
    onUnsavedChanges(true);
    setHasUnsavedChanges(true);
  };
  useEffect(() => {
    onUnsavedChanges(hasUnsavedChanges);
    return () => onUnsavedChanges(false);
  }, [hasUnsavedChanges, onUnsavedChanges]);
  const outbox = usePurchaseOutbox();
  const householdOutbox = outbox.entries.filter(
    (entry) => entry.input.householdId === household.id,
  );
  const enabledCategories = household.categories.filter((category) => !category.archived);
  const isEnabledCategory = (id: string | null | undefined): id is string =>
    Boolean(id && enabledCategories.some((category) => category.id === id));
  const { itemName, itemAmount, items } = itemsValue;
  const itemTotal = sum(items.map((item) => Number(item.amount) || 0));
  const payments = usePurchasePayments(household, itemTotal, markUnsaved);
  const { rows: paymentRows, autoTotal: paymentAutoTotal } = payments;
  const draft = {
    merchant,
    occurredAt,
    paymentAllocations: paymentRows.map((payment) => ({
      paymentAccountId: payment.paymentAccountId,
      amountYen:
        paymentAutoTotal && paymentRows.length === 1 ? String(itemTotal) : payment.amountYen,
    })),
    paymentAutoTotal,
    ...itemsValue,
  };
  const draftRef = useRef(draft);
  draftRef.current = draft;
  const persistDraft = useCallback(async () => {
    const current = draftRef.current;
    const version = editVersion.current;
    const hasContent = Boolean(
      current.merchant.trim() ||
      current.itemName.trim() ||
      current.itemAmount ||
      current.items.length,
    );
    setDraftSaveError("");
    try {
      if (hasContent) await savePurchaseDraft(household.id, current);
      else await deletePurchaseDraft(household.id);
      setSavedAt(new Date());
      if (editVersion.current === version) {
        onUnsavedChanges(false);
        setHasUnsavedChanges(false);
      }
    } catch (reason) {
      if (editVersion.current === version) {
        onUnsavedChanges(true);
        setDraftSaveError((reason as Error).message);
        setHasUnsavedChanges(true);
      }
    }
  }, [household.id, onUnsavedChanges]);
  useEffect(() => {
    let active = true;
    setStorageReady(false);
    setDraftHouseholdId(null);
    void loadPurchaseDraft(household.id)
      .then((loaded) => {
        if (!active) return;
        if (loaded) {
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
          } else {
            payments.setRows([
              {
                key: crypto.randomUUID(),
                paymentAccountId: loaded.paymentAccountId || household.accounts[0]?.id || "",
                amountYen: "",
              },
            ]);
            payments.setAutoTotal(true);
          }
          const restoredCategoryId = isEnabledCategory(loaded.categoryId) ? loaded.categoryId! : "";
          setItemsValue({
            itemName: loaded.itemName,
            itemAmount: loaded.itemAmount,
            categoryId: restoredCategoryId,
            items: loaded.items.map((item) => ({
              ...item,
              categoryId: isEnabledCategory(item.categoryId) ? item.categoryId : "",
            })),
          });
          setSavedAt(new Date());
        }
        setDraftHouseholdId(household.id);
      })
      .catch((reason) => active && setMessage((reason as Error).message))
      .finally(() => active && setStorageReady(true));
    return () => {
      active = false;
    };
  }, [household.id]);
  useEffect(() => {
    if (!storageReady || draftHouseholdId !== household.id) return;
    if (skipDraftSave.current) {
      skipDraftSave.current = false;
      return;
    }
    if (draftTimer.current !== null) window.clearTimeout(draftTimer.current);
    draftTimer.current = window.setTimeout(() => void persistDraft(), 450);
    return () => {
      if (draftTimer.current !== null) window.clearTimeout(draftTimer.current);
    };
  }, [
    household.id,
    persistDraft,
    storageReady,
    draftHouseholdId,
    merchant,
    occurredAt,
    paymentRows,
    paymentAutoTotal,
    itemsValue,
  ]);
  useEffect(
    () => () => {
      if (draftTimer.current !== null) window.clearTimeout(draftTimer.current);
      if (storageReady && draftHouseholdId === household.id) {
        const current = draftRef.current;
        if (
          current.merchant.trim() ||
          current.itemName.trim() ||
          current.itemAmount ||
          current.items.length
        )
          void savePurchaseDraft(household.id, current);
        else void deletePurchaseDraft(household.id);
      }
    },
    [household.id, storageReady, draftHouseholdId],
  );
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
  const [copiedPurchase, setCopiedPurchase] = useState<Purchase | null>(null);
  const paymentAmount = payments.amount;
  const paymentDifference = payments.difference;
  const hasDraftContent = Boolean(merchant.trim() || itemName.trim() || itemAmount || items.length);
  const draftStatus = hasUnsavedChanges
    ? "未保存"
    : hasDraftContent && savedAt
      ? "下書き保存済み"
      : "入力待ち";

  const copyPurchaseItems = (purchase: Purchase) => {
    markUnsaved();
    setMerchant(purchase.merchant);
    setCopiedPurchase({ ...purchase });
    setMessage("");
  };
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (saving.current) return;
    if (!items.length) {
      setMessage("品目を1つ以上追加してください");
      return;
    }
    if (!household.accounts.length) {
      setMessage("設定で決済元を登録してください");
      return;
    }
    if (
      !paymentRows.length ||
      paymentRows.some((payment) => !payment.paymentAccountId || paymentAmount(payment) < 1) ||
      paymentDifference !== 0
    ) {
      setMessage("決済元と金額を確認してください");
      return;
    }
    saving.current = true;
    setPending(true);
    setMessage("");
    try {
      if (draftTimer.current !== null) window.clearTimeout(draftTimer.current);
      const input: CreatePurchaseInput = {
        idempotencyKey: makeIdempotencyKey(),
        householdId: household.id,
        occurredAt: new Date(occurredAt).toISOString(),
        merchant: merchant.trim(),
        origin: "MANUAL",
        paymentAllocations: paymentRows.map((payment) => ({
          paymentAccountId: payment.paymentAccountId,
          amountYen: paymentAmount(payment),
        })),
        items: items.map((item) => {
          const quantity = parseQuantity(item.quantity ?? "", item.quantityUnit ?? "");
          return {
            name: item.name,
            amountYen: Number(item.amount),
            categoryId: item.categoryId || null,
            allocations: item.allocations.length
              ? item.allocations.filter((allocation) => allocation.weight > 0)
              : null,
            productVariantId: item.productVariantId || null,
            ...(quantity
              ? {
                  quantityNumerator: quantity.numerator,
                  quantityDenominator: quantity.denominator,
                  quantityUnit: quantity.unit,
                }
              : {}),
          };
        }),
      };
      await enqueuePurchase(input);
      skipDraftSave.current = true;
      setMerchant("");
      setOccurredAt(localDateTime());
      payments.reset();
      setItemsValue(emptyPurchaseItemsValue());
      setItemsResetToken((token) => token + 1);
      setUndoAvailable(false);
      setSavedAt(null);
      onUnsavedChanges(false);
      setHasUnsavedChanges(false);
      onQueued(input.idempotencyKey);
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      saving.current = false;
      setPending(false);
    }
  };
  if (!storageReady)
    return (
      <main className="loading-screen">
        <span>{message || "下書きを読み込んでいます"}</span>
      </main>
    );
  return (
    <form className={`composer ${styles.composer}`} onSubmit={save}>
      <div className={`composer-heading ${styles.heading}`}>
        <div>
          <button type="button" className="back-link" onClick={onBack}>
            <ChevronLeft size={16} />
            戻る
          </button>
          <h1>買い物を追加</h1>
        </div>
        <span className="save-status">
          <span className="status-dot" />
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
          markUnsaved();
          setMerchant(value);
        }}
        onOccurredAtChange={(value) => {
          markUnsaved();
          setOccurredAt(value);
        }}
        onSelectMerchant={(value) => {
          markUnsaved();
          setMerchant(value);
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
        resetToken={itemsResetToken}
        copiedPurchase={copiedPurchase}
        undoToken={undoToken}
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
            onClick={() => setUndoToken((token) => token + 1)}
          >
            <Undo2 size={16} />
            元に戻す
          </button>
        )}
        <button
          className="button button-primary"
          disabled={pending || !outbox.ready || Boolean(outbox.storageError) || items.length === 0}
        >
          {pending ? "保存中" : "保存して送信"}
          <Check size={16} />
        </button>
      </PurchaseItemsComposer>
      {householdOutbox.length > 0 && (
        <section className={`panel purchase-outbox ${styles.outbox}`}>
          <div className="panel-heading">
            <div>
              <span className="eyebrow">送信待ち</span>
              <h2>{householdOutbox.length}件</h2>
            </div>
          </div>
          {householdOutbox.map((entry) => {
            const failed = entry.status === "FAILED";
            const sending = (entry.leaseUntil ?? 0) > Date.now();
            const status = failed
              ? "送信失敗"
              : sending
                ? "送信中"
                : outbox.online === false
                  ? "オフライン保存済み"
                  : "送信待ち";
            return (
              <div className="purchase-outbox-row" key={entry.input.idempotencyKey}>
                <div>
                  <b>{entry.input.merchant || "店名未入力"}</b>
                  <small>
                    {status} ·{" "}
                    {yen(entry.input.items.reduce((total, item) => total + item.amountYen, 0))}
                  </small>
                  {entry.lastError && (
                    <small className="purchase-outbox-error">{entry.lastError}</small>
                  )}
                </div>
                <div className="purchase-outbox-actions">
                  {failed && (
                    <button
                      type="button"
                      className="text-button"
                      onClick={() =>
                        void outbox
                          .retry(entry.input.idempotencyKey)
                          .catch((reason) => setMessage((reason as Error).message))
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
                        .catch((reason) => setMessage((reason as Error).message))
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
