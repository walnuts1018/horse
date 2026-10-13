import { useEffect, useRef, useState } from "react";

import type { Purchase } from "../../../shared/api";

import {
  useBulkUpdatePurchasesMutation,
  type BulkUpdatePurchasesMutation$variables,
} from "../../../entities/purchase";

type PurchaseBulkPatch = BulkUpdatePurchasesMutation$variables["input"]["targets"][number]["patch"];

type PurchaseBulkUndo = Array<{
  purchaseId: string;
  baseRevision: number;
  patch: PurchaseBulkPatch;
}>;

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
  const [selectedPurchases, setSelectedPurchases] = useState<Set<string>>(() => new Set());
  const selectionAnchor = useRef<string | null>(null);
  const [merchantActive, setMerchantActive] = useState(false);
  const [merchant, setMerchant] = useState("");
  const [occurredAtActive, setOccurredAtActive] = useState(false);
  const [occurredAt, setOccurredAt] = useState("");
  const [paymentAccountId, setPaymentAccountId] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [undo, setUndo] = useState<PurchaseBulkUndo | null>(null);

  useEffect(() => {
    setSelectedPurchases(new Set());
    selectionAnchor.current = null;
  }, [selectionKey]);
  useEffect(() => {
    setUndo(null);
    setError("");
    setMerchantActive(false);
    setMerchant("");
    setOccurredAtActive(false);
    setOccurredAt("");
    setPaymentAccountId("");
  }, [householdId]);

  const selectPurchase = (visibleIds: string[], id: string, shiftKey: boolean) => {
    const anchorIndex =
      shiftKey && selectionAnchor.current ? visibleIds.indexOf(selectionAnchor.current) : -1;
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
      setError("一括変更は100件までです");
      return;
    }
    const next = new Set(selectedPurchases);
    if (range) range.forEach((targetId) => next.add(targetId));
    else if (next.has(id)) next.delete(id);
    else next.add(id);
    if (!shiftKey) selectionAnchor.current = id;
    setError("");
    setSelectedPurchases(next);
  };

  const clearSelection = () => {
    setSelectedPurchases(new Set());
    selectionAnchor.current = null;
  };

  const apply = async () => {
    const targets = purchases.filter((purchase) => selectedPurchases.has(purchase.id));
    const changedFields = [
      ...(merchantActive ? ["merchant"] : []),
      ...(occurredAtActive ? ["occurredAt"] : []),
      ...(paymentAccountId ? ["paymentAllocations"] : []),
    ];
    if (!targets.length || !changedFields.length) return;
    const occurredAtValue = occurredAtActive ? new Date(occurredAt).toISOString() : undefined;
    setPending(true);
    setError("");
    setUndo(null);
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
      setUndo(undoEntries.length ? undoEntries : null);
      clearSelection();
      setMerchantActive(false);
      setMerchant("");
      setOccurredAtActive(false);
      setOccurredAt("");
      setPaymentAccountId("");
      await refresh();
    } catch (reason) {
      setError((reason as Error).message);
      setUndo(undoEntries.length ? undoEntries : null);
      clearSelection();
      await refresh();
    } finally {
      setPending(false);
    }
  };

  const undoChanges = async () => {
    if (!undo?.length) return;
    setPending(true);
    setError("");
    try {
      await mutateBulkUpdatePurchases({
        input: {
          targets: undo.map((entry) => ({
            purchaseId: entry.purchaseId,
            baseRevision: entry.baseRevision,
            patch: entry.patch,
          })),
        },
      });
      setUndo(null);
      await refresh();
    } catch (reason) {
      setError((reason as Error).message);
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
    merchantActive,
    setMerchantActive,
    merchant,
    setMerchant,
    occurredAtActive,
    setOccurredAtActive,
    occurredAt,
    setOccurredAt,
    paymentAccountId,
    setPaymentAccountId,
    pending,
    error,
    undo,
    apply,
    undoChanges,
  };
}

export type PurchaseBulkEditController = ReturnType<typeof usePurchaseBulkEdit>;
