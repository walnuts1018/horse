import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
  type ClipboardEvent,
} from "react";
import {
  Check,
  ChevronDown,
  ChevronLeft,
  Plus,
  ReceiptJapaneseYen,
  Search,
  Undo2,
  X,
} from "lucide-react";
import { z } from "zod";
import { Popover } from "@base-ui/react/popover";
import {
  makeIdempotencyKey,
  readGraphql,
  type Connection,
  type CreatePurchaseInput,
  type Household,
  type Purchase,
  type ProductSuggestion,
} from "../../../shared/api";
import { purchasesQuery } from "../../../entities/purchase";
import { productSuggestionsQuery } from "../../../entities/product";
import {
  enqueuePurchase,
  deletePurchaseDraft,
  loadPurchaseDraft,
  savePurchaseDraft,
  usePurchaseOutbox,
  parseQuantity,
  type PurchaseDraftItem,
} from "../../../shared/lib";
import { InlineError } from "../../../shared/ui";

type DraftItem = PurchaseDraftItem;
type PaymentDraft = { key: string; paymentAccountId: string; amountYen: string };
type CategorySource = "default" | "history" | "product" | "manual" | "restored";
const purchaseItemSchema = z.object({
  name: z.string().trim().min(1),
  amount: z.string().regex(/^[1-9]\d*$/),
});
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
  const defaultCategoryId = household.categories.find((item) => !item.archived)?.id ?? "";
  const [merchant, setMerchant] = useState("");
  const [occurredAt, setOccurredAt] = useState(localDateTime);
  const [paymentRows, setPaymentRows] = useState<PaymentDraft[]>(() => [
    {
      key: crypto.randomUUID(),
      paymentAccountId: household.accounts[0]?.id ?? "",
      amountYen: "",
    },
  ]);
  const [paymentAutoTotal, setPaymentAutoTotal] = useState(true);
  const [items, setItems] = useState<DraftItem[]>([]);
  const [itemName, setItemName] = useState("");
  const [itemAmount, setItemAmount] = useState("");
  const [selectedProductVariantId, setSelectedProductVariantId] = useState("");
  const [productSuggestions, setProductSuggestions] = useState<ProductSuggestion[]>([]);
  const [categoryId, setCategoryId] = useState(defaultCategoryId);
  const categorySource = useRef<CategorySource>("default");
  const [storageReady, setStorageReady] = useState(false);
  const [draftHouseholdId, setDraftHouseholdId] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");
  const [undoItems, setUndoItems] = useState<DraftItem[] | null>(null);
  const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false);
  const [savedAt, setSavedAt] = useState<Date | null>(null);
  const [history, setHistory] = useState<Purchase[]>([]);
  const [focused, setFocused] = useState<"name" | "amount" | null>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const amountRef = useRef<HTMLInputElement>(null);
  const itemGridRef = useRef<HTMLDivElement>(null);
  const saving = useRef(false);
  const draftTimer = useRef<number | null>(null);
  const skipDraftSave = useRef(false);
  const editVersion = useRef(0);
  const composing = useRef(false);
  const markUnsaved = () => {
    editVersion.current += 1;
    setHasUnsavedChanges(true);
  };
  useEffect(() => {
    onUnsavedChanges(hasUnsavedChanges);
    return () => onUnsavedChanges(false);
  }, [hasUnsavedChanges, onUnsavedChanges]);
  const updateCategory = (id: string, source: CategorySource) => {
    categorySource.current = source;
    setCategoryId(id);
  };
  const changeItemName = (name: string) => {
    setItemName(name);
    setSelectedProductVariantId("");
    if (categorySource.current === "history" || categorySource.current === "product")
      updateCategory(defaultCategoryId, "default");
  };
  const outbox = usePurchaseOutbox();
  const householdOutbox = outbox.entries.filter(
    (entry) => entry.input.householdId === household.id,
  );
  const enabledCategories = household.categories.filter((category) => !category.archived);
  const defaultAllocations = household.defaultAllocation?.length
    ? household.defaultAllocation
    : household.pools.map((pool) => ({ poolId: pool.id, weight: pool.defaultShareWeight || 1 }));

  const itemTotal = sum(items.map((item) => Number(item.amount) || 0));
  const draft = {
    merchant,
    occurredAt,
    paymentAllocations: paymentRows.map((payment) => ({
      paymentAccountId: payment.paymentAccountId,
      amountYen:
        paymentAutoTotal && paymentRows.length === 1 ? String(itemTotal) : payment.amountYen,
    })),
    paymentAutoTotal,
    itemName,
    itemAmount,
    categoryId,
    items,
  };
  const draftRef = useRef(draft);
  draftRef.current = draft;
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
            setPaymentRows(
              loaded.paymentAllocations.map((payment) => ({
                key: crypto.randomUUID(),
                paymentAccountId: payment.paymentAccountId,
                amountYen: payment.amountYen,
              })),
            );
            setPaymentAutoTotal(loaded.paymentAutoTotal ?? false);
          } else {
            setPaymentRows([
              {
                key: crypto.randomUUID(),
                paymentAccountId: loaded.paymentAccountId || household.accounts[0]?.id || "",
                amountYen: "",
              },
            ]);
            setPaymentAutoTotal(true);
          }
          setItemName(loaded.itemName);
          setItemAmount(loaded.itemAmount);
          updateCategory(loaded.categoryId || defaultCategoryId, "restored");
          setItems(loaded.items);
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
    draftTimer.current = window.setTimeout(() => {
      const current = draftRef.current;
      const version = editVersion.current;
      const store =
        current.merchant.trim() ||
        current.itemName.trim() ||
        current.itemAmount ||
        current.items.length
          ? savePurchaseDraft(household.id, current)
          : deletePurchaseDraft(household.id);
      void store
        .then(() => {
          setSavedAt(new Date());
          if (editVersion.current === version) setHasUnsavedChanges(false);
        })
        .catch((reason) => setMessage((reason as Error).message));
    }, 450);
    return () => {
      if (draftTimer.current !== null) window.clearTimeout(draftTimer.current);
    };
  }, [
    household.id,
    storageReady,
    draftHouseholdId,
    merchant,
    occurredAt,
    paymentRows,
    paymentAutoTotal,
    itemName,
    itemAmount,
    categoryId,
    items,
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
    void readGraphql<{ purchases: Connection<Purchase> }>(purchasesQuery, {
      householdId: household.id,
      first: 80,
    })
      .then((data) => setHistory(data.purchases.edges.map((edge) => edge.node)))
      .catch(() => {});
  }, [household.id]);
  useEffect(() => {
    const query = itemName.trim();
    if (query.length < 2) {
      setProductSuggestions([]);
      return;
    }
    let active = true;
    const timer = window.setTimeout(() => {
      void readGraphql<{ productSuggestions: ProductSuggestion[] }>(productSuggestionsQuery, {
        householdId: household.id,
        query,
        merchant: merchant.trim() || null,
        first: 6,
      })
        .then((result) => active && setProductSuggestions(result.productSuggestions))
        .catch(() => active && setProductSuggestions([]));
    }, 220);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [household.id, itemName, merchant]);
  useEffect(() => {
    if (
      !itemName.trim() ||
      categorySource.current === "manual" ||
      categorySource.current === "restored" ||
      categorySource.current === "product"
    )
      return;
    const match = history
      .flatMap((purchase) => purchase.items)
      .find((item) => item.name.toLocaleLowerCase() === itemName.trim().toLocaleLowerCase());
    if (match?.categoryId) {
      categorySource.current = "history";
      setCategoryId(match.categoryId);
    } else if (categorySource.current === "history") {
      categorySource.current = "default";
      setCategoryId(defaultCategoryId);
    }
  }, [itemName, history, defaultCategoryId]);
  const suggestions = useMemo(() => {
    const seen = new Set<string>();
    return history
      .flatMap((purchase) => purchase.items.map((item) => item.name))
      .filter(
        (name) =>
          name.toLocaleLowerCase().includes(itemName.trim().toLocaleLowerCase()) &&
          !seen.has(name) &&
          Boolean(seen.add(name)),
      )
      .slice(0, 4);
  }, [history, itemName]);
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
  const paymentAmount = (payment: PaymentDraft) =>
    Number(paymentAutoTotal && paymentRows.length === 1 ? itemTotal : payment.amountYen) || 0;
  const paymentTotal = sum(paymentRows.map(paymentAmount));
  const paymentDifference = itemTotal - paymentTotal;
  const hasDraftContent = Boolean(merchant.trim() || itemName.trim() || itemAmount || items.length);

  const addPaymentRow = () => {
    markUnsaved();
    setPaymentRows((current) => [
      ...current.map((payment) => ({
        ...payment,
        amountYen: paymentAutoTotal && current.length === 1 ? String(itemTotal) : payment.amountYen,
      })),
      { key: crypto.randomUUID(), paymentAccountId: "", amountYen: "" },
    ]);
    setPaymentAutoTotal(false);
  };
  const patchPayment = (key: string, patch: Partial<PaymentDraft>) => {
    markUnsaved();
    if (patch.amountYen !== undefined) setPaymentAutoTotal(false);
    setPaymentRows((current) =>
      current.map((payment) => (payment.key === key ? { ...payment, ...patch } : payment)),
    );
  };
  const removePaymentRow = (key: string) => {
    markUnsaved();
    setPaymentRows((current) => current.filter((payment) => payment.key !== key));
  };

  const addItem = (name = itemName, amount = itemAmount) => {
    const parsed = purchaseItemSchema.safeParse({ name, amount });
    if (!parsed.success) return false;
    markUnsaved();
    const item: DraftItem = {
      key: crypto.randomUUID(),
      ...parsed.data,
      categoryId,
      ...(selectedProductVariantId ? { productVariantId: selectedProductVariantId } : {}),
      allocations: [],
    };
    setUndoItems(items);
    setItems((current) => [...current, item]);
    setItemName("");
    setItemAmount("");
    updateCategory(defaultCategoryId, "default");
    setSelectedProductVariantId("");
    setProductSuggestions([]);
    setFocused("name");
    nameRef.current?.focus();
    return true;
  };
  const onNameKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (
      event.key === "Enter" &&
      !event.nativeEvent.isComposing &&
      !composing.current &&
      itemName.trim()
    ) {
      event.preventDefault();
      amountRef.current?.focus();
      setFocused("amount");
    }
  };
  const onAmountKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter" && !event.nativeEvent.isComposing && !composing.current) {
      event.preventDefault();
      addItem();
    }
  };
  const onItemGridKey = (
    event: KeyboardEvent<HTMLInputElement>,
    row: number,
    column: "name" | "amount",
  ) => {
    if (
      event.altKey ||
      event.ctrlKey ||
      event.metaKey ||
      event.nativeEvent.isComposing ||
      composing.current
    )
      return;

    let targetRow = row;
    let targetColumn = column;
    if (event.key === "Enter") {
      event.preventDefault();
      if (column === "name") {
        itemGridRef.current
          ?.querySelector<HTMLInputElement>(`[data-grid-row="${row}"][data-grid-column="amount"]`)
          ?.focus();
      } else if (row + 1 < items.length) {
        itemGridRef.current
          ?.querySelector<HTMLInputElement>(`[data-grid-row="${row + 1}"][data-grid-column="name"]`)
          ?.focus();
      } else {
        nameRef.current?.focus();
      }
      return;
    }
    if (event.key === "ArrowUp" || event.key === "ArrowDown") {
      targetRow += event.key === "ArrowUp" ? -1 : 1;
    } else if (event.key === "ArrowLeft" && column === "amount") {
      const input = event.currentTarget;
      if (input.selectionStart !== 0 || input.selectionEnd !== 0) return;
      targetColumn = "name";
    } else if (event.key === "ArrowRight" && column === "name") {
      const input = event.currentTarget;
      if (input.selectionStart !== input.value.length || input.selectionEnd !== input.value.length)
        return;
      targetColumn = "amount";
    } else {
      return;
    }

    event.preventDefault();
    if (targetRow === items.length && event.key === "ArrowDown") {
      (targetColumn === "name" ? nameRef : amountRef).current?.focus();
      return;
    }
    if (targetRow < 0 || targetRow >= items.length) return;
    itemGridRef.current
      ?.querySelector<HTMLInputElement>(
        `[data-grid-row="${targetRow}"][data-grid-column="${targetColumn}"]`,
      )
      ?.focus();
  };
  const pasteRows = (event: ClipboardEvent<HTMLInputElement>) => {
    const text = event.clipboardData.getData("text");
    if (!text.includes("\n") && !text.includes("\t")) return;
    event.preventDefault();
    const parsed = text
      .trim()
      .split(/\r?\n/)
      .map((line) => line.split(/\t|,(?=\s*\d)/).map((cell) => cell.trim()))
      .filter((cells) => cells[0] && cells[1] && /^¥?[\d,]+$/.test(cells[1]));
    if (!parsed.length) return;
    markUnsaved();
    setUndoItems(items);
    setItems((current) => [
      ...current,
      ...parsed.map(([name, amount]) => ({
        key: crypto.randomUUID(),
        name,
        amount: amount.replace(/[¥,]/g, ""),
        categoryId: categorySource.current === "manual" ? categoryId : defaultCategoryId,
        allocations: [],
      })),
    ]);
  };
  const removeItem = (key: string) => {
    if (!items.some((item) => item.key === key)) return;
    markUnsaved();
    setUndoItems(items);
    setItems((current) => current.filter((item) => item.key !== key));
  };
  const undo = () => {
    if (undoItems) {
      markUnsaved();
      setItems(undoItems);
      setUndoItems(null);
    }
  };
  const patchItem = (key: string, patch: Partial<DraftItem>) => {
    markUnsaved();
    setItems((current) => current.map((item) => (item.key === key ? { ...item, ...patch } : item)));
  };
  const copyPurchaseItems = (purchase: Purchase) => {
    markUnsaved();
    setUndoItems(items);
    setItems(
      purchase.items.map((item) => ({
        key: crypto.randomUUID(),
        name: item.name,
        amount: String(item.amountYen),
        categoryId: item.categoryId ?? "",
        productVariantId: item.productVariantId ?? undefined,
        allocations: item.allocations.map(({ poolId, weight }) => ({ poolId, weight })),
        ...(item.quantityNumerator && item.quantityDenominator
          ? {
              quantity: `${item.quantityNumerator}/${item.quantityDenominator}`,
              quantityUnit: item.quantityUnit ?? "",
            }
          : {}),
      })),
    );
    setMerchant(purchase.merchant);
    setCopiedPurchase(purchase);
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
      setPaymentRows([
        {
          key: crypto.randomUUID(),
          paymentAccountId: household.accounts[0]?.id ?? "",
          amountYen: "",
        },
      ]);
      setPaymentAutoTotal(true);
      setItemName("");
      setItemAmount("");
      updateCategory(defaultCategoryId, "default");
      setItems([]);
      setSavedAt(null);
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
  const allocationName = (item: DraftItem) => {
    if (
      item.allocations.length === 0 ||
      JSON.stringify(item.allocations) === JSON.stringify(defaultAllocations)
    )
      return "標準";
    if (item.allocations.length === 1)
      return `${household.pools.find((pool) => pool.id === item.allocations[0].poolId)?.name ?? "口座"} 100%`;
    return item.allocations
      .map(
        (allocation) =>
          household.pools.find((pool) => pool.id === allocation.poolId)?.name ?? "口座",
      )
      .join(" / ");
  };

  return (
    <form className="composer" onSubmit={save}>
      <div className="composer-heading">
        <div>
          <button type="button" className="back-link" onClick={onBack}>
            <ChevronLeft size={16} />
            戻る
          </button>
          <h1>買い物を追加</h1>
        </div>
        <span className="save-status">
          <span className="status-dot" />
          {savedAt ? "下書き保存済み" : "下書き保存中"}
        </span>
      </div>
      <section className="panel purchase-meta-form">
        <div className="field-group">
          <label htmlFor="merchant">店名</label>
          <input
            id="merchant"
            placeholder="例 スーパーいちば"
            value={merchant}
            onChange={(e) => {
              markUnsaved();
              setMerchant(e.target.value);
            }}
            list="merchant-suggestions"
            autoComplete="off"
          />
          <datalist id="merchant-suggestions">
            {Array.from(new Set(history.map((item) => item.merchant).filter(Boolean)))
              .slice(0, 8)
              .map((name) => (
                <option key={name} value={name} />
              ))}
          </datalist>
        </div>
        <div className="field-group">
          <label htmlFor="occurred">日時</label>
          <input
            id="occurred"
            type="datetime-local"
            value={occurredAt}
            onChange={(e) => {
              markUnsaved();
              setOccurredAt(e.target.value);
            }}
          />
        </div>
        <div className="field-group payment-allocation-field">
          <label>決済内訳</label>
          <div className="payment-allocation-list">
            {paymentRows.map((payment, index) => (
              <div className="payment-allocation-row" key={payment.key}>
                <select
                  aria-label={`決済元 ${index + 1}`}
                  value={payment.paymentAccountId}
                  onChange={(event) =>
                    patchPayment(payment.key, { paymentAccountId: event.target.value })
                  }
                >
                  <option value="">決済元を選択</option>
                  {household.accounts.map((account) => (
                    <option key={account.id} value={account.id}>
                      {account.name}
                    </option>
                  ))}
                </select>
                <div className="yen-input">
                  <span>¥</span>
                  <input
                    aria-label={`決済額 ${index + 1}`}
                    inputMode="numeric"
                    value={
                      paymentAutoTotal && paymentRows.length === 1
                        ? String(itemTotal || "")
                        : payment.amountYen
                    }
                    onChange={(event) =>
                      patchPayment(payment.key, {
                        amountYen: event.target.value.replace(/\D/g, ""),
                      })
                    }
                  />
                </div>
                <button
                  className="row-remove"
                  type="button"
                  aria-label={`決済元 ${index + 1} を削除`}
                  disabled={paymentRows.length <= 1}
                  onClick={() => removePaymentRow(payment.key)}
                >
                  <X size={15} />
                </button>
              </div>
            ))}
          </div>
          <div className="payment-allocation-footer">
            <button className="text-button" type="button" onClick={addPaymentRow}>
              <Plus size={14} />
              決済元を追加
            </button>
            <span className={paymentDifference === 0 ? "payment-balanced" : "payment-unbalanced"}>
              {paymentDifference === 0
                ? "一致"
                : `${paymentDifference > 0 ? "不足" : "超過"} ${yen(Math.abs(paymentDifference))}`}
            </span>
          </div>
        </div>
      </section>
      {(recentMerchants.length > 0 || recentPurchases.length > 0) && (
        <section className="panel" aria-label="購入履歴から入力">
          {recentMerchants.length > 0 && (
            <div className="field-group">
              <span>同じ店でもう一度入力</span>
              <div className="purchase-history-actions">
                {recentMerchants.map((name) => (
                  <button
                    className="text-button"
                    type="button"
                    key={name}
                    onClick={() => {
                      setMerchant(name);
                      setCopiedPurchase(null);
                    }}
                  >
                    {name}
                  </button>
                ))}
              </div>
            </div>
          )}
          {recentPurchases.length > 0 && (
            <div className="field-group">
              <span>前回の購入をコピー</span>
              <div className="purchase-history-actions">
                {recentPurchases.slice(0, 5).map((purchase) => (
                  <button
                    className="text-button"
                    type="button"
                    key={purchase.id}
                    onClick={() => copyPurchaseItems(purchase)}
                  >
                    {purchase.merchant || "店名未入力"} ·{" "}
                    {new Date(purchase.occurredAt).toLocaleDateString("ja-JP")} ·{" "}
                    {yen(purchase.totalYen)}
                  </button>
                ))}
              </div>
              {copiedPurchase && (
                <small role="status">
                  {copiedPurchase.merchant || "店名未入力"}
                  の品目と前回価格をコピーしました。価格、日時、決済元を確認してください。
                </small>
              )}
            </div>
          )}
        </section>
      )}
      <section className="panel item-entry-panel">
        <div className="panel-heading entry-heading">
          <div>
            <span className="eyebrow">品目</span>
            <h2>買ったもの</h2>
          </div>
          <span className="entry-default">
            負担 <b>{household.pools.map((pool) => pool.name.slice(0, 1)).join(" / ") || "標準"}</b>
          </span>
        </div>
        <div className="item-table-head">
          <span>品名</span>
          <span>金額</span>
          <span>カテゴリ</span>
          <span>負担</span>
          <span />
        </div>
        <div className="item-list" ref={itemGridRef}>
          {items.map((item, index) => (
            <div className="item-entry" key={item.key}>
              <div className="item-edit-row">
                <span className="item-index">{String(index + 1).padStart(2, "0")}</span>
                <input
                  aria-label="品名"
                  data-grid-row={index}
                  data-grid-column="name"
                  value={item.name}
                  onChange={(e) => patchItem(item.key, { name: e.target.value })}
                  onKeyDown={(event) => onItemGridKey(event, index, "name")}
                />
                <div className="yen-input">
                  <span>¥</span>
                  <input
                    aria-label="金額"
                    data-grid-row={index}
                    data-grid-column="amount"
                    inputMode="numeric"
                    value={item.amount}
                    onChange={(e) =>
                      patchItem(item.key, { amount: e.target.value.replace(/\D/g, "") })
                    }
                    onKeyDown={(event) => onItemGridKey(event, index, "amount")}
                  />
                </div>
                <select
                  aria-label="カテゴリ"
                  value={item.categoryId}
                  onChange={(e) => patchItem(item.key, { categoryId: e.target.value })}
                >
                  <option value="">未分類</option>
                  {enabledCategories.map((category) => (
                    <option key={category.id} value={category.id}>
                      {category.name}
                    </option>
                  ))}
                </select>
                <Popover.Root>
                  <Popover.Trigger className="allocation-chip">
                    {allocationName(item)}
                    <ChevronDown size={12} />
                  </Popover.Trigger>
                  <Popover.Portal>
                    <Popover.Positioner sideOffset={6}>
                      <Popover.Popup className="allocation-popup">
                        <Popover.Title>負担割合</Popover.Title>
                        {household.pools.map((pool) => {
                          const weight =
                            (item.allocations.length ? item.allocations : defaultAllocations).find(
                              (allocation) => allocation.poolId === pool.id,
                            )?.weight ?? 0;
                          return (
                            <label className="allocation-option" key={pool.id}>
                              <span>{pool.name}</span>
                              <span className="number-field">
                                <input
                                  aria-label={`${pool.name}の負担比率`}
                                  inputMode="numeric"
                                  value={weight || ""}
                                  onChange={(event) => {
                                    const nextWeight = Math.max(
                                      0,
                                      Number(event.target.value.replace(/\D/g, "")) || 0,
                                    );
                                    const base = item.allocations.length
                                      ? item.allocations
                                      : defaultAllocations;
                                    const allocations = base.filter(
                                      (allocation) => allocation.poolId !== pool.id,
                                    );
                                    if (nextWeight > 0)
                                      allocations.push({ poolId: pool.id, weight: nextWeight });
                                    patchItem(item.key, { allocations });
                                  }}
                                />
                                <small>比</small>
                              </span>
                            </label>
                          );
                        })}
                        <button
                          type="button"
                          className="text-button"
                          onClick={() => patchItem(item.key, { allocations: [] })}
                        >
                          標準に戻す
                        </button>
                      </Popover.Popup>
                    </Popover.Positioner>
                  </Popover.Portal>
                </Popover.Root>
                <button
                  type="button"
                  className="row-remove"
                  aria-label="品目を削除"
                  onClick={() => removeItem(item.key)}
                >
                  <X size={15} />
                </button>
              </div>
              <details className="item-measure-details">
                <summary>数量</summary>
                <label>
                  数量
                  <input
                    aria-label={`${item.name}の数量`}
                    value={item.quantity ?? ""}
                    onChange={(event) => patchItem(item.key, { quantity: event.target.value })}
                    placeholder="1、1/2"
                  />
                </label>
                <label>
                  単位
                  <input
                    aria-label={`${item.name}の数量単位`}
                    value={item.quantityUnit ?? ""}
                    onChange={(event) => patchItem(item.key, { quantityUnit: event.target.value })}
                    placeholder="個、g、ml"
                    maxLength={32}
                  />
                </label>
              </details>
            </div>
          ))}
          {items.length === 0 && (
            <div className="empty-items">
              <ReceiptJapaneseYen size={24} />
              <span>品名と金額を入力して追加</span>
            </div>
          )}
        </div>
        <div className={`entry-form ${focused ? "entry-focused" : ""}`}>
          <div className="entry-input-wrap name-wrap">
            <input
              ref={nameRef}
              value={itemName}
              onChange={(e) => {
                markUnsaved();
                changeItemName(e.target.value);
              }}
              onFocus={() => setFocused("name")}
              onBlur={() => window.setTimeout(() => setFocused(null), 160)}
              onKeyDown={onNameKey}
              onPaste={pasteRows}
              onCompositionStart={() => {
                composing.current = true;
              }}
              onCompositionEnd={() => {
                composing.current = false;
              }}
              placeholder="品名を入力"
              aria-label="品名"
              autoComplete="off"
            />
            <kbd>Enter</kbd>
            {focused === "name" && suggestions.length > 0 && productSuggestions.length === 0 && (
              <div className="suggestions">
                {suggestions.map((name) => (
                  <button
                    type="button"
                    key={name}
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => {
                      changeItemName(name);
                      amountRef.current?.focus();
                      setFocused("amount");
                    }}
                  >
                    <Search size={13} />
                    {name}
                    <span>購入履歴</span>
                  </button>
                ))}
              </div>
            )}
            {focused === "name" && productSuggestions.length > 0 && !selectedProductVariantId && (
              <div className="suggestions product-suggestions">
                {productSuggestions.map((candidate) => (
                  <button
                    type="button"
                    key={candidate.variant.id}
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => {
                      setSelectedProductVariantId(candidate.variant.id);
                      changeItemName(candidate.matchedName || itemName.trim());
                      if (
                        candidate.concept.categoryId &&
                        categorySource.current !== "manual" &&
                        categorySource.current !== "restored"
                      )
                        updateCategory(candidate.concept.categoryId, "product");
                      setProductSuggestions([]);
                      amountRef.current?.focus();
                      setFocused("amount");
                    }}
                  >
                    <Search size={13} />
                    <span>{candidate.matchedName}</span>
                    <small>
                      {candidate.concept.name} · {candidate.variant.name}
                    </small>
                  </button>
                ))}
              </div>
            )}
            {selectedProductVariantId && (
              <small className="selected-product-label">商品を選択済み</small>
            )}
          </div>
          <div className="entry-input-wrap amount-wrap">
            <span>¥</span>
            <input
              ref={amountRef}
              value={itemAmount}
              inputMode="numeric"
              onChange={(e) => {
                markUnsaved();
                setItemAmount(e.target.value.replace(/\D/g, ""));
              }}
              onFocus={() => setFocused("amount")}
              onBlur={() => window.setTimeout(() => setFocused(null), 160)}
              onKeyDown={onAmountKey}
              onCompositionStart={() => {
                composing.current = true;
              }}
              onCompositionEnd={() => {
                composing.current = false;
              }}
              onPaste={pasteRows}
              placeholder="金額"
              aria-label="金額"
            />
          </div>
          <select
            aria-label="新しい品目のカテゴリ"
            value={categoryId}
            onChange={(e) => {
              markUnsaved();
              updateCategory(e.target.value, "manual");
            }}
          >
            <option value="">未分類</option>
            {enabledCategories.map((category) => (
              <option key={category.id} value={category.id}>
                {category.name}
              </option>
            ))}
          </select>
          <button
            type="button"
            className="button button-add"
            disabled={!itemName.trim() || !itemAmount.trim()}
            onClick={() => addItem()}
          >
            <Plus size={16} />
            <span>品目を追加</span>
          </button>
        </div>
        <div className="entry-hint">
          <span>
            <kbd>Enter</kbd>で品名から金額へ、もう一度で追加
          </span>
          <span>表計算の行を貼り付けできます</span>
        </div>
      </section>
      {householdOutbox.length > 0 && (
        <section className="panel purchase-outbox">
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
      <div className="composer-bottom">
        <div>
          <div className="total-label">
            合計 <strong>{yen(itemTotal)}</strong>
          </div>
          <span>
            {items.length}品目
            {hasDraftContent && (
              <>
                {" · "}
                {savedAt
                  ? "下書き保存 " +
                    savedAt.toLocaleTimeString("ja-JP", { hour: "2-digit", minute: "2-digit" })
                  : "下書き保存中"}
              </>
            )}
          </span>
        </div>
        <div className="composer-actions">
          {undoItems && (
            <button type="button" className="button button-soft" onClick={undo}>
              <Undo2 size={16} />
              元に戻す
            </button>
          )}
          <button
            className="button button-primary"
            disabled={
              pending || !outbox.ready || Boolean(outbox.storageError) || items.length === 0
            }
          >
            {pending ? "保存中" : "保存して送信"}
            <Check size={16} />
          </button>
        </div>
      </div>
      {outbox.storageError && <InlineError message={outbox.storageError} />}
      {message && <InlineError message={message} />}
    </form>
  );
}
