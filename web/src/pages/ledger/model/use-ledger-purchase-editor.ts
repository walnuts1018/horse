import { useEffect, useState } from "react";

import type { LedgerItemDraft } from "./purchase-item-draft";

import { readProductCatalog } from "../../../entities/product";
import {
  mapUpdatePurchaseItemInput,
  usePostPurchaseMutation,
  useUpdatePurchaseMutation,
  useUpdatePurchaseItem,
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
import { formatQuantity, parseQuantity } from "../../../shared/lib";
import { readLedgerPurchase } from "../api/read-ledger-purchase";
import { createPurchaseDraft, type PurchaseDraft } from "./purchase-draft";

type PurchaseConflict = {
  label: string;
  localValue: string;
  serverValue: string;
  retry: () => Promise<void>;
  discard: () => Promise<void>;
};

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

const sameNullableString = (left?: string | null, right?: string | null) =>
  (left ?? "") === (right ?? "");

export function useLedgerPurchaseEditor({
  household,
  refresh,
}: {
  household: Household;
  refresh: () => void | Promise<void>;
}) {
  const { mutate: mutatePostPurchase } = usePostPurchaseMutation();
  const mutateUpdatePurchaseItem = useUpdatePurchaseItem();
  const { mutate: mutateUpdatePurchase } = useUpdatePurchaseMutation();
  const [editing, setEditing] = useState<string | null>(null);
  const [purchaseDraft, setPurchaseDraft] = useState<PurchaseDraft | null>(null);
  const [itemDrafts, setItemDrafts] = useState<Record<string, LedgerItemDraft>>({});
  const [catalog, setCatalog] = useState<ProductConcept[]>([]);
  const [editError, setEditError] = useState("");
  const [postingPurchase, setPostingPurchase] = useState<string | null>(null);
  const [correctionTarget, setCorrectionTarget] = useState<{
    purchaseId: string;
    itemId: string;
  } | null>(null);
  const [conflict, setConflict] = useState<PurchaseConflict | null>(null);

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
      setEditError((reason as Error).message);
    } finally {
      setPostingPurchase(null);
    }
  };

  const latestPurchase = (purchaseId: string) => readLedgerPurchase(household.id, purchaseId);
  const startEditing = (purchase: Purchase) => {
    setEditing(purchase.id);
    setPurchaseDraft(createPurchaseDraft(purchase));
    setItemDrafts(
      Object.fromEntries(
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
            productLearning: "ITEM_ONLY",
            variantName: "",
          },
        ]),
      ),
    );
    setConflict(null);
    setEditError("");
  };

  const startCorrection = (purchase: Purchase, item: PurchaseItem) => {
    setCorrectionTarget({ purchaseId: purchase.id, itemId: item.id });
  };

  const saveItem = async (purchase: Purchase, item: PurchaseItem) => {
    const draft = itemDrafts[item.id];
    if (!draft || !draft.name.trim() || Number(draft.amountYen) < 1) return;
    let quantity;
    let printedBasis;
    try {
      quantity = parseQuantity(draft.quantity, draft.quantityUnit);
      printedBasis = parseQuantity(draft.printedBasisQuantity, draft.printedUnitPriceUnit);
    } catch (reason) {
      setEditError((reason as Error).message);
      return;
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
      return;
    }
    const patch = {
      name: draft.name.trim(),
      amountYen: Number(draft.amountYen),
      categoryId: draft.categoryId || null,
      productVariantId: draft.productVariantId || null,
    };
    const baseChangedFields = [
      ...(patch.name !== item.name ? ["name"] : []),
      ...(patch.amountYen !== item.amountYen ? ["amountYen"] : []),
      ...(!sameNullableString(patch.categoryId, item.categoryId) ? ["categoryId"] : []),
      ...(!sameNullableString(patch.productVariantId, item.productVariantId)
        ? ["productVariantId"]
        : []),
    ] as Array<keyof typeof patch>;
    const quantityChanged =
      !sameNullableString(quantity?.numerator, item.quantityNumerator) ||
      !sameNullableString(quantity?.denominator, item.quantityDenominator) ||
      !sameNullableString(quantity?.unit, item.quantityUnit);
    const printedPriceChanged =
      (printedPrice ?? null) !== (item.printedUnitPriceYen ?? null) ||
      !sameNullableString(printedBasis?.numerator, item.printedUnitPriceQuantityNumerator) ||
      !sameNullableString(printedBasis?.denominator, item.printedUnitPriceQuantityDenominator) ||
      !sameNullableString(printedBasis?.unit, item.printedUnitPriceUnit);
    const changedFields = [
      ...baseChangedFields,
      ...(quantityChanged ? ["quantity"] : []),
      ...(printedPriceChanged ? ["printedUnitPrice"] : []),
    ];
    if (!changedFields.length) return;
    const changedPatch = Object.fromEntries(
      baseChangedFields.map((field) => [field, patch[field]]),
    );
    const inputForRevision = (baseRevision: number) =>
      mapUpdatePurchaseItemInput({
        itemId: item.id,
        baseRevision,
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
      });
    try {
      await mutateUpdatePurchaseItem({ input: inputForRevision(item.revision) });
      await refresh();
      setEditError("");
    } catch (reason) {
      if (
        (reason instanceof ApiError && reason.code === "CONFLICT") ||
        (reason as Error).message.toLowerCase().includes("conflict")
      ) {
        const latest = await latestPurchase(purchase.id);
        const current = latest?.items.find((row) => row.id === item.id);
        await refresh();
        if (current)
          setConflict({
            label: `${item.name}の品目`,
            localValue: changedFields
              .map((field) => `${field}: ${String(patch[field as keyof typeof patch] ?? "未分類")}`)
              .join("、"),
            serverValue: `${current.name} · ${yen(current.amountYen)}`,
            retry: async () => {
              await mutateUpdatePurchaseItem({ input: inputForRevision(current.revision) });
              await refresh();
              setConflict(null);
            },
            discard: async () => {
              await refresh();
              setConflict(null);
            },
          });
      } else setEditError((reason as Error).message);
    }
  };

  const savePurchase = async (purchase: Purchase) => {
    if (!purchaseDraft) return;
    const paymentAllocations: PurchasePaymentInput[] = purchaseDraft.paymentAllocations.map(
      (payment) => ({
        paymentAccountId: payment.paymentAccountId,
        amountYen: Number(payment.amountYen),
      }),
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
        paymentAllocations.reduce((total, payment) => total + payment.amountYen, 0) !==
          purchase.totalYen)
    ) {
      setEditError("決済元と金額を確認してください");
      return;
    }
    const patch = {
      merchant: purchaseDraft.merchant.trim(),
      occurredAt: new Date(purchaseDraft.occurredAt).toISOString(),
      paymentAllocations,
    };
    const changedFields = [
      ...(patch.merchant !== purchase.merchant ? ["merchant"] : []),
      ...(Date.parse(patch.occurredAt) !== Date.parse(purchase.occurredAt) ? ["occurredAt"] : []),
      ...(paymentAllocationsChanged ? ["paymentAllocations"] : []),
    ] as Array<keyof typeof patch>;
    if (!changedFields.length) return;
    const changedPatch = Object.fromEntries(changedFields.map((field) => [field, patch[field]]));
    try {
      await mutateUpdatePurchase({
        input: {
          purchaseId: purchase.id,
          baseRevision: purchase.revision,
          patch: changedPatch,
        },
      });
      await refresh();
      setEditing(null);
      setConflict(null);
      setEditError("");
    } catch (reason) {
      if (
        (reason instanceof ApiError && reason.code === "CONFLICT") ||
        (reason as Error).message.toLowerCase().includes("conflict")
      ) {
        const latest = await latestPurchase(purchase.id);
        await refresh();
        if (latest)
          setConflict({
            label: "買い物の情報",
            localValue: `${patch.merchant} · ${new Date(patch.occurredAt).toLocaleString("ja-JP")}`,
            serverValue: `${latest.merchant} · ${new Date(latest.occurredAt).toLocaleString("ja-JP")}`,
            retry: async () => {
              await mutateUpdatePurchase({
                input: {
                  purchaseId: latest.id,
                  baseRevision: latest.revision,
                  patch: changedPatch,
                },
              });
              await refresh();
              setConflict(null);
            },
            discard: async () => {
              await refresh();
              setConflict(null);
              setEditing(null);
            },
          });
      } else setEditError((reason as Error).message);
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
    saveItem,
    savePurchase,
    setConflict,
    setCorrectionTarget,
    setEditError,
    setEditing,
    setItemDrafts,
    setPurchaseDraft,
    startCorrection,
    startEditing,
  };
}
