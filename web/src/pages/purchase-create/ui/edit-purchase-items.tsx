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

import { type ProductSuggestion } from "../../../entities/product";
import { EditPurchaseItemAllocations } from "../../../entities/purchase";
import { useProductSuggestions } from "../../../features/product-variant-select";
import { type Household, type Purchase } from "../../../shared/api";
import {
  readPurchaseAllocationData,
  type PurchaseAllocationData,
} from "../api/read-purchase-allocation-data";
import {
  parsePastedPurchaseItems,
  type ParsedPurchaseItemPaste,
} from "../model/pasted-purchase-items";
import { purchaseEntrySuggestions, recentPurchasePrice } from "../model/product-suggestions";
import { resolvePurchaseAllocationRule } from "../model/purchase-allocation-rules";
import {
  emptyPurchaseEditHistory,
  recordPurchaseEdit,
  redoPurchaseEdit,
  undoPurchaseEdit,
  type PurchaseEditHistory,
  type PurchaseEditSnapshot,
} from "../model/purchase-edit-history";
import {
  emptyPurchaseItemsValue,
  suggestedPurchaseItemCategory,
  type PurchaseItemsValue,
} from "../model/purchase-items";
import { PurchaseItemEntry } from "./purchase-item-entry";
import { PurchaseItemsGrid } from "./purchase-items-grid";

import styles from "./edit-purchase-items.module.css";

const emptyPurchaseAllocationData: PurchaseAllocationData = {
  rules: [],
  conceptIdsByVariant: new Map(),
};

const purchaseItemSchema = z.object({
  name: z.string().trim().min(1),
  amount: z.string().regex(/^[1-9]\d*$/),
});
const normalizePastedAmount = (amount: string) => amount.replace(/[¥,]/g, "");
const isValidPastedAmount = (amount: string) => /^[1-9]\d*$/.test(normalizePastedAmount(amount));

export type EditPurchaseItemsCommands = {
  reset: () => void;
  undo: () => PurchaseEditSnapshot | null;
  redo: () => PurchaseEditSnapshot | null;
  invalidateRedo: () => void;
  copy: (purchase: Purchase, mode: "replace" | "append") => void;
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
  const { suggestions: productSuggestions } = useProductSuggestions({
    householdId,
    query: draft.itemName,
    merchant,
    limit: 6,
    delay: 220,
  });
  const [suggestionsDismissed, setSuggestionsDismissed] = useState(false);
  const historyRef = useRef<PurchaseEditHistory>(emptyPurchaseEditHistory());
  const [selectedItemKeys, setSelectedItemKeys] = useState<Set<string>>(() => new Set());
  const [copiedPrices, setCopiedPrices] = useState<
    Map<string, { amount: string; occurredAt: string }>
  >(() => new Map());
  const [mobileSelectionMode, setMobileSelectionMode] = useState(false);
  const [bulkCategory, setBulkCategory] = useState("");
  const [bulkAllocation, setBulkAllocation] = useState<PurchaseDraftItem["allocations"] | null>(
    null,
  );
  const selectionAnchorKey = useRef<string | null>(null);
  const [focused, setFocused] = useState<"name" | "amount" | null>(null);
  const [invalidEntryField, setInvalidEntryField] = useState<"name" | "amount" | null>(null);
  const [pasteReview, setPasteReview] = useState<{
    rawText: string;
    parsed: ParsedPurchaseItemPaste;
  } | null>(null);
  const [categoryManuallySelected, setCategoryManuallySelected] = useState(false);
  const [allocationDataState, setAllocationDataState] = useState<{
    householdId: string;
    data: PurchaseAllocationData;
  } | null>(null);
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
  const defaultAllocationLabel = useMemo(() => {
    const namedAllocations = defaultAllocations
      .map((allocation) => ({
        name: household.pools.find((pool) => pool.id === allocation.poolId)?.name,
        weight: allocation.weight,
      }))
      .filter((allocation): allocation is { name: string; weight: number } =>
        Boolean(allocation.name && allocation.weight > 0),
      );
    const totalWeight = namedAllocations.reduce((sum, allocation) => sum + allocation.weight, 0);
    return totalWeight > 0
      ? namedAllocations
          .map(
            (allocation) =>
              `${allocation.name} ${new Intl.NumberFormat("ja-JP", { maximumFractionDigits: 1 }).format((allocation.weight / totalWeight) * 100)}%`,
          )
          .join(" / ")
      : "設定なし";
  }, [defaultAllocations, household.pools]);
  useEffect(() => {
    let active = true;
    void readPurchaseAllocationData(householdId)
      .then((data) => {
        if (active) setAllocationDataState({ householdId, data });
      })
      .catch(() => {
        if (active) {
          setAllocationDataState({
            householdId,
            data: { rules: [], conceptIdsByVariant: new Map() },
          });
        }
      });
    return () => {
      active = false;
    };
  }, [householdId]);
  const allocationData =
    allocationDataState?.householdId === householdId
      ? allocationDataState.data
      : emptyPurchaseAllocationData;
  const allocationDefaultsForItem = useCallback(
    (item: PurchaseDraftItem) => {
      const rule = resolvePurchaseAllocationRule(
        allocationData.rules,
        allocationData.conceptIdsByVariant,
        merchant,
        item.categoryId,
        item.productVariantId,
      );
      return {
        allocations: rule?.allocations ?? defaultAllocations,
        label: rule ? "ルール" : "標準",
      };
    },
    [allocationData, defaultAllocations, merchant],
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
  const suggestedCategoryFromHistory = useMemo(
    () =>
      suggestedPurchaseItemCategory(
        draft.itemName,
        draft.categoryId,
        categoryManuallySelected,
        history,
        isEnabledCategory,
      ),
    [categoryManuallySelected, draft.categoryId, draft.itemName, history, isEnabledCategory],
  );

  const effectiveCategoryId = draft.categoryId || suggestedCategoryFromHistory;

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
  const updateCategory = useCallback(
    (id: string) => {
      setCategoryManuallySelected(true);
      patchDraft({ categoryId: id });
    },
    [patchDraft],
  );
  const changeItemName = useCallback(
    (name: string) => {
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
    setCategoryManuallySelected(false);
    clearAllocationUndoGroup();
    updateHistory(emptyPurchaseEditHistory());
    update(empty);
  }, [clearAllocationUndoGroup, updateHistory, update]);
  const undo = useCallback(() => {
    clearAllocationUndoGroup();
    setBulkAllocation(null);
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
    setBulkAllocation(null);
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
    (purchase: Purchase, mode: "replace" | "append") => {
      markDirty();
      const copiedItems = purchase.items.map((item) => ({
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
        (current) =>
          new Map([
            ...(mode === "append" ? current : []),
            ...copiedItems.map(
              (item) =>
                [item.key, { amount: item.amount, occurredAt: purchase.occurredAt }] as const,
            ),
          ]),
      );
      setCategoryManuallySelected(false);
      recordUndo({
        items: draftRef.current.items,
        categoryId: draftRef.current.categoryId,
        merchant,
        copiedPurchase,
      });
      const current = draftRef.current;
      update({
        ...current,
        items: mode === "append" ? [...current.items, ...copiedItems] : copiedItems,
        categoryId: mode === "append" ? current.categoryId : "",
      });
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

  const suggestions = useMemo(
    () => purchaseEntrySuggestions(history, draft.itemName, productSuggestions),
    [history, draft.itemName, productSuggestions],
  );
  const suggestedPrice = useMemo(
    () => recentPurchasePrice(history, draft.itemName, draft.selectedProductVariantId || null),
    [history, draft.itemName, draft.selectedProductVariantId],
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
    setCategoryManuallySelected(false);
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
      ...(isEnabledCategory(candidate.concept.categoryId) &&
      !effectiveCategoryId &&
      !categoryManuallySelected
        ? { categoryId: candidate.concept.categoryId }
        : {}),
    });
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
    const suggestedCategoryId = isEnabledCategory(suggestion.categoryId)
      ? suggestion.categoryId
      : suggestion.candidate.concept.categoryId;
    patchDraft({
      itemName: suggestion.name,
      selectedProductVariantId: suggestion.candidate.variant.id,
      ...(!effectiveCategoryId &&
      !categoryManuallySelected &&
      isEnabledCategory(suggestedCategoryId)
        ? { categoryId: suggestedCategoryId }
        : {}),
    });
    amountRef.current?.focus();
    setFocused("amount");
  };
  const clearProductSelection = () => {
    markDirty();
    patchDraft({ selectedProductVariantId: "" });
    setFocused("name");
    nameRef.current?.focus();
  };
  const onNameKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (
      event.key === "Enter" &&
      !event.currentTarget.getAttribute("aria-activedescendant") &&
      !event.nativeEvent.isComposing &&
      !composing.current
    ) {
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
    setPasteReview({ rawText: text, parsed });
  };
  const addPastedItems = () => {
    if (!pasteReview || pasteReview.parsed.invalidSyntax) return;
    const repairedItems = pasteReview.parsed.invalidRows.map(({ name, amount }) => ({
      name: name.trim(),
      amount: normalizePastedAmount(amount),
    }));
    if (repairedItems.some(({ name, amount }) => !name || !isValidPastedAmount(amount))) return;
    const itemsToAdd = [...pasteReview.parsed.items, ...repairedItems];
    if (!itemsToAdd.length) return;
    markDirty();
    recordUndo({ items: draft.items });
    replaceItems([
      ...draft.items,
      ...itemsToAdd.map(({ name, amount }) => {
        const categoryId =
          categoryManuallySelected || draft.categoryId
            ? effectiveCategoryId
            : suggestedPurchaseItemCategory(name, "", false, history, isEnabledCategory);
        return {
          key: crypto.randomUUID(),
          name,
          amount,
          categoryId,
          allocations: [],
        };
      }),
    ]);
    setPasteReview(null);
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
    clearAllocationUndoGroup();
    setSelectedItemKeys(new Set());
    setBulkAllocation(null);
    selectionAnchorKey.current = null;
    setMobileSelectionMode(false);
  };
  const changeSelection = (key: string, shiftKey: boolean) => {
    clearAllocationUndoGroup();
    setBulkAllocation(null);
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
    allocationUndoKey?: string,
  ) => {
    if (!selectedItems.length) return;
    markDirty();
    if (allocationUndoKey) recordAllocationUndo(allocationUndoKey, { items: draft.items });
    else recordUndo({ items: draft.items });
    const selectedKeys = new Set(selectedItems.map((item) => item.key));
    replaceItems(
      draft.items.map((item) =>
        selectedKeys.has(item.key) ? { ...item, ...patchForItem(item) } : item,
      ),
    );
    setBulkCategory("");
  };
  const applyBulkCategory = (categoryValue: string) => {
    setBulkCategory(categoryValue);
    if (!categoryValue) return;
    const categoryId = categoryValue === "__uncategorized" ? "" : categoryValue;
    if (categoryId && !isEnabledCategory(categoryId)) return;
    if (!selectedItems.some((item) => item.categoryId !== categoryId)) return;
    applyBulkPatch(() => ({ categoryId }));
  };
  const applyBulkAllocation = (allocations: PurchaseDraftItem["allocations"]) => {
    setBulkAllocation(allocations);
    if (
      !selectedItems.some(
        (item) => JSON.stringify(item.allocations) !== JSON.stringify(allocations),
      )
    )
      return;
    applyBulkPatch(() => ({ allocations }), "bulk-allocation");
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
  const updatePasteInvalidRow = (index: number, field: "name" | "amount", nextValue: string) => {
    setPasteReview((current) =>
      current
        ? {
            ...current,
            parsed: {
              ...current.parsed,
              invalidRows: current.parsed.invalidRows.map((item, itemIndex) =>
                itemIndex === index ? { ...item, [field]: nextValue } : item,
              ),
            },
          }
        : current,
    );
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
            標準負担 <b>{defaultAllocationLabel}</b>
          </span>
        </div>
      </div>
      {pasteReview && (
        <section className={styles.pasteReview} aria-label="貼り付けた品目">
          <div className={styles.pasteReviewHeading}>
            <b>貼り付けた品目</b>
            <button type="button" className="text-button" onClick={() => setPasteReview(null)}>
              閉じる
            </button>
          </div>
          {pasteReview.parsed.items.length > 0 && (
            <ul className={styles.pasteValidItems}>
              {pasteReview.parsed.items.map((item) => (
                <li key={item.rowNumber}>
                  <span>{item.name}</span>
                  <span>¥{new Intl.NumberFormat("ja-JP").format(Number(item.amount))}</span>
                </li>
              ))}
            </ul>
          )}
          {pasteReview.parsed.invalidSyntax ? (
            <div className={styles.pasteSyntaxError}>
              <label>
                <span>CSV</span>
                <textarea
                  value={pasteReview.rawText}
                  onChange={(event) =>
                    setPasteReview({
                      rawText: event.target.value,
                      parsed: parsePastedPurchaseItems(event.target.value),
                    })
                  }
                  rows={4}
                />
              </label>
              <span role="alert">引用符を確認してください</span>
            </div>
          ) : (
            <>
              {pasteReview.parsed.invalidRows.map((row, index) => (
                <div className={styles.pasteInvalidRow} key={row.rowNumber}>
                  <span>行{row.rowNumber}</span>
                  <label>
                    <span>品名</span>
                    <input
                      value={row.name}
                      aria-label={`行${row.rowNumber}の品名`}
                      aria-invalid={!row.name.trim()}
                      onChange={(event) => updatePasteInvalidRow(index, "name", event.target.value)}
                    />
                    {!row.name.trim() && <small>品名を入力</small>}
                  </label>
                  <label>
                    <span>金額</span>
                    <input
                      inputMode="numeric"
                      value={row.amount}
                      aria-label={`行${row.rowNumber}の金額`}
                      aria-invalid={!isValidPastedAmount(row.amount)}
                      onChange={(event) =>
                        updatePasteInvalidRow(index, "amount", event.target.value)
                      }
                    />
                    {!isValidPastedAmount(row.amount) && <small>正の金額を入力</small>}
                  </label>
                </div>
              ))}
            </>
          )}
          <button
            type="button"
            className="button button-primary"
            disabled={
              pasteReview.parsed.invalidSyntax ||
              (!pasteReview.parsed.items.length && !pasteReview.parsed.invalidRows.length) ||
              pasteReview.parsed.invalidRows.some(
                ({ name, amount }) => !name.trim() || !isValidPastedAmount(amount),
              )
            }
            onClick={addPastedItems}
          >
            品目に追加
          </button>
        </section>
      )}
      {selectedItems.length > 0 && (
        <div className={styles.bulkActions} aria-label="選択した品目の一括操作">
          <span className={styles.selectionCount}>{selectedItems.length}品目を選択中</span>
          <label>
            <span>カテゴリ</span>
            <select
              value={bulkCategory}
              onChange={(event) => applyBulkCategory(event.target.value)}
            >
              <option value="">変更なし</option>
              <option value="__uncategorized">未分類</option>
              {enabledCategories.map((category) => (
                <option key={category.id} value={category.id}>
                  {category.name}
                </option>
              ))}
            </select>
          </label>
          <EditPurchaseItemAllocations
            allocations={bulkAllocation ?? []}
            defaultAllocations={defaultAllocations}
            pools={household.pools}
            onChange={applyBulkAllocation}
          />
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
        allocationDefaultsForItem={allocationDefaultsForItem}
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
        onSuggestionsOpenChange={(open) => setSuggestionsDismissed(!open)}
        onAmountChange={(amount) => {
          markDirty();
          setInvalidEntryField(null);
          update({ ...draft, itemAmount: amount.replace(/\D/g, "") });
        }}
        recentPurchasePrice={suggestedPrice}
        onApplyRecentPrice={() => {
          if (!suggestedPrice) return;
          markDirty();
          update({ ...draft, itemAmount: String(suggestedPrice.amountYen) });
          setFocused("amount");
          amountRef.current?.focus();
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
