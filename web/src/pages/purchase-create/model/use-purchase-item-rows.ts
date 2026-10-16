import {
  useCallback,
  type Dispatch,
  type KeyboardEvent,
  type RefObject,
  type SetStateAction,
} from "react";

import type { PurchaseDraftItem } from "../../../shared/lib";
import type { PurchaseEditSnapshot } from "./purchase-edit-history";
import type { PurchaseItemsValue } from "./purchase-items";

import { purchaseItemSchema } from "./purchase-item-input";

type MutableRef<T> = { current: T };

export function usePurchaseItemRows({
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
}: {
  draft: PurchaseItemsValue;
  draftRef: MutableRef<PurchaseItemsValue>;
  effectiveCategoryId: string;
  update: (next: PurchaseItemsValue) => void;
  replaceItems: (items: PurchaseDraftItem[]) => void;
  markDirty: () => void;
  recordUndo: (snapshot: PurchaseEditSnapshot) => void;
  recordAllocationUndo: (key: string, snapshot: PurchaseEditSnapshot) => void;
  setCopiedPrices: Dispatch<SetStateAction<Map<string, { amount: string; occurredAt: string }>>>;
  setCategoryManuallySelected: (selected: boolean) => void;
  setInvalidEntryField: (field: "name" | "amount" | null) => void;
  setFocused: (field: "name" | "amount" | null) => void;
  nameRef: RefObject<HTMLInputElement | null>;
  amountRef: RefObject<HTMLInputElement | null>;
  itemGridRef: RefObject<HTMLDivElement | null>;
  composing: MutableRef<boolean>;
}) {
  const addItem = useCallback(() => {
    const parsed = purchaseItemSchema.safeParse({ name: draft.itemName, amount: draft.itemAmount });
    if (!parsed.success) {
      const field = draft.itemName.trim() ? "amount" : "name";
      setInvalidEntryField(field);
      setFocused(field);
      (field === "name" ? nameRef : amountRef).current?.focus();
      return;
    }
    markDirty();
    const item: PurchaseDraftItem = {
      key: crypto.randomUUID(),
      ...parsed.data,
      categoryId: effectiveCategoryId,
      ...(draft.selectedProductVariantId
        ? { productVariantId: draft.selectedProductVariantId }
        : {}),
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
  }, [
    draft,
    markDirty,
    effectiveCategoryId,
    recordUndo,
    update,
    setCategoryManuallySelected,
    setInvalidEntryField,
    setFocused,
    nameRef,
    amountRef,
  ]);

  const onItemGridKey = useCallback(
    (event: KeyboardEvent<HTMLInputElement>, row: number, column: "name" | "amount") => {
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
            ?.querySelector<HTMLInputElement>(
              `[data-grid-row="${row + 1}"][data-grid-column="name"]`,
            )
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
    },
    [draft.items.length, composing, itemGridRef, nameRef, amountRef],
  );

  const patchItem = useCallback(
    (key: string, patch: Partial<PurchaseDraftItem>) => {
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
      update({
        ...draftRef.current,
        items: draft.items.map((item) => (item.key === key ? { ...item, ...patch } : item)),
      });
    },
    [draft.items, draftRef, recordAllocationUndo, recordUndo, markDirty, setCopiedPrices, update],
  );

  const removeItem = useCallback(
    (key: string) => {
      if (!draft.items.some((item) => item.key === key)) return;
      markDirty();
      recordUndo({ items: draft.items });
      replaceItems(draft.items.filter((item) => item.key !== key));
    },
    [draft.items, markDirty, recordUndo, replaceItems],
  );

  return { addItem, onItemGridKey, patchItem, removeItem };
}
