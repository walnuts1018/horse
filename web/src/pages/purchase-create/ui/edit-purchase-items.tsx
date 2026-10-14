import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type ClipboardEvent,
  type KeyboardEvent,
  type Ref,
} from "react";
import { z } from "zod";

import type { PurchaseDraftItem } from "../../../shared/lib";

import { readProductSuggestions, type ProductSuggestion } from "../../../entities/product";
import { type Household, type Purchase } from "../../../shared/api";
import { parsePastedPurchaseItems } from "../model/pasted-purchase-items";
import { productSuggestionsForQuery } from "../model/product-suggestions";
import { emptyPurchaseItemsValue, type PurchaseItemsValue } from "../model/purchase-items";
import { PurchaseItemEntry } from "./purchase-item-entry";
import { PurchaseItemsGrid } from "./purchase-items-grid";

const purchaseItemSchema = z.object({
  name: z.string().trim().min(1),
  amount: z.string().regex(/^[1-9]\d*$/),
});

export type EditPurchaseItemsCommands = {
  reset: () => void;
  undo: () => void;
  copy: (purchase: Purchase) => void;
  prepareForSubmit: () => PurchaseDraftItem[] | null;
};

export function EditPurchaseItems({
  householdId,
  merchant,
  household,
  history,
  value,
  commandsRef,
  classNames,
  onChange,
  onDirty,
  onUndoAvailabilityChange,
}: {
  householdId: string;
  merchant: string;
  household: Household;
  history: Purchase[];
  value: PurchaseItemsValue;
  commandsRef: Ref<EditPurchaseItemsCommands>;
  classNames: {
    itemHeading: string;
    itemTable: string;
    itemList: string;
    entryForm: string;
    entryHint: string;
    entryInputTarget: string;
  };
  onChange: (value: PurchaseItemsValue) => void;
  onDirty: () => void;
  onUndoAvailabilityChange: (available: boolean) => void;
}) {
  const [draft, setDraft] = useState(value);
  const draftRef = useRef(draft);
  const onChangeRef = useRef(onChange);
  const selectedProductVariantId = draft.selectedProductVariantId;
  const [productSuggestionResponse, setProductSuggestionResponse] = useState<{
    query: string;
    suggestions: ProductSuggestion[];
  } | null>(null);
  const productSuggestions = productSuggestionsForQuery(draft.itemName, productSuggestionResponse);
  const [activeSuggestionIndex, setActiveSuggestionIndex] = useState(-1);
  const [suggestionsDismissed, setSuggestionsDismissed] = useState(false);
  const [undoItems, setUndoItems] = useState<PurchaseDraftItem[] | null>(null);
  const [focused, setFocused] = useState<"name" | "amount" | null>(null);
  const [invalidEntryField, setInvalidEntryField] = useState<"name" | "amount" | null>(null);
  const nameRef = useRef<HTMLInputElement>(null);
  const amountRef = useRef<HTMLInputElement>(null);
  const itemGridRef = useRef<HTMLDivElement>(null);
  const composing = useRef(false);
  const enabledCategories = useMemo(
    () => household.categories.filter((category) => !category.archived),
    [household.categories],
  );
  const defaultAllocations = useMemo(
    () =>
      household.defaultAllocation.length > 0
        ? household.defaultAllocation
        : household.pools.map((pool) => ({
            poolId: pool.id,
            weight: pool.defaultShareWeight || 1,
          })),
    [household.defaultAllocation, household.pools],
  );
  const isEnabledCategory = useCallback(
    (id: string | null | undefined): id is string =>
      Boolean(id && enabledCategories.some((category) => category.id === id)),
    [enabledCategories],
  );

  // 実効カテゴリ：ユーザー明示選択 > 自動推測 > 空
  const suggestedCategoryFromHistory = useMemo(() => {
    const query = draft.itemName.trim();
    if (!query || draft.categoryId !== "") return null;
    const match = history
      .flatMap((purchase) => purchase.items)
      .find((item) => item.name.toLocaleLowerCase() === query.toLocaleLowerCase());
    if (match && isEnabledCategory(match.categoryId)) return match.categoryId;
    return null;
  }, [draft.categoryId, draft.itemName, history, isEnabledCategory]);

  const effectiveCategoryId = draft.categoryId || suggestedCategoryFromHistory || "";

  // コールバックはref経由で参照し、副作用の依存が毎回変わって延々と再実行されるのを避ける
  useEffect(() => {
    onChangeRef.current = onChange;
  });

  const update = useCallback((next: PurchaseItemsValue) => {
    draftRef.current = next;
    setDraft(next);
    onChangeRef.current(next);
  }, []);
  const patchDraft = useCallback(
    (patch: Partial<PurchaseItemsValue>) => update({ ...draftRef.current, ...patch }),
    [update],
  );
  const updateCategory = useCallback((id: string) => patchDraft({ categoryId: id }), [patchDraft]);
  const changeItemName = useCallback(
    (name: string) => {
      if (name.trim() !== draftRef.current.itemName.trim()) {
        setProductSuggestionResponse(null);
      }
      setActiveSuggestionIndex(-1);
      setSuggestionsDismissed(false);
      patchDraft({ itemName: name, selectedProductVariantId: "" });
    },
    [patchDraft],
  );
  const markDirty = useCallback(() => onDirty(), [onDirty]);
  const replaceItems = useCallback(
    (items: PurchaseDraftItem[]) => patchDraft({ items }),
    [patchDraft],
  );
  const rememberUndo = useCallback(
    (items: PurchaseDraftItem[] | null) => {
      setUndoItems(items);
      onUndoAvailabilityChange(Boolean(items));
    },
    [onUndoAvailabilityChange],
  );
  const reset = useCallback(() => {
    const empty = emptyPurchaseItemsValue();
    rememberUndo(null);
    setProductSuggestionResponse(null);
    update(empty);
  }, [rememberUndo, update]);
  const undo = useCallback(() => {
    const previousItems = undoItems;
    if (!previousItems) return;
    markDirty();
    replaceItems(previousItems);
    rememberUndo(null);
  }, [undoItems, markDirty, replaceItems, rememberUndo]);
  const copy = useCallback(
    (purchase: Purchase) => {
      markDirty();
      const items = purchase.items.map((item) => ({
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
      rememberUndo(draftRef.current.items);
      update({ ...draftRef.current, items, categoryId: "" });
    },
    [markDirty, isEnabledCategory, rememberUndo, update],
  );
  const prepareForSubmit = useCallback(() => {
    const current = draftRef.current;
    if (!current.itemName && !current.itemAmount) {
      if (current.items.length) {
        setInvalidEntryField(null);
        return current.items;
      }
      setInvalidEntryField("name");
      setFocused("name");
      nameRef.current?.focus();
      return null;
    }
    const parsed = purchaseItemSchema.safeParse({
      name: current.itemName,
      amount: current.itemAmount,
    });
    if (!parsed.success) {
      const field = current.itemName.trim() ? "amount" : "name";
      setInvalidEntryField(field);
      setFocused(field);
      (field === "name" ? nameRef : amountRef).current?.focus();
      return null;
    }
    setInvalidEntryField(null);
    return [
      ...current.items,
      {
        key: crypto.randomUUID(),
        ...parsed.data,
        categoryId: effectiveCategoryId,
        ...(current.selectedProductVariantId
          ? { productVariantId: current.selectedProductVariantId }
          : {}),
        allocations: [],
      },
    ];
  }, [effectiveCategoryId]);
  useImperativeHandle(commandsRef, () => ({ reset, undo, copy, prepareForSubmit }), [
    reset,
    undo,
    copy,
    prepareForSubmit,
  ]);

  useEffect(() => {
    const query = draft.itemName.trim();
    if (query.length < 2) {
      queueMicrotask(() => setProductSuggestionResponse(null));
      return;
    }
    let active = true;
    const timer = window.setTimeout(() => {
      void readProductSuggestions(householdId, query, merchant.trim() || null, 6)
        .then((result) => {
          if (!active) return;
          setProductSuggestionResponse({ query, suggestions: result });
          setActiveSuggestionIndex(-1);
        })
        .catch(() => {
          if (!active) return;
          setProductSuggestionResponse({ query, suggestions: [] });
          setActiveSuggestionIndex(-1);
        });
    }, 220);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [householdId, draft.itemName, merchant]);

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
      categoryId: effectiveCategoryId,
      ...(selectedProductVariantId ? { productVariantId: selectedProductVariantId } : {}),
      allocations: [],
    };
    rememberUndo(draft.items);
    update({
      ...draft,
      items: [...draft.items, item],
      itemName: "",
      itemAmount: "",
      selectedProductVariantId: "",
      categoryId: "",
    });
    setProductSuggestionResponse(null);
    setInvalidEntryField(null);
    setFocused("name");
    nameRef.current?.focus();
  };
  const selectHistorySuggestion = (name: string) => {
    changeItemName(name);
    amountRef.current?.focus();
    setFocused("amount");
  };
  const selectProductSuggestion = (candidate: ProductSuggestion) => {
    if (!productSuggestions.includes(candidate)) return;
    markDirty();
    patchDraft({
      itemName: candidate.matchedName || draft.itemName.trim(),
      selectedProductVariantId: candidate.variant.id,
      ...(isEnabledCategory(candidate.concept.categoryId) && !draft.categoryId
        ? { categoryId: candidate.concept.categoryId }
        : {}),
    });
    setProductSuggestionResponse(null);
    amountRef.current?.focus();
    setFocused("amount");
  };
  const visibleSuggestionCount =
    productSuggestions.length > 0 && !selectedProductVariantId
      ? productSuggestions.length
      : suggestions.length;
  const onNameKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (
      (event.key === "Escape" || event.key === "ArrowDown" || event.key === "ArrowUp") &&
      (event.nativeEvent.isComposing || composing.current)
    )
      return;
    if (event.key === "Escape" && visibleSuggestionCount > 0) {
      event.preventDefault();
      setSuggestionsDismissed(true);
      setActiveSuggestionIndex(-1);
      return;
    }
    if ((event.key === "ArrowDown" || event.key === "ArrowUp") && visibleSuggestionCount > 0) {
      event.preventDefault();
      setSuggestionsDismissed(false);
      setActiveSuggestionIndex((index) => {
        if (event.key === "ArrowDown") return (index + 1) % visibleSuggestionCount;
        return index <= 0 ? visibleSuggestionCount - 1 : index - 1;
      });
      return;
    }
    if (
      event.key === "Enter" &&
      activeSuggestionIndex >= 0 &&
      !event.nativeEvent.isComposing &&
      !composing.current
    ) {
      event.preventDefault();
      if (productSuggestions.length > 0 && !selectedProductVariantId) {
        const candidate = productSuggestions.at(activeSuggestionIndex);
        if (candidate) selectProductSuggestion(candidate);
      } else {
        const suggestion = suggestions[activeSuggestionIndex];
        if (suggestion) selectHistorySuggestion(suggestion);
      }
      return;
    }
    if (event.key === "Enter" && !event.nativeEvent.isComposing && !composing.current) {
      event.preventDefault();
      if (!draft.itemName.trim()) return;
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
    const parsed = parsePastedPurchaseItems(text);
    if (!parsed.structured) return;
    event.preventDefault();
    if (parsed.invalidSyntax) {
      window.alert("貼り付けたCSVの引用符を解析できませんでした。内容を確認してください。");
      return;
    }
    if (parsed.invalidRowCount > 0) {
      const message = `${parsed.invalidRowCount}行を品目として解析できませんでした。解析できた行だけ追加しますか？`;
      if (!parsed.items.length || !window.confirm(message)) return;
    }
    if (!parsed.items.length) return;
    markDirty();
    rememberUndo(draft.items);
    replaceItems([
      ...draft.items,
      ...parsed.items.map(({ name, amount }) => {
        const historyCategoryId = history
          .flatMap((purchase) => purchase.items)
          .find((item) => item.name.toLocaleLowerCase() === name.toLocaleLowerCase())?.categoryId;
        return {
          key: crypto.randomUUID(),
          name,
          amount,
          categoryId: draft.categoryId
            ? effectiveCategoryId
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
    rememberUndo(draft.items);
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
      <PurchaseItemsGrid
        items={draft.items}
        household={household}
        defaultAllocations={defaultAllocations}
        enabledCategories={enabledCategories}
        classNames={{ itemTable: classNames.itemTable, itemList: classNames.itemList }}
        gridRef={itemGridRef}
        onKeyDown={onItemGridKey}
        onPatch={patchItem}
        onRemove={removeItem}
      />
      <PurchaseItemEntry
        itemName={draft.itemName}
        itemAmount={draft.itemAmount}
        categoryId={effectiveCategoryId}
        categories={enabledCategories}
        suggestions={suggestions}
        productSuggestions={productSuggestions}
        selectedProductVariantId={selectedProductVariantId}
        activeSuggestionIndex={activeSuggestionIndex}
        suggestionsDismissed={suggestionsDismissed}
        focused={focused}
        classNames={{
          entryForm: classNames.entryForm,
          entryHint: classNames.entryHint,
          entryInputTarget: classNames.entryInputTarget,
        }}
        nameRef={nameRef}
        amountRef={amountRef}
        onNameChange={(name) => {
          markDirty();
          setInvalidEntryField(null);
          changeItemName(name);
        }}
        onNameFocus={() => {
          setFocused("name");
          setSuggestionsDismissed(false);
        }}
        onNameBlur={() => window.setTimeout(() => setFocused(null), 160)}
        onNameKey={onNameKey}
        onPaste={pasteRows}
        onCompositionStart={() => {
          composing.current = true;
        }}
        onCompositionEnd={() => {
          composing.current = false;
        }}
        onSelectHistorySuggestion={selectHistorySuggestion}
        onSelectProductSuggestion={selectProductSuggestion}
        onSuggestionHover={setActiveSuggestionIndex}
        onAmountChange={(amount) => {
          markDirty();
          setInvalidEntryField(null);
          update({ ...draft, itemAmount: amount.replace(/\D/g, "") });
        }}
        onAmountFocus={() => setFocused("amount")}
        onAmountBlur={() => window.setTimeout(() => setFocused(null), 160)}
        onAmountKey={onAmountKey}
        onCategoryChange={(id) => {
          markDirty();
          updateCategory(id);
        }}
        onAdd={addItem}
        invalidEntryField={invalidEntryField}
      />
    </>
  );
}
