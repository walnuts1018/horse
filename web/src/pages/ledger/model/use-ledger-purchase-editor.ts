import { useEffect, useRef, useState } from "react";

import type { LedgerItemDraft } from "./purchase-item-draft";

import { readProductCatalog } from "../../../entities/product";
import {
  mapUpdatePurchaseItemInput,
  purchaseOccurredAtLabel,
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
import { errorMessage, parseQuantity } from "../../../shared/lib";
import { readLedgerPurchase } from "../api/read-ledger-purchase";
import {
  createLedgerItemDraft,
  ledgerItemFieldPatch,
  rebaseLedgerItemDraft,
  reverseLedgerItemFieldPatch,
  type LedgerItemFieldPatch,
} from "./ledger-item-autosave";
import { createPurchaseDraft, type PurchaseDraft } from "./purchase-draft";
import { purchaseDraftTotalYen } from "./purchase-draft-total";
import { isLedgerPurchaseEditorDirty, ledgerPurchaseEditorSnapshot } from "./purchase-editor-dirty";

type PurchaseConflict = {
  label: string;
  localValue: string;
  serverValue: string;
  retry: () => Promise<void>;
  discard: () => Promise<void>;
};

type LedgerItemUndo = {
  purchaseId: string;
  itemId: string;
  patch: LedgerItemFieldPatch;
};

const sameNullableString = (left?: string | null, right?: string | null) =>
  (left ?? "") === (right ?? "");

function createLedgerItemDraftMap(purchase: Purchase) {
  return purchase.items.reduce<Record<string, LedgerItemDraft | undefined>>((drafts, item) => {
    drafts[item.id] = createLedgerItemDraft(item);
    return drafts;
  }, {});
}

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
  const [dirty, setDirty] = useState(false);
  const [savingPurchase, setSavingPurchase] = useState(false);
  const [postingPurchase, setPostingPurchase] = useState<string | null>(null);
  const [correctionTarget, setCorrectionTarget] = useState<{
    purchaseId: string;
    itemId: string;
  } | null>(null);
  const [conflict, setConflict] = useState<PurchaseConflict | null>(null);
  const conflictRef = useRef<PurchaseConflict | null>(null);
  const [itemUndo, setItemUndo] = useState<LedgerItemUndo | null>(null);
  const initialDraftSnapshot = useRef<string | null>(null);
  const latestPurchaseDraft = useRef<PurchaseDraft | null>(null);
  const latestItemDrafts = useRef(itemDrafts);
  const baselinePurchaseRef = useRef<Purchase | null>(null);
  const baselinePurchaseDraftRef = useRef<PurchaseDraft | null>(null);
  const baselineItemDraftsRef = useRef<Record<string, LedgerItemDraft | undefined>>({});
  const inlineItemIdRef = useRef<string | null>(null);
  const autoSaveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const savingPurchaseRef = useRef(false);

  const changeConflict = (next: PurchaseConflict | null) => {
    conflictRef.current = next;
    setConflict(next);
  };

  const updateDirtyState = () => {
    const nextDirty = isLedgerPurchaseEditorDirty(
      initialDraftSnapshot.current,
      latestPurchaseDraft.current,
      latestItemDrafts.current,
    );
    setDirty(nextDirty);
    onDirtyChange(nextDirty);
    return nextDirty;
  };

  const clearAutoSaveTimer = () => {
    if (autoSaveTimer.current) clearTimeout(autoSaveTimer.current);
    autoSaveTimer.current = null;
  };

  const reportDirty = (nextPurchaseDraft = purchaseDraft, nextItemDrafts = itemDrafts) => {
    latestPurchaseDraft.current = nextPurchaseDraft;
    latestItemDrafts.current = nextItemDrafts;
    const nextDirty = isLedgerPurchaseEditorDirty(
      initialDraftSnapshot.current,
      nextPurchaseDraft,
      nextItemDrafts,
    );
    setDirty(nextDirty);
    onDirtyChange(nextDirty);
  };

  const purchaseSaveState: "conflict" | "saving" | "unsaved" | "saved" = savingPurchase
    ? "saving"
    : conflict
      ? "conflict"
      : dirty
        ? "unsaved"
        : "saved";

  useEffect(() => {
    let active = true;
    void readProductCatalog(household.id)
      .then((products) => active && setCatalog(products))
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [household.id]);

  useEffect(
    () => () => {
      if (autoSaveTimer.current) clearTimeout(autoSaveTimer.current);
    },
    [],
  );

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
  const installServerBaseline = (purchase: Purchase, serverPurchaseDraft: PurchaseDraft | null) => {
    baselinePurchaseRef.current = purchase;
    if (serverPurchaseDraft) baselinePurchaseDraftRef.current = serverPurchaseDraft;
    baselineItemDraftsRef.current = createLedgerItemDraftMap(purchase);
    if (baselinePurchaseDraftRef.current)
      initialDraftSnapshot.current = ledgerPurchaseEditorSnapshot(
        baselinePurchaseDraftRef.current,
        baselineItemDraftsRef.current,
      );
  };

  const saveItemFields = async () => {
    const purchase = baselinePurchaseRef.current;
    const itemId = inlineItemIdRef.current;
    if (!purchase || !itemId || savingPurchaseRef.current || conflictRef.current) return;
    const baselineItem = baselineItemDraftsRef.current[itemId];
    const submittedDraft = latestItemDrafts.current[itemId];
    if (!baselineItem || !submittedDraft) return;
    const patch = ledgerItemFieldPatch(baselineItem, submittedDraft);
    if (!Object.keys(patch).length) return;
    if (!submittedDraft.name.trim()) {
      setEditError("品名を入力してください");
      return;
    }
    const item = purchase.items.find((candidate) => candidate.id === itemId);
    if (!item || purchase.status === "CLOSED") return;
    const input = mapUpdatePurchaseItemInput({
      itemId,
      baseRevision: item.revision,
      ...patch,
    });
    const latestItemDraftsAtStart = { ...latestItemDrafts.current };
    savingPurchaseRef.current = true;
    setSavingPurchase(true);
    setEditError("");
    let failed = false;
    try {
      await mutateSavePurchaseItemEdits({
        input: {
          purchaseId: purchase.id,
          purchaseBaseRevision: purchase.revision,
          items: [input],
        },
      });
      const latest = await latestPurchase(purchase.id);
      if (!latest) throw new Error("保存後の購入記録を取得できませんでした");
      await refresh();
      const currentDrafts = latestItemDrafts.current;
      const rebasedDrafts = createLedgerItemDraftMap(latest);
      for (const latestItem of latest.items) {
        const previousBaseline = baselineItemDraftsRef.current[latestItem.id];
        const submitted = latestItemDraftsAtStart[latestItem.id];
        const current = currentDrafts[latestItem.id];
        const serverDraft = rebasedDrafts[latestItem.id];
        if (previousBaseline && submitted && current && serverDraft)
          rebasedDrafts[latestItem.id] = rebaseLedgerItemDraft(
            current,
            submitted,
            previousBaseline,
            serverDraft,
          );
      }
      installServerBaseline(latest, baselinePurchaseDraftRef.current);
      latestItemDrafts.current = rebasedDrafts;
      setItemDrafts(rebasedDrafts);
      updateDirtyState();
      const inversePatch = reverseLedgerItemFieldPatch(patch, baselineItem);
      setItemUndo({ purchaseId: purchase.id, itemId, patch: inversePatch });
      changeConflict(null);
      setEditError("");
    } catch (reason) {
      failed = true;
      if (
        (reason instanceof ApiError && reason.code === "CONFLICT") ||
        errorMessage(reason).toLowerCase().includes("conflict")
      ) {
        try {
          const latest = await latestPurchase(purchase.id);
          await refresh();
          if (latest) {
            installServerBaseline(latest, baselinePurchaseDraftRef.current);
            updateDirtyState();
            const serverItem = latest.items.find((candidate) => candidate.id === itemId);
            const serverDraft = serverItem ? createLedgerItemDraft(serverItem) : null;
            changeConflict({
              label: item.name,
              localValue: [patch.name, patch.categoryId]
                .filter((value) => value !== undefined)
                .join(" · "),
              serverValue: serverDraft
                ? [serverDraft.name, serverDraft.categoryId || "未分類"].join(" · ")
                : "記録なし",
              retry: async () => {
                changeConflict(null);
                setEditError("");
                await saveItemFields();
              },
              discard: async () => {
                clearAutoSaveTimer();
                changeConflict(null);
                setEditing(null);
                inlineItemIdRef.current = null;
                initialDraftSnapshot.current = null;
                latestPurchaseDraft.current = null;
                setDirty(false);
                onDirtyChange(false);
              },
            });
          } else setEditError(errorMessage(reason));
        } catch {
          setEditError(`${errorMessage(reason)}（最新の記録を取得できませんでした）`);
        }
      } else setEditError(errorMessage(reason));
    } finally {
      savingPurchaseRef.current = false;
      setSavingPurchase(false);
      const pendingPatch = ledgerItemFieldPatch(
        baselineItemDraftsRef.current[itemId] ?? baselineItem,
        latestItemDrafts.current[itemId] ?? submittedDraft,
      );
      if (!failed && (pendingPatch.name || pendingPatch.categoryId !== undefined))
        scheduleItemAutoSave();
    }
  };

  function scheduleItemAutoSave() {
    if (!inlineItemIdRef.current || conflictRef.current) return;
    clearAutoSaveTimer();
    autoSaveTimer.current = setTimeout(() => {
      autoSaveTimer.current = null;
      void saveItemFields();
    }, 650);
  }

  const undoItemFields = async () => {
    const undo = itemUndo;
    const purchase = baselinePurchaseRef.current;
    const hasUnsavedChanges = isLedgerPurchaseEditorDirty(
      initialDraftSnapshot.current,
      latestPurchaseDraft.current,
      latestItemDrafts.current,
    );
    if (!undo || !purchase || hasUnsavedChanges || savingPurchaseRef.current || conflictRef.current)
      return;
    const item = purchase.items.find((candidate) => candidate.id === undo.itemId);
    if (!item || purchase.id !== undo.purchaseId || purchase.status === "CLOSED") return;
    const submittedDrafts = latestItemDrafts.current;
    const baselineBefore = baselineItemDraftsRef.current;
    savingPurchaseRef.current = true;
    setSavingPurchase(true);
    setEditError("");
    try {
      await mutateSavePurchaseItemEdits({
        input: {
          purchaseId: purchase.id,
          purchaseBaseRevision: purchase.revision,
          items: [
            mapUpdatePurchaseItemInput({
              itemId: item.id,
              baseRevision: item.revision,
              ...undo.patch,
            }),
          ],
        },
      });
      const latest = await latestPurchase(purchase.id);
      if (!latest) throw new Error("保存後の購入記録を取得できませんでした");
      await refresh();
      const rebasedDrafts = createLedgerItemDraftMap(latest);
      for (const latestItem of latest.items) {
        const before = baselineBefore[latestItem.id];
        const submitted = submittedDrafts[latestItem.id];
        const current = latestItemDrafts.current[latestItem.id];
        const server = rebasedDrafts[latestItem.id];
        if (before && submitted && current && server)
          rebasedDrafts[latestItem.id] = rebaseLedgerItemDraft(current, submitted, before, server);
      }
      installServerBaseline(latest, baselinePurchaseDraftRef.current);
      latestItemDrafts.current = rebasedDrafts;
      setItemDrafts(rebasedDrafts);
      setItemUndo(null);
      updateDirtyState();
    } catch (reason) {
      setEditError(errorMessage(reason));
      try {
        await refresh();
      } catch {
        setEditError(`${errorMessage(reason)}（最新の記録を取得できませんでした）`);
      }
    } finally {
      savingPurchaseRef.current = false;
      setSavingPurchase(false);
    }
  };

  const startEditing = (purchase: Purchase, inlineItemId?: string) => {
    clearAutoSaveTimer();
    const nextPurchaseDraft = createPurchaseDraft(purchase);
    const nextItemDrafts = createLedgerItemDraftMap(purchase);
    installServerBaseline(purchase, nextPurchaseDraft);
    initialDraftSnapshot.current = ledgerPurchaseEditorSnapshot(nextPurchaseDraft, nextItemDrafts);
    latestPurchaseDraft.current = nextPurchaseDraft;
    latestItemDrafts.current = nextItemDrafts;
    baselineItemDraftsRef.current = nextItemDrafts;
    baselinePurchaseDraftRef.current = nextPurchaseDraft;
    inlineItemIdRef.current = inlineItemId ?? null;
    setItemUndo(null);
    setDirty(false);
    onDirtyChange(false);
    setEditing(purchase.id);
    setPurchaseDraft(nextPurchaseDraft);
    setItemDrafts(nextItemDrafts);
    changeConflict(null);
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
    if (inlineItemIdRef.current === itemId && ("name" in updates || "categoryId" in updates)) {
      scheduleItemAutoSave();
    }
  };

  const cancelEditing = () => {
    if (savingPurchaseRef.current) return false;
    const hasUnsavedChanges = isLedgerPurchaseEditorDirty(
      initialDraftSnapshot.current,
      latestPurchaseDraft.current,
      latestItemDrafts.current,
    );
    if (hasUnsavedChanges && !window.confirm("未保存の変更を破棄しますか？")) return false;
    clearAutoSaveTimer();
    inlineItemIdRef.current = null;
    setEditing(null);
    initialDraftSnapshot.current = null;
    latestPurchaseDraft.current = null;
    baselinePurchaseRef.current = null;
    baselinePurchaseDraftRef.current = null;
    baselineItemDraftsRef.current = {};
    setItemUndo(null);
    setDirty(false);
    onDirtyChange(false);
    return true;
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
    if (!purchaseDraft || savingPurchaseRef.current) return;
    clearAutoSaveTimer();
    if (baselinePurchaseRef.current?.id === purchase.id) purchase = baselinePurchaseRef.current;
    const paymentAllocations: PurchasePaymentInput[] = purchaseDraft.paymentAllocations.map(
      (payment) => ({
        paymentAccountId: payment.paymentAccountId,
        amountYen: Number(payment.amountYen),
      }),
    );
    const totalYen = purchaseDraftTotalYen(purchase, itemDrafts);
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
      occurredAt:
        purchaseDraft.datePrecision === "DATE_ONLY"
          ? new Date(`${purchaseDraft.occurredAt}T12:00:00+09:00`).toISOString()
          : new Date(purchaseDraft.occurredAt).toISOString(),
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
      setDirty(false);
      onDirtyChange(false);
      changeConflict(null);
      setEditError("");
      return;
    }
    savingPurchaseRef.current = true;
    setSavingPurchase(true);
    let failed = false;
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
      const latest = await latestPurchase(purchase.id);
      if (latest) installServerBaseline(latest, purchaseDraft);
      const savedSnapshot = ledgerPurchaseEditorSnapshot(purchaseDraft, itemDrafts);
      initialDraftSnapshot.current = savedSnapshot;
      const hasLaterChanges = isLedgerPurchaseEditorDirty(
        savedSnapshot,
        latestPurchaseDraft.current,
        latestItemDrafts.current,
      );
      setDirty(hasLaterChanges);
      onDirtyChange(hasLaterChanges);
      changeConflict(null);
      setItemUndo(null);
      setEditError("");
    } catch (reason) {
      failed = true;
      if (
        (reason instanceof ApiError && reason.code === "CONFLICT") ||
        errorMessage(reason).toLowerCase().includes("conflict")
      ) {
        const latest = await latestPurchase(purchase.id);
        await refresh();
        if (latest)
          changeConflict({
            label: "買い物と品目",
            localValue: `${patch.merchant} · ${purchaseOccurredAtLabel(patch.occurredAt, purchase.datePrecision)}`,
            serverValue: `${latest.merchant} · ${purchaseOccurredAtLabel(latest.occurredAt, latest.datePrecision)}`,
            retry: async () => {
              const latestItemRevisions = new Map(
                latest.items.map((item) => [item.id, item.revision]),
              );
              setSavingPurchase(true);
              savingPurchaseRef.current = true;
              try {
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
                const refreshed = await latestPurchase(latest.id);
                if (refreshed) installServerBaseline(refreshed, purchaseDraft);
                const savedSnapshot = ledgerPurchaseEditorSnapshot(purchaseDraft, itemDrafts);
                initialDraftSnapshot.current = savedSnapshot;
                const hasLaterChanges = isLedgerPurchaseEditorDirty(
                  savedSnapshot,
                  latestPurchaseDraft.current,
                  latestItemDrafts.current,
                );
                setDirty(hasLaterChanges);
                changeConflict(null);
                onDirtyChange(hasLaterChanges);
              } finally {
                savingPurchaseRef.current = false;
                setSavingPurchase(false);
              }
            },
            discard: async () => {
              await refresh();
              changeConflict(null);
              setEditing(null);
              inlineItemIdRef.current = null;
              initialDraftSnapshot.current = null;
              latestPurchaseDraft.current = null;
              baselinePurchaseRef.current = null;
              baselinePurchaseDraftRef.current = null;
              baselineItemDraftsRef.current = {};
              setDirty(false);
              onDirtyChange(false);
            },
          });
      } else setEditError(errorMessage(reason));
    } finally {
      savingPurchaseRef.current = false;
      setSavingPurchase(false);
      if (inlineItemIdRef.current) {
        const itemId = inlineItemIdRef.current;
        const baseline = baselineItemDraftsRef.current[itemId];
        const draft = latestItemDrafts.current[itemId];
        const pendingPatch = baseline && draft ? ledgerItemFieldPatch(baseline, draft) : {};
        if (
          !failed &&
          !conflictRef.current &&
          (pendingPatch.name !== undefined || pendingPatch.categoryId !== undefined)
        )
          scheduleItemAutoSave();
      }
    }
  };

  return {
    catalog,
    conflict,
    correctionTarget,
    editError,
    editing,
    itemUndo,
    itemDrafts,
    postDraftPurchase,
    postingPurchase,
    purchaseSaveState,
    purchaseDraft,
    savePurchase,
    setConflict: changeConflict,
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
    undoItemFields,
  };
}
