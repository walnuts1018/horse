import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type Ref,
} from "react";

import type { PurchaseDraftItem } from "../../../shared/lib";

import { EditPurchaseItemAllocations } from "../../../entities/purchase";
import { type Household, type Purchase } from "../../../shared/api";
import {
  readPurchaseAllocationData,
  type PurchaseAllocationData,
} from "../api/read-purchase-allocation-data";
import { resolvePurchaseAllocationRule } from "../model/purchase-allocation-rules";
import {
  emptyPurchaseEditHistory,
  recordPurchaseEdit,
  redoPurchaseEdit,
  undoPurchaseEdit,
  type PurchaseEditHistory,
  type PurchaseEditSnapshot,
} from "../model/purchase-edit-history";
import { purchaseItemSchema } from "../model/purchase-item-input";
import {
  emptyPurchaseItemsValue,
  suggestedPurchaseItemCategory,
  type PurchaseItemsValue,
} from "../model/purchase-items";
import { usePurchaseEntryCandidates } from "../model/use-purchase-entry-candidates";
import { usePurchaseItemBulkEdit } from "../model/use-purchase-item-bulk-edit";
import { usePurchaseItemPaste } from "../model/use-purchase-item-paste";
import { usePurchaseItemRows } from "../model/use-purchase-item-rows";
import { PurchaseItemEntry } from "./purchase-item-entry";
import { PurchaseItemPasteReview } from "./purchase-item-paste-review";
import { PurchaseItemsGrid } from "./purchase-items-grid";

import styles from "./edit-purchase-items.module.css";

const emptyPurchaseAllocationData: PurchaseAllocationData = {
  rules: [],
  conceptIdsByVariant: new Map(),
};

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
  const historyRef = useRef<PurchaseEditHistory>(emptyPurchaseEditHistory());
  const [copiedPrices, setCopiedPrices] = useState<
    Map<string, { amount: string; occurredAt: string }>
  >(() => new Map());
  const [focused, setFocused] = useState<"name" | "amount" | null>(null);
  const [invalidEntryField, setInvalidEntryField] = useState<"name" | "amount" | null>(null);
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
  const markDirty = useCallback(() => onDirty(), [onDirty]);
  const {
    suggestions,
    suggestedPrice,
    suggestionsDismissed,
    setSuggestionsDismissed,
    changeName,
    selectEntrySuggestion,
    clearProductSelection,
  } = usePurchaseEntryCandidates({
    householdId,
    merchant,
    history,
    query: draft.itemName,
    selectedProductVariantId,
    effectiveCategoryId,
    categoryManuallySelected,
    isEnabledCategory,
    patchDraft,
    markDirty,
    focusAmount: () => amountRef.current?.focus(),
    focusName: () => nameRef.current?.focus(),
    setFocused,
  });
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
  const { addItem, onItemGridKey, patchItem, removeItem } = usePurchaseItemRows({
    draft,
    draftRef,
    effectiveCategoryId,
    update,
    replaceItems,
    markDirty,
    recordUndo,
    recordAllocationUndo,
    setCopiedPrices,
    setCategoryManuallySelected,
    setInvalidEntryField,
    setFocused,
    nameRef,
    amountRef,
    itemGridRef,
    composing,
  });
  const onNameKey = useCallback(
    (event: KeyboardEvent<HTMLInputElement>) => {
      if (
        event.key !== "Enter" ||
        event.currentTarget.getAttribute("aria-activedescendant") ||
        event.nativeEvent.isComposing ||
        composing.current
      )
        return;
      event.preventDefault();
      if (!draft.itemName.trim()) {
        setInvalidEntryField("name");
        setFocused("name");
        return;
      }
      amountRef.current?.focus();
      setFocused("amount");
    },
    [draft.itemName, setInvalidEntryField, setFocused],
  );
  const onAmountKey = useCallback(
    (event: KeyboardEvent<HTMLInputElement>) => {
      if (event.key !== "Enter" || event.nativeEvent.isComposing || composing.current) return;
      event.preventDefault();
      addItem();
    },
    [addItem, composing],
  );
  const {
    pasteReview,
    setPasteReview,
    onPaste: pasteRows,
    addPastedItems,
    updateInvalidRow: updatePasteInvalidRow,
    replaceRawText: replacePasteRawText,
  } = usePurchaseItemPaste({
    draft,
    effectiveCategoryId,
    categoryManuallySelected,
    history,
    isEnabledCategory,
    markDirty,
    recordUndo,
    replaceItems,
  });
  const bulkEdit = usePurchaseItemBulkEdit({
    items: draft.items,
    enabledCategoryIds: new Set(enabledCategories.map((category) => category.id)),
    markDirty,
    replaceItems,
    recordUndo: () => recordUndo({ items: draftRef.current.items }),
    recordAllocationUndo: (key) => recordAllocationUndo(key, { items: draftRef.current.items }),
    clearAllocationUndoGroup,
  });
  const { setBulkAllocation } = bulkEdit;
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
  }, [
    clearAllocationUndoGroup,
    markDirty,
    updateHistory,
    update,
    merchant,
    copiedPurchase,
    setBulkAllocation,
  ]);
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
  }, [
    clearAllocationUndoGroup,
    markDirty,
    updateHistory,
    update,
    merchant,
    copiedPurchase,
    setBulkAllocation,
  ]);
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

  const {
    selectedItems,
    selectedItemKeys,
    mobileSelectionMode,
    setMobileSelectionMode,
    bulkCategory,
    bulkAllocation,
    clearSelection,
    changeSelection,
    applyBulkCategory,
    applyBulkAllocation,
    removeSelectedItems,
  } = bulkEdit;
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
        <PurchaseItemPasteReview
          review={pasteReview}
          onClose={() => setPasteReview(null)}
          onRawTextChange={replacePasteRawText}
          onInvalidRowChange={updatePasteInvalidRow}
          onAdd={addPastedItems}
        />
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
          changeName(name);
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
