import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type ClipboardEvent,
  type KeyboardEvent,
} from "react";
import { Plus, ReceiptJapaneseYen, Search, X } from "lucide-react";
import { z } from "zod";
import {
  readGraphql,
  type Household,
  type ProductSuggestion,
  type Purchase,
} from "../../../shared/api";
import { productSuggestionsQuery } from "../../../entities/product";
import type { PurchaseDraftItem } from "../../../shared/lib";
import { emptyPurchaseItemsValue, type PurchaseItemsValue } from "../model/purchase-items";
import { EditPurchaseItemAllocations } from "./edit-purchase-item-allocations";

const purchaseItemSchema = z.object({
  name: z.string().trim().min(1),
  amount: z.string().regex(/^[1-9]\d*$/),
});
type CategorySource = "default" | "history" | "product" | "manual" | "restored";

export function EditPurchaseItems({
  householdId,
  merchant,
  household,
  history,
  value,
  resetToken,
  copiedPurchase,
  classNames,
  undoToken,
  onChange,
  onDirty,
  onUndoAvailabilityChange,
}: {
  householdId: string;
  merchant: string;
  household: Household;
  history: Purchase[];
  value: PurchaseItemsValue;
  resetToken: number;
  copiedPurchase: Purchase | null;
  classNames: {
    itemHeading: string;
    itemTable: string;
    itemList: string;
    entryForm: string;
    entryHint: string;
  };
  undoToken: number;
  onChange: (value: PurchaseItemsValue) => void;
  onDirty: () => void;
  onUndoAvailabilityChange: (available: boolean) => void;
}) {
  const [draft, setDraft] = useState(value);
  const draftRef = useRef(draft);
  const [selectedProductVariantId, setSelectedProductVariantId] = useState("");
  const [productSuggestions, setProductSuggestions] = useState<ProductSuggestion[]>([]);
  const [undoItems, setUndoItems] = useState<PurchaseDraftItem[] | null>(null);
  const [focused, setFocused] = useState<"name" | "amount" | null>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const amountRef = useRef<HTMLInputElement>(null);
  const itemGridRef = useRef<HTMLDivElement>(null);
  const categorySource = useRef<CategorySource>(value.categoryId ? "restored" : "default");
  const composing = useRef(false);
  const lastResetToken = useRef(resetToken);
  const lastCopiedPurchase = useRef<Purchase | null>(null);
  const lastUndoToken = useRef(undoToken);
  const enabledCategories = household.categories.filter((category) => !category.archived);
  const defaultAllocations = household.defaultAllocation?.length
    ? household.defaultAllocation
    : household.pools.map((pool) => ({ poolId: pool.id, weight: pool.defaultShareWeight || 1 }));
  const isEnabledCategory = (id: string | null | undefined): id is string =>
    Boolean(id && enabledCategories.some((category) => category.id === id));

  const update = (next: PurchaseItemsValue) => {
    draftRef.current = next;
    setDraft(next);
    onChange(next);
  };
  const patchDraft = (patch: Partial<PurchaseItemsValue>) =>
    update({ ...draftRef.current, ...patch });
  const updateCategory = (id: string, source: CategorySource) => {
    categorySource.current = source;
    patchDraft({ categoryId: id });
  };
  const changeItemName = (name: string) => {
    setSelectedProductVariantId("");
    patchDraft({
      itemName: name,
      categoryId:
        categorySource.current === "history" || categorySource.current === "product"
          ? ""
          : draftRef.current.categoryId,
    });
    if (categorySource.current === "history" || categorySource.current === "product")
      categorySource.current = "default";
  };
  const markDirty = () => onDirty();
  const replaceItems = (items: PurchaseDraftItem[]) => patchDraft({ items });

  useEffect(() => {
    if (lastResetToken.current === resetToken) return;
    lastResetToken.current = resetToken;
    const empty = emptyPurchaseItemsValue();
    categorySource.current = "default";
    setSelectedProductVariantId("");
    setUndoItems(null);
    setProductSuggestions([]);
    update(empty);
  }, [resetToken]);

  useEffect(() => {
    onUndoAvailabilityChange(Boolean(undoItems));
  }, [undoItems, onUndoAvailabilityChange]);

  useEffect(() => {
    if (lastUndoToken.current === undoToken) return;
    lastUndoToken.current = undoToken;
    if (!undoItems) return;
    markDirty();
    replaceItems(undoItems);
    setUndoItems(null);
  }, [undoToken]);

  useEffect(() => {
    if (!copiedPurchase || lastCopiedPurchase.current === copiedPurchase) return;
    lastCopiedPurchase.current = copiedPurchase;
    markDirty();
    const items = copiedPurchase.items.map((item) => ({
      key: crypto.randomUUID(),
      name: item.name,
      amount: String(item.amountYen),
      categoryId: isEnabledCategory(item.categoryId) ? (item.categoryId ?? "") : "",
      productVariantId: item.productVariantId ?? undefined,
      allocations: item.allocations.map(({ poolId, weight }) => ({ poolId, weight })),
      ...(item.quantityNumerator && item.quantityDenominator
        ? {
            quantity: `${item.quantityNumerator}/${item.quantityDenominator}`,
            quantityUnit: item.quantityUnit ?? "",
          }
        : {}),
    }));
    setUndoItems(draft.items);
    replaceItems(items);
  }, [copiedPurchase]);

  useEffect(() => {
    const query = draft.itemName.trim();
    if (query.length < 2) {
      setProductSuggestions([]);
      return;
    }
    let active = true;
    const timer = window.setTimeout(() => {
      void readGraphql<{ productSuggestions: ProductSuggestion[] }>(productSuggestionsQuery, {
        householdId,
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
  }, [householdId, draft.itemName, merchant]);

  useEffect(() => {
    if (
      !draft.itemName.trim() ||
      categorySource.current === "manual" ||
      categorySource.current === "restored" ||
      categorySource.current === "product"
    )
      return;
    const match = history
      .flatMap((purchase) => purchase.items)
      .find((item) => item.name.toLocaleLowerCase() === draft.itemName.trim().toLocaleLowerCase());
    if (isEnabledCategory(match?.categoryId)) {
      categorySource.current = "history";
      update({ ...draft, categoryId: match.categoryId });
    } else if (categorySource.current === "history") {
      categorySource.current = "default";
      update({ ...draft, categoryId: "" });
    }
  }, [draft.itemName, history]);

  const suggestions = useMemo(() => {
    const seen = new Set<string>();
    return history
      .flatMap((purchase) => purchase.items.map((item) => item.name))
      .filter(
        (name) =>
          name.toLocaleLowerCase().includes(draft.itemName.trim().toLocaleLowerCase()) &&
          !seen.has(name) &&
          Boolean(seen.add(name)),
      )
      .slice(0, 4);
  }, [history, draft.itemName]);

  const addItem = () => {
    const parsed = purchaseItemSchema.safeParse({ name: draft.itemName, amount: draft.itemAmount });
    if (!parsed.success) return;
    markDirty();
    const item: PurchaseDraftItem = {
      key: crypto.randomUUID(),
      ...parsed.data,
      categoryId: draft.categoryId,
      ...(selectedProductVariantId ? { productVariantId: selectedProductVariantId } : {}),
      allocations: [],
    };
    setUndoItems(draft.items);
    update({
      ...draft,
      items: [...draft.items, item],
      itemName: "",
      itemAmount: "",
      categoryId: "",
    });
    categorySource.current = "default";
    setSelectedProductVariantId("");
    setProductSuggestions([]);
    setFocused("name");
    nameRef.current?.focus();
  };
  const onNameKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (
      event.key === "Enter" &&
      !event.nativeEvent.isComposing &&
      !composing.current &&
      draft.itemName.trim()
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
      } else if (row + 1 < draft.items.length) {
        itemGridRef.current
          ?.querySelector<HTMLInputElement>(`[data-grid-row="${row + 1}"][data-grid-column="name"]`)
          ?.focus();
      } else {
        nameRef.current?.focus();
      }
      return;
    }
    if (event.key === "ArrowUp" || event.key === "ArrowDown")
      targetRow += event.key === "ArrowUp" ? -1 : 1;
    else if (event.key === "ArrowLeft" && column === "amount") {
      if (event.currentTarget.selectionStart !== 0 || event.currentTarget.selectionEnd !== 0)
        return;
      targetColumn = "name";
    } else if (event.key === "ArrowRight" && column === "name") {
      if (
        event.currentTarget.selectionStart !== event.currentTarget.value.length ||
        event.currentTarget.selectionEnd !== event.currentTarget.value.length
      )
        return;
      targetColumn = "amount";
    } else return;
    event.preventDefault();
    if (targetRow === draft.items.length && event.key === "ArrowDown") {
      (targetColumn === "name" ? nameRef : amountRef).current?.focus();
      return;
    }
    if (targetRow < 0 || targetRow >= draft.items.length) return;
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
    markDirty();
    setUndoItems(draft.items);
    replaceItems([
      ...draft.items,
      ...parsed.map(([name, amount]) => {
        const historyCategoryId = history
          .flatMap((purchase) => purchase.items)
          .find((item) => item.name.toLocaleLowerCase() === name.toLocaleLowerCase())?.categoryId;
        return {
          key: crypto.randomUUID(),
          name,
          amount: amount.replace(/[¥,]/g, ""),
          categoryId:
            categorySource.current === "manual" || categorySource.current === "restored"
              ? draft.categoryId
              : isEnabledCategory(historyCategoryId)
                ? historyCategoryId
                : "",
          allocations: [],
        };
      }),
    ]);
  };
  const patchItem = (key: string, patch: Partial<PurchaseDraftItem>) => {
    markDirty();
    replaceItems(draft.items.map((item) => (item.key === key ? { ...item, ...patch } : item)));
  };
  const removeItem = (key: string) => {
    if (!draft.items.some((item) => item.key === key)) return;
    markDirty();
    setUndoItems(draft.items);
    replaceItems(draft.items.filter((item) => item.key !== key));
  };
  return (
    <>
      <div className={`panel-heading entry-heading ${classNames.itemHeading}`}>
        <div>
          <span className="eyebrow">品目</span>
          <h2>買ったもの</h2>
        </div>
        <span className="entry-default">
          負担 <b>{household.pools.map((pool) => pool.name.slice(0, 1)).join(" / ") || "標準"}</b>
        </span>
      </div>
      <div className={`item-table-head ${classNames.itemTable}`}>
        <span>品名</span>
        <span>金額</span>
        <span>カテゴリ</span>
        <span>負担</span>
        <span />
      </div>
      <div className={`item-list ${classNames.itemList}`} ref={itemGridRef}>
        {draft.items.map((item, index) => (
          <div className="item-entry" key={item.key}>
            <div className="item-edit-row">
              <span className="item-index">{String(index + 1).padStart(2, "0")}</span>
              <input
                aria-label="品名"
                data-grid-row={index}
                data-grid-column="name"
                value={item.name}
                onChange={(event) => patchItem(item.key, { name: event.target.value })}
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
                  onChange={(event) =>
                    patchItem(item.key, { amount: event.target.value.replace(/\D/g, "") })
                  }
                  onKeyDown={(event) => onItemGridKey(event, index, "amount")}
                />
              </div>
              <select
                aria-label="カテゴリ"
                value={item.categoryId}
                onChange={(event) => patchItem(item.key, { categoryId: event.target.value })}
              >
                <option value="">未分類</option>
                {enabledCategories.map((category) => (
                  <option key={category.id} value={category.id}>
                    {category.name}
                  </option>
                ))}
              </select>
              <EditPurchaseItemAllocations
                allocations={item.allocations}
                defaultAllocations={defaultAllocations}
                pools={household.pools}
                onChange={(allocations) => patchItem(item.key, { allocations })}
              />
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
        {draft.items.length === 0 && (
          <div className="empty-items">
            <ReceiptJapaneseYen size={24} />
            <span>品名と金額を入力して追加</span>
          </div>
        )}
      </div>
      <div className={`entry-form ${focused ? "entry-focused" : ""} ${classNames.entryForm}`}>
        <div className="entry-input-wrap name-wrap">
          <input
            ref={nameRef}
            value={draft.itemName}
            onChange={(event) => {
              markDirty();
              changeItemName(event.target.value);
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
                  onMouseDown={(event) => event.preventDefault()}
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
                    changeItemName(candidate.matchedName || draft.itemName.trim());
                    setSelectedProductVariantId(candidate.variant.id);
                    if (
                      isEnabledCategory(candidate.concept.categoryId) &&
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
            value={draft.itemAmount}
            inputMode="numeric"
            onChange={(event) => {
              markDirty();
              update({ ...draft, itemAmount: event.target.value.replace(/\D/g, "") });
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
          value={draft.categoryId}
          onChange={(event) => {
            markDirty();
            updateCategory(event.target.value, "manual");
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
          disabled={!draft.itemName.trim() || !draft.itemAmount.trim()}
          onClick={addItem}
        >
          <Plus size={16} />
          <span>品目を追加</span>
        </button>
      </div>
      <div className={`entry-hint ${classNames.entryHint}`}>
        <span>
          <kbd>Enter</kbd>で品名から金額へ、もう一度で追加
        </span>
        <span>表計算の行を貼り付けできます</span>
      </div>
    </>
  );
}
