import { useCallback, useState } from "react";

import type { Purchase } from "../../../shared/api";

import {
  useBulkUpdatePurchaseItemsMutation,
  type BulkUpdatePurchaseItemsMutation$variables,
} from "../../../entities/purchase";
import { errorMessage } from "../../../shared/lib";

type LedgerItemBulkPatch = BulkUpdatePurchaseItemsMutation$variables["input"]["patch"];

type LedgerItemBulkUndo = Array<{
  itemId: string;
  baseRevision: number;
  patch: LedgerItemBulkPatch;
}>;

// 選択状態はスコープ(世帯×フィルタ)ごとに持ち、スコープが一致しない値は導出値として破棄する
type LedgerItemSelectionState = {
  scope: string;
  ids: Set<string>;
  anchor: { context: string; id: string } | null;
};

// undoとエラーは世帯ごとに持ち、世帯が一致しない値は導出値として初期値に戻す
type LedgerItemBulkSessionState = {
  householdId: string;
  undo: LedgerItemBulkUndo | null;
  error: string;
};

const EMPTY_SELECTION_IDS = new Set<string>();

function createBulkSession(householdId: string): LedgerItemBulkSessionState {
  return { householdId, undo: null, error: "" };
}

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
  const { mutate: mutateBulkUpdatePurchaseItems } = useBulkUpdatePurchaseItemsMutation();
  const selectionScope = `${householdId}\u0000${selectionKey}`;
  const [selection, setSelection] = useState<LedgerItemSelectionState>(() => ({
    scope: selectionScope,
    ids: new Set<string>(),
    anchor: null,
  }));
  const currentSelection: LedgerItemSelectionState =
    selection.scope === selectionScope
      ? selection
      : { scope: selectionScope, ids: EMPTY_SELECTION_IDS, anchor: null };
  const selectedItems = currentSelection.ids;
  const [session, setSession] = useState<LedgerItemBulkSessionState>(() =>
    createBulkSession(householdId),
  );
  const currentSession: LedgerItemBulkSessionState =
    session.householdId === householdId ? session : createBulkSession(householdId);
  const [category, setCategory] = useState("");
  const [allocationsActive, setAllocationsActive] = useState(false);
  const [allocations, setAllocations] = useState<Record<string, string | undefined>>({});
  const [variantId, setVariantId] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [tagsActive, setTagsActive] = useState(false);
  const [pending, setPending] = useState(false);

  const patchSession = (patch: Partial<LedgerItemBulkSessionState>) => {
    setSession((current) => ({
      ...(current.householdId === householdId ? current : createBulkSession(householdId)),
      ...patch,
    }));
  };
  // effectの依存配列で使われるため、世帯が変わるときだけ参照を更新する
  const setError = useCallback(
    (message: string) => {
      setSession((current) => ({
        ...(current.householdId === householdId ? current : createBulkSession(householdId)),
        error: message,
      }));
    },
    [householdId],
  );

  const clearSelection = () => {
    setSelection({ scope: selectionScope, ids: new Set<string>(), anchor: null });
  };
  const toggleSelection = (
    context: string,
    visibleIds: string[],
    id: string,
    shiftKey: boolean,
  ) => {
    const anchorIndex =
      shiftKey && currentSelection.anchor?.context === context
        ? visibleIds.indexOf(currentSelection.anchor.id)
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
      patchSession({ error: "一括変更は100品目までです" });
      return;
    }
    const next = new Set(selectedItems);
    if (range) range.forEach((targetId) => next.add(targetId));
    else if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelection({
      scope: selectionScope,
      ids: next,
      anchor: shiftKey ? currentSelection.anchor : { context, id },
    });
    patchSession({ error: "" });
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
    patchSession({ error: "", undo: null });
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
      const undoEntries: LedgerItemBulkUndo = targets.map(({ item }) => {
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
      patchSession({ undo: undoEntries.length ? undoEntries : null });
      clearSelection();
      setCategory("");
      setAllocationsActive(false);
      setAllocations({});
      setVariantId("");
      setTags([]);
      setTagsActive(false);
      await refresh();
    } catch (reason) {
      patchSession({ error: errorMessage(reason) });
      await refresh();
    } finally {
      setPending(false);
    }
  };

  const undoChanges = async () => {
    const undoEntries = currentSession.undo;
    if (!undoEntries?.length) return;
    setPending(true);
    patchSession({ error: "" });
    const completed = new Set<number>();
    try {
      const groups = new Map<string, Array<{ index: number; entry: LedgerItemBulkUndo[number] }>>();
      undoEntries.forEach((entry, index) => {
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
      patchSession({ undo: null });
      await refresh();
    } catch (reason) {
      patchSession({
        error: errorMessage(reason),
        undo: undoEntries.filter((_, index) => !completed.has(index)),
      });
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
    error: currentSession.error,
    setError,
    undo: currentSession.undo,
    apply,
    undoChanges,
  };
}
