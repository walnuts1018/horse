import { useState } from "react";

import type { Purchase } from "../../../shared/api";

import {
  useBulkUpdatePurchasesMutation,
  type BulkUpdatePurchasesMutation$variables,
} from "../../../entities/purchase";
import { errorMessage } from "../../../shared/lib";

type PurchaseBulkPatch = BulkUpdatePurchasesMutation$variables["input"]["targets"][number]["patch"];

type PurchaseBulkUndo = Array<{
  purchaseId: string;
  baseRevision: number;
  patch: PurchaseBulkPatch;
}>;

// 選択状態はスコープ(世帯×フィルタ)ごとに持ち、スコープが一致しない値は導出値として破棄する
type PurchaseSelectionState = {
  scope: string;
  ids: Set<string>;
  anchor: string | null;
};

// フォーム状態は世帯ごとに持ち、世帯が一致しない値は導出値として初期値に戻す
type PurchaseBulkFormState = {
  householdId: string;
  merchantActive: boolean;
  merchant: string;
  occurredAtActive: boolean;
  occurredAt: string;
  paymentAccountId: string;
  undo: PurchaseBulkUndo | null;
  error: string;
};

const EMPTY_SELECTION_IDS = new Set<string>();

function createBulkForm(householdId: string): PurchaseBulkFormState {
  return {
    householdId,
    merchantActive: false,
    merchant: "",
    occurredAtActive: false,
    occurredAt: "",
    paymentAccountId: "",
    undo: null,
    error: "",
  };
}

export function usePurchaseBulkEdit({
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
  const { mutate: mutateBulkUpdatePurchases } = useBulkUpdatePurchasesMutation();
  const selectionScope = `${householdId}\u0000${selectionKey}`;
  const [selection, setSelection] = useState<PurchaseSelectionState>(() => ({
    scope: selectionScope,
    ids: new Set<string>(),
    anchor: null,
  }));
  const currentSelection: PurchaseSelectionState =
    selection.scope === selectionScope
      ? selection
      : { scope: selectionScope, ids: EMPTY_SELECTION_IDS, anchor: null };
  const selectedPurchases = currentSelection.ids;
  const [form, setForm] = useState<PurchaseBulkFormState>(() => createBulkForm(householdId));
  const currentForm: PurchaseBulkFormState =
    form.householdId === householdId ? form : createBulkForm(householdId);
  const [pending, setPending] = useState(false);

  const patchForm = (patch: Partial<PurchaseBulkFormState>) => {
    setForm((current) => ({
      ...(current.householdId === householdId ? current : createBulkForm(householdId)),
      ...patch,
    }));
  };

  const selectPurchase = (visibleIds: string[], id: string, shiftKey: boolean) => {
    const anchorIndex =
      shiftKey && currentSelection.anchor ? visibleIds.indexOf(currentSelection.anchor) : -1;
    const targetIndex = visibleIds.indexOf(id);
    const range =
      anchorIndex >= 0 && targetIndex >= 0
        ? visibleIds.slice(
            Math.min(anchorIndex, targetIndex),
            Math.max(anchorIndex, targetIndex) + 1,
          )
        : null;
    const additions = range?.filter((targetId) => !selectedPurchases.has(targetId)) ?? [];
    if (selectedPurchases.size + additions.length > 100) {
      patchForm({ error: "一括変更は100件までです" });
      return;
    }
    const next = new Set(selectedPurchases);
    if (range) range.forEach((targetId) => next.add(targetId));
    else if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelection({
      scope: selectionScope,
      ids: next,
      anchor: shiftKey ? currentSelection.anchor : id,
    });
    patchForm({ error: "" });
  };

  const clearSelection = () => {
    setSelection({ scope: selectionScope, ids: new Set<string>(), anchor: null });
  };

  const apply = async () => {
    const { merchantActive, merchant, occurredAtActive, occurredAt, paymentAccountId } =
      currentForm;
    const targets = purchases.filter((purchase) => selectedPurchases.has(purchase.id));
    const changedFields = [
      ...(merchantActive ? ["merchant"] : []),
      ...(occurredAtActive ? ["occurredAt"] : []),
      ...(paymentAccountId ? ["paymentAllocations"] : []),
    ];
    if (!targets.length || !changedFields.length) return;
    const occurredAtValue = occurredAtActive ? new Date(occurredAt).toISOString() : undefined;
    setPending(true);
    patchForm({ error: "", undo: null });
    const undoEntries: PurchaseBulkUndo = [];
    try {
      const result = await mutateBulkUpdatePurchases({
        input: {
          targets: targets.map((purchase) => ({
            purchaseId: purchase.id,
            baseRevision: purchase.revision,
            patch: {
              ...(merchantActive ? { merchant: merchant.trim() } : {}),
              ...(occurredAtValue ? { occurredAt: occurredAtValue } : {}),
              ...(paymentAccountId
                ? {
                    paymentAllocations: [{ paymentAccountId, amountYen: purchase.totalYen }],
                  }
                : {}),
            },
          })),
        },
      });
      for (const purchase of targets) {
        const patch: PurchaseBulkPatch = {};
        if (merchantActive) patch.merchant = purchase.merchant;
        if (occurredAtActive) patch.occurredAt = purchase.occurredAt;
        if (paymentAccountId)
          patch.paymentAllocations = purchase.paymentAllocations.map(
            ({ paymentAccountId: previousAccountId, amountYen }) => ({
              paymentAccountId: previousAccountId,
              amountYen,
            }),
          );
        undoEntries.push({
          purchaseId: purchase.id,
          baseRevision:
            result.bulkUpdatePurchases.find((updated) => updated.id === purchase.id)?.revision ??
            purchase.revision + 1,
          patch,
        });
      }
      setForm({ ...createBulkForm(householdId), undo: undoEntries.length ? undoEntries : null });
      clearSelection();
      await refresh();
    } catch (reason) {
      patchForm({ error: errorMessage(reason), undo: undoEntries.length ? undoEntries : null });
      clearSelection();
      await refresh();
    } finally {
      setPending(false);
    }
  };

  const undoChanges = async () => {
    const undoEntries = currentForm.undo;
    if (!undoEntries?.length) return;
    setPending(true);
    patchForm({ error: "" });
    try {
      await mutateBulkUpdatePurchases({
        input: {
          targets: undoEntries.map((entry) => ({
            purchaseId: entry.purchaseId,
            baseRevision: entry.baseRevision,
            patch: entry.patch,
          })),
        },
      });
      patchForm({ undo: null });
      await refresh();
    } catch (reason) {
      patchForm({ error: errorMessage(reason) });
      await refresh();
    } finally {
      setPending(false);
    }
  };

  return {
    selectedPurchases,
    selectedCount: selectedPurchases.size,
    selectPurchase,
    clearSelection,
    merchantActive: currentForm.merchantActive,
    setMerchantActive: (value: boolean) => patchForm({ merchantActive: value }),
    merchant: currentForm.merchant,
    setMerchant: (value: string) => patchForm({ merchant: value }),
    occurredAtActive: currentForm.occurredAtActive,
    setOccurredAtActive: (value: boolean) => patchForm({ occurredAtActive: value }),
    occurredAt: currentForm.occurredAt,
    setOccurredAt: (value: string) => patchForm({ occurredAt: value }),
    paymentAccountId: currentForm.paymentAccountId,
    setPaymentAccountId: (value: string) => patchForm({ paymentAccountId: value }),
    pending,
    error: currentForm.error,
    undo: currentForm.undo,
    apply,
    undoChanges,
  };
}

export type PurchaseBulkEditController = ReturnType<typeof usePurchaseBulkEdit>;
