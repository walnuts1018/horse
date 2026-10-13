import { useCallback, useEffect, useRef, useState } from "react";
import {
  bulkUpdatePurchaseItemsMutation,
  type BulkUpdatePurchaseItemsMutation$data,
  type BulkUpdatePurchaseItemsMutation$variables,
} from "../../../entities/purchase";
import { useGraphqlMutation, type Purchase } from "../../../shared/api";

type LedgerItemBulkEditResult = {
  bulkUpdatePurchaseItems: NonNullable<
    BulkUpdatePurchaseItemsMutation$data["bulkUpdatePurchaseItems"]["items"]
  >;
};
type LedgerItemBulkPatch = BulkUpdatePurchaseItemsMutation$variables["input"]["patch"];

type LedgerItemBulkUndo = Array<{
  itemId: string;
  baseRevision: number;
  patch: LedgerItemBulkPatch;
}>;

export function useLedgerItemBulkEdit({
  householdId,
  purchases,
  selectionKey,
  refresh,
}: {
  householdId: string;
  purchases: Purchase[];
  selectionKey: string;
  refresh: () => void | Promise<void>;
}) {
  const { mutate } = useGraphqlMutation<LedgerItemBulkEditResult>(bulkUpdatePurchaseItemsMutation);
  const mutateBulkUpdatePurchaseItems = useCallback(
    (variables: BulkUpdatePurchaseItemsMutation$variables) => mutate(variables),
    [mutate],
  );
  const [selectedItems, setSelectedItems] = useState<Set<string>>(() => new Set());
  const selectionAnchor = useRef<{ context: string; id: string } | null>(null);
  const [category, setCategory] = useState("");
  const [allocationsActive, setAllocationsActive] = useState(false);
  const [allocations, setAllocations] = useState<Record<string, string>>({});
  const [variantId, setVariantId] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [tagsActive, setTagsActive] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [undo, setUndo] = useState<LedgerItemBulkUndo | null>(null);

  useEffect(() => {
    setSelectedItems(new Set());
    selectionAnchor.current = null;
  }, [selectionKey]);
  useEffect(() => {
    setSelectedItems(new Set());
    selectionAnchor.current = null;
    setUndo(null);
    setError("");
  }, [householdId]);

  const clearSelection = () => {
    setSelectedItems(new Set());
    selectionAnchor.current = null;
  };
  const toggleSelection = (
    context: string,
    visibleIds: string[],
    id: string,
    shiftKey: boolean,
  ) => {
    const anchorIndex =
      shiftKey && selectionAnchor.current?.context === context
        ? visibleIds.indexOf(selectionAnchor.current.id)
        : -1;
    const targetIndex = visibleIds.indexOf(id);
    const range =
      anchorIndex >= 0 && targetIndex >= 0
        ? visibleIds.slice(
            Math.min(anchorIndex, targetIndex),
            Math.max(anchorIndex, targetIndex) + 1,
          )
        : null;
    const additions = range?.filter((targetId) => !selectedItems.has(targetId)) ?? [];
    if (selectedItems.size + additions.length > 100) {
      setError("一括変更は100品目までです");
      return;
    }
    const next = new Set(selectedItems);
    if (range) range.forEach((targetId) => next.add(targetId));
    else if (next.has(id)) next.delete(id);
    else next.add(id);
    if (!shiftKey) selectionAnchor.current = { context, id };
    setError("");
    setSelectedItems(next);
  };

  const apply = async () => {
    if (!category && !allocationsActive && !variantId && !tagsActive) return;
    const targets = purchases.flatMap((purchase) =>
      purchase.items
        .filter((item) => selectedItems.has(item.id))
        .map((item) => ({ purchase, item })),
    );
    if (!targets.length) return;
    setPending(true);
    setError("");
    setUndo(null);
    try {
      const patch: BulkUpdatePurchaseItemsMutation$variables["input"]["patch"] = {};
      if (category) patch.categoryId = category === "__uncategorized" ? null : category;
      if (allocationsActive)
        patch.allocations = Object.entries(allocations)
          .filter(([, weight]) => Number(weight) > 0)
          .map(([poolId, weight]) => ({ poolId, weight: Number(weight) }));
      if (variantId) patch.productVariantId = variantId === "__none" ? null : variantId;
      if (tagsActive) patch.tagIds = tags;
      const result = await mutateBulkUpdatePurchaseItems({
        input: {
          targets: targets.map(({ item }) => ({ itemId: item.id, baseRevision: item.revision })),
          patch,
        },
      });
      const undoEntries: NonNullable<typeof undo> = targets.map(({ item }) => {
        const undoPatch: LedgerItemBulkPatch = {};
        if (category) undoPatch.categoryId = item.categoryId ?? null;
        if (allocationsActive)
          undoPatch.allocations = item.allocations
            .filter((allocation) => allocation.weight > 0)
            .map(({ poolId, weight }) => ({ poolId, weight }));
        if (variantId) undoPatch.productVariantId = item.productVariantId ?? null;
        if (tagsActive) undoPatch.tagIds = item.tags.map((tag) => tag.id);
        return {
          itemId: item.id,
          baseRevision:
            result.bulkUpdatePurchaseItems.find((updated) => updated.id === item.id)?.revision ??
            item.revision + 1,
          patch: undoPatch,
        };
      });
      setUndo(undoEntries.length ? undoEntries : null);
      clearSelection();
      setCategory("");
      setAllocationsActive(false);
      setAllocations({});
      setVariantId("");
      setTags([]);
      setTagsActive(false);
      await refresh();
    } catch (reason) {
      setError((reason as Error).message);
      await refresh();
    } finally {
      setPending(false);
    }
  };

  const undoChanges = async () => {
    if (!undo?.length) return;
    setPending(true);
    setError("");
    const completed = new Set<number>();
    try {
      const groups = new Map<string, Array<{ index: number; entry: LedgerItemBulkUndo[number] }>>();
      undo.forEach((entry, index) => {
        const key = JSON.stringify(entry.patch);
        groups.set(key, [...(groups.get(key) ?? []), { index, entry }]);
      });
      for (const group of groups.values()) {
        if (!group.length) continue;
        await mutateBulkUpdatePurchaseItems({
          input: {
            targets: group.map(({ entry: { itemId, baseRevision } }) => ({ itemId, baseRevision })),
            patch: group[0].entry.patch,
          },
        });
        group.forEach(({ index }) => completed.add(index));
      }
      setUndo(null);
      await refresh();
    } catch (reason) {
      setError((reason as Error).message);
      setUndo(undo.filter((_, index) => !completed.has(index)));
      await refresh();
    } finally {
      setPending(false);
    }
  };

  return {
    selectedItems,
    selectedCount: selectedItems.size,
    toggleSelection,
    clearSelection,
    category,
    setCategory,
    allocationsActive,
    setAllocationsActive,
    allocations,
    setAllocations,
    variantId,
    setVariantId,
    tags,
    setTags,
    tagsActive,
    setTagsActive,
    pending,
    error,
    setError,
    undo,
    apply,
    undoChanges,
  };
}
