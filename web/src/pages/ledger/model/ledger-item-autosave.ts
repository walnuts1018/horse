import type { PurchaseItem } from "../../../shared/api";
import type { LedgerItemDraft } from "./purchase-item-draft";

import { formatQuantity } from "../../../shared/lib";

export type LedgerItemFieldPatch = {
  name?: string;
  categoryId?: string | null;
};

const ledgerItemDraftKeys = [
  "name",
  "amountYen",
  "quantity",
  "quantityUnit",
  "printedUnitPriceYen",
  "printedBasisQuantity",
  "printedUnitPriceUnit",
  "categoryId",
  "productVariantId",
  "allocations",
  "productLearning",
  "variantName",
] as const satisfies ReadonlyArray<keyof LedgerItemDraft>;

export function createLedgerItemDraft(item: PurchaseItem): LedgerItemDraft {
  return {
    name: item.name,
    amountYen: String(item.amountYen),
    quantity: formatQuantity(item.quantityNumerator, item.quantityDenominator),
    quantityUnit: item.quantityUnit ?? "",
    printedUnitPriceYen: item.printedUnitPriceYen == null ? "" : String(item.printedUnitPriceYen),
    printedBasisQuantity: formatQuantity(
      item.printedUnitPriceQuantityNumerator,
      item.printedUnitPriceQuantityDenominator,
    ),
    printedUnitPriceUnit: item.printedUnitPriceUnit ?? "",
    categoryId: item.categoryId ?? "",
    productVariantId: item.productVariantId ?? "",
    allocations: item.allocations.map(({ poolId, weight }) => ({ poolId, weight })),
    productLearning: "ITEM_ONLY",
    variantName: "",
  };
}

export function ledgerItemFieldPatch(
  baseline: LedgerItemDraft,
  draft: LedgerItemDraft,
): LedgerItemFieldPatch {
  const patch: LedgerItemFieldPatch = {};
  const name = draft.name.trim();
  if (name && name !== baseline.name) patch.name = name;
  if (draft.categoryId !== baseline.categoryId) patch.categoryId = draft.categoryId || null;
  return patch;
}

export function reverseLedgerItemFieldPatch(
  patch: LedgerItemFieldPatch,
  previous: LedgerItemDraft,
): LedgerItemFieldPatch {
  return {
    ...("name" in patch ? { name: previous.name } : {}),
    ...("categoryId" in patch ? { categoryId: previous.categoryId || null } : {}),
  };
}

export function rebaseLedgerItemDraft(
  current: LedgerItemDraft,
  submitted: LedgerItemDraft,
  previousBaseline: LedgerItemDraft,
  server: LedgerItemDraft,
): LedgerItemDraft {
  const rebased = { ...server };
  for (const key of ledgerItemDraftKeys) {
    const localValueNeedsPreservation =
      JSON.stringify(current[key]) !== JSON.stringify(previousBaseline[key]) ||
      JSON.stringify(current[key]) !== JSON.stringify(submitted[key]);
    if (localValueNeedsPreservation) Object.assign(rebased, { [key]: current[key] });
  }
  return rebased;
}
