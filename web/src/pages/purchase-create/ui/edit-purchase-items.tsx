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
import {
  purchaseEntrySuggestions,
  productSuggestionsForQuery,
  type ProductSuggestionResponse,
} from "../model/product-suggestions";
import {
  emptyPurchaseEditHistory,
  recordPurchaseEdit,
  redoPurchaseEdit,
  undoPurchaseEdit,
  type PurchaseEditHistory,
  type PurchaseEditSnapshot,
} from "../model/purchase-edit-history";
import { emptyPurchaseItemsValue, type PurchaseItemsValue } from "../model/purchase-items";
import { PurchaseItemEntry } from "./purchase-item-entry";
import { PurchaseItemsGrid } from "./purchase-items-grid";

import styles from "./edit-purchase-items.module.css";

const purchaseItemSchema = z.object({
  name: z.string().trim().min(1),
  amount: z.string().regex(/^[1-9]\d*$/),
});

export type EditPurchaseItemsCommands = {
  reset: () => void;
  undo: () => PurchaseEditSnapshot | null;
  redo: () => PurchaseEditSnapshot | null;
  invalidateRedo: () => void;
  copy: (purchase: Purchase) => void;
  prepareForSubmit: () => PurchaseDraftItem[] | null;
};

export function EditPurchaseItems({
  householdId,
  merchant,
  copiedPurchase,
  household,
  history,
  value,
  commandsRef,
  classNames,
  onChange,
  onDirty,
  onHistoryAvailabilityChange,
}: {
  householdId: string;
  merchant: string;
  copiedPurchase: Purchase | null;
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
  onHistoryAvailabilityChange: (availability: { undo: boolean; redo: boolean }) => void;
}) {
  const [draft, setDraft] = useState(value);
  const draftRef = useRef(draft);
  const allocationUndoGroup = useRef<{ key: string; timer: number } | null>(null);
  const onChangeRef = useRef(onChange);
  const selectedProductVariantId = draft.selectedProductVariantId;
  const [productSuggestionResponse, setProductSuggestionResponse] =
    useState<ProductSuggestionResponse<ProductSuggestion>>(null);
  const productSuggestions = productSuggestionsForQuery(
    householdId,
    draft.itemName,
    merchant,
    productSuggestionResponse,
  );
  const [activeSuggestionIndex, setActiveSuggestionIndex] = useState(-1);
  const [suggestionsDismissed, setSuggestionsDismissed] = useState(false);
  const historyRef = useRef<PurchaseEditHistory>(emptyPurchaseEditHistory());
  const [selectedItemKeys, setSelectedItemKeys] = useState<Set<string>>(() => new Set());
  const [copiedPrices, setCopiedPrices] = useState<
    Map<string, { amount: string; occurredAt: string }>
  >(() => new Map());
  const [mobileSelectionMode, setMobileSelectionMode] = useState(false);
  const [bulkCategory, setBulkCategory] = useState("");
  const [bulkAllocation, setBulkAllocation] = useState("");
  const selectionAnchorKey = useRef<string | null>(null);
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
  const clearAllocationUndoGroup = useCallback(() => {
    if (allocationUndoGroup.current) window.clearTimeout(allocationUndoGroup.current.timer);
    allocationUndoGroup.current = null;
  }, []);
  const isEnabledCategory = useCallback(
    (id: string | null | undefined): id is string =>
      Boolean(id && enabledCategories.some((category) => category.id === id)),
    [enabledCategories],
  );

  useEffect(() => {
    if (window.matchMedia("(min-width: 761px) and (pointer: fine)").matches) {
      nameRef.current?.focus({ preventScroll: true });
    }
  }, []);

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

  const invalidateRedo = useCallback(() => {
    if (!historyRef.current.future.length) return;
    historyRef.current = { ...historyRef.current, future: [] };
    onHistoryAvailabilityChange({ undo: historyRef.current.past.length > 0, redo: false });
  }, [onHistoryAvailabilityChange]);
  const update = useCallback(
    (next: PurchaseItemsValue, preserveRedo = false, preserveAllocationGroup = false) => {
      if (!preserveAllocationGroup) clearAllocationUndoGroup();
      if (!preserveRedo) invalidateRedo();
      draftRef.current = next;
      setDraft(next);
      onChangeRef.current(next);
    },
    [clearAllocationUndoGroup, invalidateRedo],
  );
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
  const updateHistory = useCallback(
    (nextHistory: PurchaseEditHistory) => {
      historyRef.current = nextHistory;
      onHistoryAvailabilityChange({
        undo: nextHistory.past.length > 0,
        redo: nextHistory.future.length > 0,
      });
    },
    [onHistoryAvailabilityChange],
  );
  const recordUndo = useCallback(
    (snapshot: PurchaseEditSnapshot) => {
      clearAllocationUndoGroup();
      updateHistory(recordPurchaseEdit(historyRef.current, snapshot));
    },
    [clearAllocationUndoGroup, updateHistory],
  );
  const recordAllocationUndo = useCallback(
    (key: string, snapshot: PurchaseEditSnapshot) => {
      const group = allocationUndoGroup.current;
      if (!group || group.key !== key) {
        clearAllocationUndoGroup();
        updateHistory(recordPurchaseEdit(historyRef.current, snapshot));
      } else {
        window.clearTimeout(group.timer);
      }
      allocationUndoGroup.current = {
        key,
        timer: window.setTimeout(() => {
          allocationUndoGroup.current = null;
        }, 1000),
      };
    },
    [clearAllocationUndoGroup, updateHistory],
  );
  const reset = useCallback(() => {
    const empty = emptyPurchaseItemsValue();
    clearAllocationUndoGroup();
    updateHistory(emptyPurchaseEditHistory());
    setProductSuggestionResponse(null);
    update(empty);
  }, [clearAllocationUndoGroup, updateHistory, update]);
  const undo = useCallback(() => {
    clearAllocationUndoGroup();
    const transition = undoPurchaseEdit(historyRef.current, {
      items: draftRef.current.items,
      categoryId: draftRef.current.categoryId,
      merchant,
      copiedPurchase,
    });
    if (!transition) return null;
    markDirty();
    updateHistory(transition.history);
    update(
      {
        ...draftRef.current,
        items: transition.snapshot.items,
        ...(Object.hasOwn(transition.snapshot, "categoryId")
          ? { categoryId: transition.snapshot.categoryId ?? "" }
          : {}),
      },
      true,
    );
    return transition.snapshot;
  }, [clearAllocationUndoGroup, markDirty, updateHistory, update, merchant, copiedPurchase]);
  const redo = useCallback(() => {
    clearAllocationUndoGroup();
    const transition = redoPurchaseEdit(historyRef.current, {
      items: draftRef.current.items,
      categoryId: draftRef.current.categoryId,
      merchant,
      copiedPurchase,
    });
    if (!transition) return null;
    markDirty();
    updateHistory(transition.history);
    update(
      {
        ...draftRef.current,
        items: transition.snapshot.items,
        ...(Object.hasOwn(transition.snapshot, "categoryId")
          ? { categoryId: transition.snapshot.categoryId ?? "" }
          : {}),
      },
      true,
    );
    return transition.snapshot;
  }, [clearAllocationUndoGroup, markDirty, updateHistory, update, merchant, copiedPurchase]);
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
      setCopiedPrices(
        new Map(
          items.map((item) => [item.key, { amount: item.amount, occurredAt: purchase.occurredAt }]),
        ),
      );
      recordUndo({
        items: draftRef.current.items,
        categoryId: draftRef.current.categoryId,
        merchant,
        copiedPurchase,
      });
      update({ ...draftRef.current, items, categoryId: "" });
    },
    [markDirty, isEnabledCategory, recordUndo, update, merchant, copiedPurchase],
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
  useImperativeHandle(
    commandsRef,
    () => ({
      reset,
      undo,
      redo,
      invalidateRedo,
      copy,
      prepareForSubmit,
    }),
    [reset, undo, redo, invalidateRedo, copy, prepareForSubmit],
  );
  useEffect(() => () => clearAllocationUndoGroup(), [clearAllocationUndoGroup]);

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
          setProductSuggestionResponse({
            householdId,
            query,
            merchant: merchant.trim() || null,
            suggestions: result,
          });
          setActiveSuggestionIndex(-1);
        })
        .catch(() => {
          if (!active) return;
          setProductSuggestionResponse({
            householdId,
            query,
            merchant: merchant.trim() || null,
            suggestions: [],
          });
          setActiveSuggestionIndex(-1);
        });
    }, 220);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [householdId, draft.itemName, merchant]);

  const suggestions = useMemo(
    () => purchaseEntrySuggestions(history, draft.itemName, productSuggestions),
    [history, draft.itemName, productSuggestions],
  );

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
    recordUndo({ items: draft.items });
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
  const selectEntrySuggestion = (suggestion: (typeof suggestions)[number]) => {
    if (suggestion.kind === "catalog") {
      selectProductSuggestion(suggestion.candidate);
      return;
    }
    if (suggestion.kind === "history") {
      selectHistorySuggestion(suggestion.name);
      return;
    }
    if (!productSuggestions.includes(suggestion.candidate)) return;
    markDirty();
    patchDraft({
      itemName: suggestion.name,
      selectedProductVariantId: suggestion.candidate.variant.id,
      ...(isEnabledCategory(suggestion.categoryId) && !draft.categoryId
        ? { categoryId: suggestion.categoryId }
        : {}),
    });
    setProductSuggestionResponse(null);
    amountRef.current?.focus();
    setFocused("amount");
  };
  const clearProductSelection = () => {
    markDirty();
    patchDraft({ selectedProductVariantId: "" });
    setFocused("name");
    nameRef.current?.focus();
  };
  const visibleSuggestionCount = selectedProductVariantId ? 0 : suggestions.length;
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
      const suggestion = suggestions[activeSuggestionIndex];
      selectEntrySuggestion(suggestion);
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
    recordUndo({ items: draft.items });
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
    const currentItem = draft.items.find((item) => item.key === key);
    if (!currentItem) return;
    const allocationChanged =
      patch.allocations !== undefined &&
      JSON.stringify(patch.allocations) !== JSON.stringify(currentItem.allocations);
    const categoryChanged =
      patch.categoryId !== undefined && patch.categoryId !== currentItem.categoryId;
    if (allocationChanged) recordAllocationUndo(key, { items: draft.items });
    else if (categoryChanged) recordUndo({ items: draft.items });
    markDirty();
    if (patch.amount !== undefined) {
      setCopiedPrices((current) => {
        if (!current.has(key)) return current;
        const next = new Map(current);
        next.delete(key);
        return next;
      });
    }
    update(
      {
        ...draftRef.current,
        items: draft.items.map((item) => (item.key === key ? { ...item, ...patch } : item)),
      },
      false,
      allocationChanged,
    );
  };
  const removeItem = (key: string) => {
    if (!draft.items.some((item) => item.key === key)) return;
    markDirty();
    recordUndo({ items: draft.items });
    replaceItems(draft.items.filter((item) => item.key !== key));
  };
  const selectedItems = draft.items.filter((item) => selectedItemKeys.has(item.key));
  const clearSelection = () => {
    setSelectedItemKeys(new Set());
    selectionAnchorKey.current = null;
    setMobileSelectionMode(false);
  };
  const changeSelection = (key: string, shiftKey: boolean) => {
    const currentIndex = draft.items.findIndex((item) => item.key === key);
    const anchorIndex = draft.items.findIndex((item) => item.key === selectionAnchorKey.current);
    if (shiftKey && anchorIndex >= 0 && currentIndex >= 0) {
      const start = Math.min(anchorIndex, currentIndex);
      const end = Math.max(anchorIndex, currentIndex);
      setSelectedItemKeys(new Set(draft.items.slice(start, end + 1).map((item) => item.key)));
      return;
    }
    selectionAnchorKey.current = key;
    setSelectedItemKeys((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };
  const applyBulkPatch = (
    patchForItem: (item: PurchaseDraftItem) => Partial<PurchaseDraftItem>,
  ) => {
    if (!selectedItems.length) return;
    markDirty();
    recordUndo({ items: draft.items });
    const selectedKeys = new Set(selectedItems.map((item) => item.key));
    replaceItems(
      draft.items.map((item) =>
        selectedKeys.has(item.key) ? { ...item, ...patchForItem(item) } : item,
      ),
    );
    clearSelection();
    setBulkCategory("");
    setBulkAllocation("");
  };
  const applyBulkCategory = () => {
    if (!bulkCategory) return;
    const categoryId = bulkCategory === "__uncategorized" ? "" : bulkCategory;
    if (categoryId && !isEnabledCategory(categoryId)) return;
    applyBulkPatch(() => ({ categoryId }));
  };
  const applyBulkAllocation = () => {
    if (!bulkAllocation) return;
    const allocations =
      bulkAllocation === "__default"
        ? []
        : household.pools.some((pool) => pool.id === bulkAllocation)
          ? [{ poolId: bulkAllocation, weight: 1 }]
          : null;
    if (!allocations) return;
    applyBulkPatch(() => ({ allocations }));
  };
  const removeSelectedItems = () => {
    if (!selectedItems.length) return;
    if (!window.confirm(`選択した${selectedItems.length}品目を削除しますか？`)) return;
    markDirty();
    recordUndo({ items: draft.items });
    const selectedKeys = new Set(selectedItems.map((item) => item.key));
    replaceItems(draft.items.filter((item) => !selectedKeys.has(item.key)));
    clearSelection();
  };
  return (
    <>
      <div className={`panel-heading entry-heading ${classNames.itemHeading}`}>
        <div>
          <span className="eyebrow">品目</span>
          <h2>買ったもの</h2>
        </div>
        <div className={styles.selectionHeadingActions}>
          <button
            className={`button button-soft ${styles.mobileSelectionToggle}`}
            type="button"
            aria-pressed={mobileSelectionMode}
            onClick={() => {
              if (mobileSelectionMode) clearSelection();
              else setMobileSelectionMode(true);
            }}
          >
            {mobileSelectionMode ? "選択を終了" : "選択"}
          </button>
          <span className="entry-default">
            負担 <b>{household.pools.map((pool) => pool.name.slice(0, 1)).join(" / ") || "標準"}</b>
          </span>
        </div>
      </div>
      {selectedItems.length > 0 && (
        <div className={styles.bulkActions} aria-label="選択した品目の一括操作">
          <span className={styles.selectionCount}>{selectedItems.length}品目を選択中</span>
          <label>
            <span>カテゴリ</span>
            <select value={bulkCategory} onChange={(event) => setBulkCategory(event.target.value)}>
              <option value="">変更なし</option>
              <option value="__uncategorized">未分類</option>
              {enabledCategories.map((category) => (
                <option key={category.id} value={category.id}>
                  {category.name}
                </option>
              ))}
            </select>
          </label>
          <button
            className="button button-soft"
            type="button"
            disabled={!bulkCategory}
            onClick={applyBulkCategory}
          >
            カテゴリ変更
          </button>
          <label>
            <span>負担</span>
            <select
              value={bulkAllocation}
              onChange={(event) => setBulkAllocation(event.target.value)}
            >
              <option value="">変更なし</option>
              <option value="__default">標準に戻す</option>
              {household.pools.map((pool) => (
                <option key={pool.id} value={pool.id}>
                  {pool.name}が全額負担
                </option>
              ))}
            </select>
          </label>
          <button
            className="button button-soft"
            type="button"
            disabled={!bulkAllocation}
            onClick={applyBulkAllocation}
          >
            負担変更
          </button>
          <button className="button button-soft" type="button" onClick={removeSelectedItems}>
            削除
          </button>
          <button className="text-button" type="button" onClick={clearSelection}>
            選択解除
          </button>
        </div>
      )}
      <PurchaseItemsGrid
        items={draft.items}
        copiedPrices={copiedPrices}
        household={household}
        defaultAllocations={defaultAllocations}
        enabledCategories={enabledCategories}
        classNames={{ itemTable: classNames.itemTable, itemList: classNames.itemList }}
        selectedKeys={selectedItemKeys}
        mobileSelectionMode={mobileSelectionMode}
        gridRef={itemGridRef}
        onKeyDown={onItemGridKey}
        onSelectionChange={changeSelection}
        onPatch={patchItem}
        onRemove={removeItem}
      />
      <PurchaseItemEntry
        itemName={draft.itemName}
        itemAmount={draft.itemAmount}
        categoryId={effectiveCategoryId}
        categories={enabledCategories}
        suggestions={suggestions}
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
        onSelectSuggestion={selectEntrySuggestion}
        onClearProductSelection={clearProductSelection}
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
