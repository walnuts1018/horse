import { useCallback, useState, type ClipboardEvent } from "react";

import type { Purchase } from "../../../shared/api";
import type { PurchaseDraftItem } from "../../../shared/lib";

import {
  parsePastedPurchaseItems,
  type ParsedPurchaseItemPaste,
} from "./parse-pasted-purchase-items";
import { suggestedPurchaseItemCategory, type PurchaseItemsValue } from "./purchase-items";

const normalizePastedAmount = (amount: string) => amount.replace(/[¥,]/g, "");
const isValidPastedAmount = (amount: string) => /^[1-9]\d*$/.test(normalizePastedAmount(amount));

type PasteReview = { rawText: string; parsed: ParsedPurchaseItemPaste };

export function usePurchaseItemPaste({
  draft,
  effectiveCategoryId,
  categoryManuallySelected,
  history,
  isEnabledCategory,
  markDirty,
  recordUndo,
  replaceItems,
}: {
  draft: PurchaseItemsValue;
  effectiveCategoryId: string;
  categoryManuallySelected: boolean;
  history: Purchase[];
  isEnabledCategory: (id: string | null | undefined) => id is string;
  markDirty: () => void;
  recordUndo: (snapshot: { items: PurchaseDraftItem[] }) => void;
  replaceItems: (items: PurchaseDraftItem[]) => void;
}) {
  const [pasteReview, setPasteReview] = useState<PasteReview | null>(null);
  const onPaste = useCallback((event: ClipboardEvent<HTMLInputElement>) => {
    const text = event.clipboardData.getData("text");
    const parsed = parsePastedPurchaseItems(text);
    if (!parsed.structured) return;
    event.preventDefault();
    setPasteReview({ rawText: text, parsed });
  }, []);
  const replaceRawText = useCallback((rawText: string) => {
    setPasteReview({ rawText, parsed: parsePastedPurchaseItems(rawText) });
  }, []);
  const addPastedItems = useCallback(() => {
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
          categoryManuallySelected || draft.categoryId ? effectiveCategoryId : undefined;
        return {
          key: crypto.randomUUID(),
          name,
          amount,
          categoryId:
            categoryId ??
            suggestedPurchaseItemCategory(name, "", false, history, isEnabledCategory),
          allocations: [],
        };
      }),
    ]);
    setPasteReview(null);
  }, [
    pasteReview,
    markDirty,
    recordUndo,
    draft.items,
    draft.categoryId,
    categoryManuallySelected,
    effectiveCategoryId,
    replaceItems,
    history,
    isEnabledCategory,
  ]);
  const updateInvalidRow = useCallback(
    (index: number, field: "name" | "amount", nextValue: string) => {
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
    },
    [],
  );

  return { pasteReview, setPasteReview, onPaste, addPastedItems, updateInvalidRow, replaceRawText };
}
