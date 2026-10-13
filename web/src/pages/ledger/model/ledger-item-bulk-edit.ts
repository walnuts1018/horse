import { useEffect, useRef, useState } from "react";
import { bulkUpdatePurchaseItemsMutation } from "../../../entities/purchase";
import { useGraphqlMutation, type Purchase } from "../../../shared/api";

type LedgerItemBulkUndo = Array<{
  itemId: string;
  baseRevision: number;
  patch: Record<string, unknown>;
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
  const { mutate } = useGraphqlMutation<{
    bulkUpdatePurchaseItems: Array<{ id: string; revision: number }>;
  }>(bulkUpdatePurchaseItemsMutation);
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
      const patch: Record<string, unknown> = {};
      if (category) patch.categoryId = category === "__uncategorized" ? null : category;
      if (allocationsActive)
        patch.allocations = Object.entries(allocations)
          .filter(([, weight]) => Number(weight) > 0)
          .map(([poolId, weight]) => ({ poolId, weight: Number(weight) }));
      if (variantId) patch.productVariantId = variantId === "__none" ? null : variantId;
      if (tagsActive) patch.tagIds = tags;
      const result = await mutate({
        input: {
          targets: targets.map(({ item }) => ({ itemId: item.id, baseRevision: item.revision })),
          patch,
        },
      });
      const undoEntries: NonNullable<typeof undo> = targets.map(({ item }) => {
        const undoPatch: Record<string, unknown> = {};
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
    let undone = 0;
    try {
      const groups = new Map<string, LedgerItemBulkUndo>();
      for (const entry of undo) {
        const key = JSON.stringify(entry.patch);
        groups.set(key, [...(groups.get(key) ?? []), entry]);
      }
      for (const group of groups.values()) {
        if (!group.length) continue;
        await mutate({
          input: {
            targets: group.map(({ itemId, baseRevision }) => ({ itemId, baseRevision })),
            patch: group[0].patch,
          },
        });
        undone += group.length;
      }
      setUndo(null);
      await refresh();
    } catch (reason) {
      setError((reason as Error).message);
      setUndo(undo.slice(undone));
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
