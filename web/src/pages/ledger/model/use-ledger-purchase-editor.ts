import { useEffect, useRef, useState } from "react";

import type { LedgerItemDraft } from "./purchase-item-draft";

import { readProductCatalog } from "../../../entities/product";
import {
  mapUpdatePurchaseItemInput,
  usePostPurchaseMutation,
  useSavePurchaseItemEdits,
} from "../../../entities/purchase";
import {
  ApiError,
  makeIdempotencyKey,
  type Household,
  type ProductConcept,
  type Purchase,
  type PurchaseItem,
  type PurchasePaymentInput,
} from "../../../shared/api";
import { errorMessage, formatQuantity, parseQuantity } from "../../../shared/lib";
import { readLedgerPurchase } from "../api/read-ledger-purchase";
import { createPurchaseDraft, type PurchaseDraft } from "./purchase-draft";
import { isLedgerPurchaseEditorDirty, ledgerPurchaseEditorSnapshot } from "./purchase-editor-dirty";

type PurchaseConflict = {
  label: string;
  localValue: string;
  serverValue: string;
  retry: () => Promise<void>;
  discard: () => Promise<void>;
};

const sameNullableString = (left?: string | null, right?: string | null) =>
  (left ?? "") === (right ?? "");

export function useLedgerPurchaseEditor({
  household,
  refresh,
  onDirtyChange,
}: {
  household: Household;
  refresh: () => void | Promise<void>;
  onDirtyChange: (dirty: boolean) => void;
}) {
  const { mutate: mutatePostPurchase } = usePostPurchaseMutation();
  const mutateSavePurchaseItemEdits = useSavePurchaseItemEdits();
  const [editing, setEditing] = useState<string | null>(null);
  const [purchaseDraft, setPurchaseDraft] = useState<PurchaseDraft | null>(null);
  const [itemDrafts, setItemDrafts] = useState<Record<string, LedgerItemDraft | undefined>>({});
  const [catalog, setCatalog] = useState<ProductConcept[]>([]);
  const [editError, setEditError] = useState("");
  const [postingPurchase, setPostingPurchase] = useState<string | null>(null);
  const [correctionTarget, setCorrectionTarget] = useState<{
    purchaseId: string;
    itemId: string;
  } | null>(null);
  const [conflict, setConflict] = useState<PurchaseConflict | null>(null);
  const initialDraftSnapshot = useRef<string | null>(null);

  const reportDirty = (nextPurchaseDraft = purchaseDraft, nextItemDrafts = itemDrafts) => {
    onDirtyChange(
      isLedgerPurchaseEditorDirty(initialDraftSnapshot.current, nextPurchaseDraft, nextItemDrafts),
    );
  };

  useEffect(() => {
    let active = true;
    void readProductCatalog(household.id)
      .then((products) => active && setCatalog(products))
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [household.id]);

  const postDraftPurchase = async (purchase: Purchase) => {
    setPostingPurchase(purchase.id);
    setEditError("");
    try {
      await mutatePostPurchase({
        input: {
          purchaseId: purchase.id,
          expectedRevision: purchase.revision,
          idempotencyKey: makeIdempotencyKey(),
        },
      });
      await refresh();
    } catch (reason) {
      await refresh();
      setEditError(errorMessage(reason));
    } finally {
      setPostingPurchase(null);
    }
  };

  const latestPurchase = (purchaseId: string) => readLedgerPurchase(household.id, purchaseId);
  const startEditing = (purchase: Purchase) => {
    const nextPurchaseDraft = createPurchaseDraft(purchase);
    const nextItemDrafts = Object.fromEntries(
      purchase.items.map((item) => [
        item.id,
        {
          name: item.name,
          amountYen: String(item.amountYen),
          quantity: formatQuantity(item.quantityNumerator, item.quantityDenominator),
          quantityUnit: item.quantityUnit ?? "",
          printedUnitPriceYen:
            item.printedUnitPriceYen == null ? "" : String(item.printedUnitPriceYen),
          printedBasisQuantity: formatQuantity(
            item.printedUnitPriceQuantityNumerator,
            item.printedUnitPriceQuantityDenominator,
          ),
          printedUnitPriceUnit: item.printedUnitPriceUnit ?? "",
          categoryId: item.categoryId ?? "",
          productVariantId: item.productVariantId ?? "",
          allocations: item.allocations.map(({ poolId, weight }) => ({ poolId, weight })),
          productLearning: "ITEM_ONLY" as const,
          variantName: "",
        },
      ]),
    );
    initialDraftSnapshot.current = ledgerPurchaseEditorSnapshot(nextPurchaseDraft, nextItemDrafts);
    onDirtyChange(false);
    setEditing(purchase.id);
    setPurchaseDraft(nextPurchaseDraft);
    setItemDrafts(nextItemDrafts);
    setConflict(null);
    setEditError("");
  };

  const updatePurchaseDraft = (draft: PurchaseDraft) => {
    setPurchaseDraft(draft);
    reportDirty(draft);
  };

  const updateItemDraft = (itemId: string, updates: Partial<LedgerItemDraft>) => {
    const draft = itemDrafts[itemId];
    if (!draft) return;
    const nextDrafts = { ...itemDrafts, [itemId]: { ...draft, ...updates } };
    setItemDrafts(nextDrafts);
    reportDirty(purchaseDraft, nextDrafts);
  };

  const cancelEditing = () => {
    setEditing(null);
    initialDraftSnapshot.current = null;
    onDirtyChange(false);
  };

  const startCorrection = (purchase: Purchase, item: PurchaseItem) => {
    setCorrectionTarget({ purchaseId: purchase.id, itemId: item.id });
  };

  const buildItemEdits = (purchase: Purchase) => {
    const inputs = [];
    const defaultAllocations =
      household.defaultAllocation.length > 0
        ? household.defaultAllocation.map(({ poolId, weight }) => ({ poolId, weight }))
        : household.pools.map((pool) => ({
            poolId: pool.id,
            weight: pool.defaultShareWeight || 1,
          }));
    for (const item of purchase.items) {
      const draft = itemDrafts[item.id];
      if (!draft) continue;
      const amountYen = Number(draft.amountYen);
      const amountChanged = amountYen !== item.amountYen;
      if (
        !draft.name.trim() ||
        (amountChanged &&
          (!Number.isSafeInteger(amountYen) || amountYen < 0 || amountYen > 1_000_000_000_000))
      ) {
        setEditError("品名と金額を確認してください");
        return null;
      }
      let quantity;
      let printedBasis;
      try {
        quantity = parseQuantity(draft.quantity, draft.quantityUnit);
        printedBasis = parseQuantity(draft.printedBasisQuantity, draft.printedUnitPriceUnit);
      } catch (reason) {
        setEditError(errorMessage(reason));
        return null;
      }
      const printedPriceText = draft.printedUnitPriceYen.trim();
      const hasPrintedPrice = Boolean(
        printedPriceText || draft.printedBasisQuantity.trim() || draft.printedUnitPriceUnit.trim(),
      );
      const printedPrice = printedPriceText ? Number(printedPriceText.replace(/\D/g, "")) : null;
      if (
        hasPrintedPrice &&
        (!printedBasis || printedPrice === null || !Number.isSafeInteger(printedPrice))
      ) {
        setEditError("印字単価、基準量、単位を確認してください");
        return null;
      }
      const patch = {
        name: draft.name.trim(),
        amountYen,
        categoryId: draft.categoryId || null,
        productVariantId: draft.productVariantId || null,
      };
      const baseChangedFields: Array<keyof typeof patch> = [];
      if (patch.name !== item.name) baseChangedFields.push("name");
      if (patch.amountYen !== item.amountYen) baseChangedFields.push("amountYen");
      if (!sameNullableString(patch.categoryId, item.categoryId))
        baseChangedFields.push("categoryId");
      if (!sameNullableString(patch.productVariantId, item.productVariantId))
        baseChangedFields.push("productVariantId");
      const quantityChanged =
        !sameNullableString(quantity?.numerator, item.quantityNumerator) ||
        !sameNullableString(quantity?.denominator, item.quantityDenominator) ||
        !sameNullableString(quantity?.unit, item.quantityUnit);
      const printedPriceChanged =
        (printedPrice ?? null) !== (item.printedUnitPriceYen ?? null) ||
        !sameNullableString(printedBasis?.numerator, item.printedUnitPriceQuantityNumerator) ||
        !sameNullableString(printedBasis?.denominator, item.printedUnitPriceQuantityDenominator) ||
        !sameNullableString(printedBasis?.unit, item.printedUnitPriceUnit);
      const allocations = draft.allocations.length ? draft.allocations : defaultAllocations;
      const currentAllocations = item.allocations.map(({ poolId, weight }) => ({ poolId, weight }));
      const allocationsChanged =
        allocations.length !== currentAllocations.length ||
        allocations.some((allocation) =>
          currentAllocations.every(
            (current) =>
              current.poolId !== allocation.poolId || current.weight !== allocation.weight,
          ),
        );
      if (
        !baseChangedFields.length &&
        !quantityChanged &&
        !printedPriceChanged &&
        !allocationsChanged
      )
        continue;
      const changedPatch = Object.fromEntries(
        baseChangedFields.map((field) => [field, patch[field]]),
      );
      inputs.push(
        mapUpdatePurchaseItemInput({
          itemId: item.id,
          baseRevision: item.revision,
          ...changedPatch,
          ...(quantityChanged
            ? {
                quantityNumerator: quantity?.numerator ?? null,
                quantityDenominator: quantity?.denominator ?? null,
                quantityUnit: quantity?.unit ?? null,
              }
            : {}),
          ...(printedPriceChanged
            ? {
                printedUnitPriceYen: hasPrintedPrice ? printedPrice : null,
                printedUnitPriceQuantityNumerator: printedBasis?.numerator ?? null,
                printedUnitPriceQuantityDenominator: printedBasis?.denominator ?? null,
                printedUnitPriceUnit: printedBasis?.unit ?? null,
              }
            : {}),
          ...(allocationsChanged ? { allocations } : {}),
          ...(baseChangedFields.includes("productVariantId") && patch.productVariantId
            ? {
                productLearning:
                  draft.productLearning === "VARIANT_NAME"
                    ? { variantName: draft.variantName.trim() }
                    : draft.productLearning === "MERCHANT_ALIAS"
                      ? { merchantAlias: true }
                      : { itemOnly: true },
              }
            : {}),
        }),
      );
    }
    return inputs;
  };

  const savePurchase = async (purchase: Purchase) => {
    if (!purchaseDraft) return;
    const paymentAllocations: PurchasePaymentInput[] = purchaseDraft.paymentAllocations.map(
      (payment) => ({
        paymentAccountId: payment.paymentAccountId,
        amountYen: Number(payment.amountYen),
      }),
    );
    const totalYen = purchase.items.reduce(
      (total, item) => total + Number(itemDrafts[item.id]?.amountYen ?? item.amountYen),
      0,
    );
    const currentAllocations = purchase.paymentAllocations.map((payment) => ({
      paymentAccountId: payment.paymentAccountId,
      amountYen: payment.amountYen,
    }));
    const paymentAllocationsChanged =
      JSON.stringify(paymentAllocations) !== JSON.stringify(currentAllocations);
    if (
      paymentAllocationsChanged &&
      (paymentAllocations.some(
        (payment) =>
          !payment.paymentAccountId ||
          !Number.isSafeInteger(payment.amountYen) ||
          payment.amountYen < 1,
      ) ||
        paymentAllocations.reduce((total, payment) => total + payment.amountYen, 0) !== totalYen)
    ) {
      setEditError("決済元と金額を確認してください");
      return;
    }
    const patch = {
      merchant: purchaseDraft.merchant.trim(),
      occurredAt: new Date(purchaseDraft.occurredAt).toISOString(),
      paymentAllocations,
    };
    const purchasePatch = {
      ...(patch.merchant !== purchase.merchant ? { merchant: patch.merchant } : {}),
      ...(Date.parse(patch.occurredAt) !== Date.parse(purchase.occurredAt)
        ? { occurredAt: patch.occurredAt }
        : {}),
      ...(paymentAllocationsChanged ? { paymentAllocations } : {}),
    };
    const items = buildItemEdits(purchase);
    if (!items) return;
    const hasPurchaseChanges = Object.keys(purchasePatch).length > 0;
    if (!items.length && !hasPurchaseChanges) {
      setEditing(null);
      initialDraftSnapshot.current = null;
      onDirtyChange(false);
      setConflict(null);
      setEditError("");
      return;
    }
    try {
      await mutateSavePurchaseItemEdits({
        input: {
          purchaseId: purchase.id,
          purchaseBaseRevision: purchase.revision,
          ...(hasPurchaseChanges ? { purchasePatch } : {}),
          items,
        },
      });
      await refresh();
      setEditing(null);
      initialDraftSnapshot.current = null;
      onDirtyChange(false);
      setConflict(null);
      setEditError("");
    } catch (reason) {
      if (
        (reason instanceof ApiError && reason.code === "CONFLICT") ||
        errorMessage(reason).toLowerCase().includes("conflict")
      ) {
        const latest = await latestPurchase(purchase.id);
        await refresh();
        if (latest)
          setConflict({
            label: "買い物と品目",
            localValue: `${patch.merchant} · ${new Date(patch.occurredAt).toLocaleString("ja-JP")}`,
            serverValue: `${latest.merchant} · ${new Date(latest.occurredAt).toLocaleString("ja-JP")}`,
            retry: async () => {
              const latestItemRevisions = new Map(
                latest.items.map((item) => [item.id, item.revision]),
              );
              await mutateSavePurchaseItemEdits({
                input: {
                  purchaseId: latest.id,
                  purchaseBaseRevision: latest.revision,
                  ...(hasPurchaseChanges ? { purchasePatch } : {}),
                  items: items.map((item) =>
                    Object.assign({}, item, {
                      baseRevision: latestItemRevisions.get(item.itemId) ?? item.baseRevision,
                    }),
                  ),
                },
              });
              await refresh();
              setConflict(null);
              setEditing(null);
              initialDraftSnapshot.current = null;
              onDirtyChange(false);
            },
            discard: async () => {
              await refresh();
              setConflict(null);
              setEditing(null);
              initialDraftSnapshot.current = null;
              onDirtyChange(false);
            },
          });
      } else setEditError(errorMessage(reason));
    }
  };

  return {
    catalog,
    conflict,
    correctionTarget,
    editError,
    editing,
    itemDrafts,
    postDraftPurchase,
    postingPurchase,
    purchaseDraft,
    savePurchase,
    setConflict,
    setCorrectionTarget,
    setEditError,
    setEditing,
    setItemDrafts,
    setPurchaseDraft,
    startCorrection,
    startEditing,
    updateItemDraft,
    updatePurchaseDraft,
    cancelEditing,
  };
}
