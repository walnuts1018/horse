import type { PurchaseItem } from "../../../shared/api";
import type { PurchaseListFragment$data } from "../../../shared/api";

type PurchaseListItem =
  PurchaseListFragment$data["purchases"]["edges"][number]["node"]["items"][number];

export function mapPurchaseItem(item: PurchaseListItem): PurchaseItem {
  return {
    id: item.id,
    name: item.name,
    amountYen: item.amountYen,
    revision: item.revision,
    quantityNumerator: item.quantity?.numerator ?? null,
    quantityDenominator: item.quantity?.denominator ?? null,
    quantityUnit: item.quantity?.unit ?? null,
    printedUnitPriceYen: item.printedUnitPrice?.amountYen ?? null,
    printedUnitPriceQuantityNumerator: item.printedUnitPrice?.basis.numerator ?? null,
    printedUnitPriceQuantityDenominator: item.printedUnitPrice?.basis.denominator ?? null,
    printedUnitPriceUnit: item.printedUnitPrice?.basis.unit ?? null,
    effectiveUnitPriceNumeratorYen: item.effectiveUnitPrice?.numeratorYen ?? null,
    effectiveUnitPriceDenominator: item.effectiveUnitPrice?.denominator ?? null,
    categoryId: item.categoryId,
    productVariantId: item.productVariantId,
    allocations: item.allocations.map((allocation) => ({
      amountYen: allocation.amountYen,
      poolId: allocation.poolId,
      poolName: allocation.poolName,
      weight: allocation.weight,
    })),
    tags: item.tags.map((tag) => ({
      archived: tag.archived,
      householdId: tag.householdId,
      id: tag.id,
      name: tag.name,
      revision: tag.revision,
    })),
  };
}
