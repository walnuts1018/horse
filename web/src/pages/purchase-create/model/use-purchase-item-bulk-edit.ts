import { useRef, useState } from "react";

import type { PurchaseDraftItem } from "../../../shared/lib";

export function usePurchaseItemBulkEdit({
  items,
  enabledCategoryIds,
  markDirty,
  replaceItems,
  recordUndo,
  recordAllocationUndo,
  clearAllocationUndoGroup,
}: {
  items: PurchaseDraftItem[];
  enabledCategoryIds: ReadonlySet<string>;
  markDirty: () => void;
  replaceItems: (items: PurchaseDraftItem[]) => void;
  recordUndo: () => void;
  recordAllocationUndo: (key: string) => void;
  clearAllocationUndoGroup: () => void;
}) {
  const [selectedItemKeys, setSelectedItemKeys] = useState<Set<string>>(() => new Set());
  const [mobileSelectionMode, setMobileSelectionMode] = useState(false);
  const [bulkCategory, setBulkCategory] = useState("");
  const [bulkAllocation, setBulkAllocation] = useState<PurchaseDraftItem["allocations"] | null>(
    null,
  );
  const selectionAnchorKey = useRef<string | null>(null);
  const selectedItems = items.filter((item) => selectedItemKeys.has(item.key));

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
    const currentIndex = items.findIndex((item) => item.key === key);
    const anchorIndex = items.findIndex((item) => item.key === selectionAnchorKey.current);
    if (shiftKey && anchorIndex >= 0 && currentIndex >= 0) {
      const start = Math.min(anchorIndex, currentIndex);
      const end = Math.max(anchorIndex, currentIndex);
      setSelectedItemKeys(new Set(items.slice(start, end + 1).map((item) => item.key)));
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
    if (allocationUndoKey) recordAllocationUndo(allocationUndoKey);
    else recordUndo();
    const selectedKeys = new Set(selectedItems.map((item) => item.key));
    replaceItems(
      items.map((item) => (selectedKeys.has(item.key) ? { ...item, ...patchForItem(item) } : item)),
    );
    setBulkCategory("");
  };

  const applyBulkCategory = (categoryValue: string) => {
    setBulkCategory(categoryValue);
    if (!categoryValue) return;
    const categoryId = categoryValue === "__uncategorized" ? "" : categoryValue;
    if (categoryId && !enabledCategoryIds.has(categoryId)) return;
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
    if (
      !selectedItems.length ||
      !window.confirm(`選択した${selectedItems.length}品目を削除しますか？`)
    )
      return;
    markDirty();
    recordUndo();
    const selectedKeys = new Set(selectedItems.map((item) => item.key));
    replaceItems(items.filter((item) => !selectedKeys.has(item.key)));
    clearSelection();
  };

  return {
    selectedItemKeys,
    selectedItems,
    mobileSelectionMode,
    setMobileSelectionMode,
    bulkCategory,
    bulkAllocation,
    setBulkAllocation,
    clearSelection,
    changeSelection,
    applyBulkCategory,
    applyBulkAllocation,
    removeSelectedItems,
  };
}
