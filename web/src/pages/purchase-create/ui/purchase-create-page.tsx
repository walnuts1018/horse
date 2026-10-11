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
  createPurchaseMutation,
  makeIdempotencyKey,
  purchasesQuery,
  readGraphql,
  type Connection,
  type Household,
  type Purchase,
} from "../../../shared/api";
import { InlineError } from "../../../shared/ui";

type DraftItem = {
  key: string;
  name: string;
  amount: string;
  categoryId: string;
  allocations: Array<{ poolId: string; weight: number }>;
};
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
  onSaved,
}: {
  household: Household;
  onSaved: () => void;
}) {
  const storageKey = `horse:draft:${household.id}`;
  const initial = useMemo(() => {
    try {
      return JSON.parse(localStorage.getItem(storageKey) ?? "{}") as {
        merchant?: string;
        occurredAt?: string;
        paymentAccountId?: string;
        items?: DraftItem[];
      };
    } catch {
      return {};
    }
  }, [storageKey]);
  const [merchant, setMerchant] = useState(initial.merchant ?? "");
  const [occurredAt, setOccurredAt] = useState(initial.occurredAt ?? localDateTime());
  const [paymentAccountId, setPaymentAccountId] = useState(
    initial.paymentAccountId ?? household.accounts[0]?.id ?? "",
  );
  const [items, setItems] = useState<DraftItem[]>(initial.items ?? []);
  const [itemName, setItemName] = useState("");
  const [itemAmount, setItemAmount] = useState("");
  const [categoryId, setCategoryId] = useState(
    household.categories.find((item) => !item.archived)?.id ?? "",
  );
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");
  const [undoItems, setUndoItems] = useState<DraftItem[] | null>(null);
  const [savedAt, setSavedAt] = useState<Date | null>(null);
  const [history, setHistory] = useState<Purchase[]>([]);
  const [focused, setFocused] = useState<"name" | "amount" | null>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const amountRef = useRef<HTMLInputElement>(null);
  const composing = useRef(false);
  const enabledCategories = household.categories.filter((category) => !category.archived);
  const defaultAllocations = household.defaultAllocation?.length
    ? household.defaultAllocation
    : household.pools.map((pool) => ({ poolId: pool.id, weight: pool.defaultShareWeight || 1 }));

  useEffect(() => {
    const timer = window.setTimeout(() => {
      localStorage.setItem(
        storageKey,
        JSON.stringify({ merchant, occurredAt, paymentAccountId, items }),
      );
      setSavedAt(new Date());
    }, 450);
    return () => window.clearTimeout(timer);
  }, [storageKey, merchant, occurredAt, paymentAccountId, items]);
  useEffect(() => {
    void readGraphql<{ purchases: Connection<Purchase> }>(purchasesQuery, {
      householdId: household.id,
      first: 80,
    })
      .then((data) => setHistory(data.purchases.edges.map((edge) => edge.node)))
      .catch(() => {});
  }, [household.id]);
  useEffect(() => {
    if (!itemName.trim()) return;
    const match = history
      .flatMap((purchase) => purchase.items)
      .find((item) => item.name.toLocaleLowerCase() === itemName.trim().toLocaleLowerCase());
    if (match?.categoryId) setCategoryId(match.categoryId);
  }, [itemName, history]);
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
  const total = sum(items.map((item) => Number(item.amount) || 0));

  const addItem = (name = itemName, amount = itemAmount) => {
    const parsed = purchaseItemSchema.safeParse({ name, amount });
    if (!parsed.success) return false;
    const item: DraftItem = {
      key: crypto.randomUUID(),
      ...parsed.data,
      categoryId,
      allocations: [],
    };
    setUndoItems(items);
    setItems((current) => [...current, item]);
    setItemName("");
    setItemAmount("");
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
    const categoryByName = new Map(
      history.flatMap((purchase) =>
        purchase.items.map((item) => [item.name, item.categoryId] as const),
      ),
    );
    setUndoItems(items);
    setItems((current) => [
      ...current,
      ...parsed.map(([name, amount]) => ({
        key: crypto.randomUUID(),
        name,
        amount: amount.replace(/[¥,]/g, ""),
        categoryId: categoryByName.get(name) || categoryId,
        allocations: [],
      })),
    ]);
  };
  const removeItem = (key: string) => {
    if (!items.some((item) => item.key === key)) return;
    setUndoItems(items);
    setItems((current) => current.filter((item) => item.key !== key));
  };
  const undo = () => {
    if (undoItems) {
      setItems(undoItems);
      setUndoItems(null);
    }
  };
  const patchItem = (key: string, patch: Partial<DraftItem>) =>
    setItems((current) => current.map((item) => (item.key === key ? { ...item, ...patch } : item)));
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!items.length) {
      setMessage("品目を1つ以上追加してください");
      return;
    }
    if (!household.accounts.length) {
      setMessage("設定で決済元を登録してください");
      return;
    }
    setPending(true);
    setMessage("");
    try {
      const data = await readGraphql<{ createPurchase: Purchase }>(createPurchaseMutation, {
        input: {
          idempotencyKey: makeIdempotencyKey(),
          householdId: household.id,
          occurredAt: new Date(occurredAt).toISOString(),
          merchant: merchant.trim(),
          paymentAccountId,
          items: items.map((item) => ({
            name: item.name,
            amountYen: Number(item.amount),
            categoryId: item.categoryId || null,
            allocations: item.allocations.length
              ? item.allocations.filter((allocation) => allocation.weight > 0)
              : null,
          })),
        },
      });
      localStorage.removeItem(storageKey);
      void data;
      onSaved();
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
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
          <button type="button" className="back-link" onClick={() => window.history.back()}>
            <ChevronLeft size={16} />
            戻る
          </button>
          <h1>買い物を追加</h1>
          <p>同じ買い物の品目を続けて入力</p>
        </div>
        <span className="save-status">
          <span className="status-dot" />
          {savedAt ? "下書きを保存" : "保存中"}
        </span>
      </div>
      <section className="panel purchase-meta-form">
        <div className="field-group">
          <label htmlFor="merchant">店名</label>
          <input
            id="merchant"
            placeholder="例 スーパーいちば"
            value={merchant}
            onChange={(e) => setMerchant(e.target.value)}
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
            onChange={(e) => setOccurredAt(e.target.value)}
          />
        </div>
        <div className="field-group">
          <label htmlFor="payment">決済元</label>
          <select
            id="payment"
            value={paymentAccountId}
            onChange={(e) => setPaymentAccountId(e.target.value)}
          >
            <option value="">決済元を選択</option>
            {household.accounts.map((account) => (
              <option key={account.id} value={account.id}>
                {account.name}
              </option>
            ))}
          </select>
        </div>
      </section>
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
        <div className="item-list">
          {items.map((item, index) => (
            <div className="item-edit-row" key={item.key}>
              <span className="item-index">{String(index + 1).padStart(2, "0")}</span>
              <input
                aria-label="品名"
                value={item.name}
                onChange={(e) => patchItem(item.key, { name: e.target.value })}
              />
              <div className="yen-input">
                <span>¥</span>
                <input
                  aria-label="金額"
                  inputMode="numeric"
                  value={item.amount}
                  onChange={(e) =>
                    patchItem(item.key, { amount: e.target.value.replace(/\D/g, "") })
                  }
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
              onChange={(e) => setItemName(e.target.value)}
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
            {focused === "name" && suggestions.length > 0 && (
              <div className="suggestions">
                {suggestions.map((name) => (
                  <button
                    type="button"
                    key={name}
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => {
                      setItemName(name);
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
          </div>
          <div className="entry-input-wrap amount-wrap">
            <span>¥</span>
            <input
              ref={amountRef}
              value={itemAmount}
              inputMode="numeric"
              onChange={(e) => setItemAmount(e.target.value.replace(/\D/g, ""))}
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
            onChange={(e) => setCategoryId(e.target.value)}
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
      <div className="composer-bottom">
        <div>
          <div className="total-label">
            合計 <strong>{yen(total)}</strong>
          </div>
          <span>
            {items.length}品目 ·{" "}
            {savedAt
              ? `保存 ${savedAt.toLocaleTimeString("ja-JP", { hour: "2-digit", minute: "2-digit" })}`
              : "下書き保存中"}
          </span>
        </div>
        <div className="composer-actions">
          {undoItems && (
            <button type="button" className="button button-soft" onClick={undo}>
              <Undo2 size={16} />
              元に戻す
            </button>
          )}
          <button className="button button-primary" disabled={pending || items.length === 0}>
            {pending ? "保存中" : "帳簿に追加"}
            <Check size={16} />
          </button>
        </div>
      </div>
      {message && <InlineError message={message} />}
    </form>
  );
}
