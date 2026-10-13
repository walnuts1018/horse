import type { PurchaseItem, UpdatePurchaseItemMutation$variables } from "../../../shared/api";

type FlatPurchaseItemInput = Omit<
  UpdatePurchaseItemMutation$variables["input"],
  "quantity" | "printedUnitPrice"
> &
  Pick<
    PurchaseItem,
    | "quantityNumerator"
    | "quantityDenominator"
    | "quantityUnit"
    | "printedUnitPriceYen"
    | "printedUnitPriceQuantityNumerator"
    | "printedUnitPriceQuantityDenominator"
    | "printedUnitPriceUnit"
  >;

export function mapUpdatePurchaseItemInput(
  input: FlatPurchaseItemInput,
): UpdatePurchaseItemMutation$variables["input"] {
  const {
    quantityNumerator,
    quantityDenominator,
    quantityUnit,
    printedUnitPriceYen,
    printedUnitPriceQuantityNumerator,
    printedUnitPriceQuantityDenominator,
    printedUnitPriceUnit,
    ...rest
  } = input;
  const hasQuantity =
    "quantityNumerator" in input || "quantityDenominator" in input || "quantityUnit" in input;
  const hasPrintedUnitPrice =
    "printedUnitPriceYen" in input ||
    "printedUnitPriceQuantityNumerator" in input ||
    "printedUnitPriceQuantityDenominator" in input ||
    "printedUnitPriceUnit" in input;
  return {
    ...rest,
    ...(hasQuantity
      ? {
          quantity:
            quantityNumerator && quantityDenominator && quantityUnit
              ? {
                  numerator: quantityNumerator,
                  denominator: quantityDenominator,
                  unit: quantityUnit,
                }
              : null,
        }
      : {}),
    ...(hasPrintedUnitPrice
      ? {
          printedUnitPrice:
            printedUnitPriceYen != null &&
            printedUnitPriceQuantityNumerator &&
            printedUnitPriceQuantityDenominator &&
            printedUnitPriceUnit
              ? {
                  amountYen: printedUnitPriceYen,
                  basis: {
                    numerator: printedUnitPriceQuantityNumerator,
                    denominator: printedUnitPriceQuantityDenominator,
                    unit: printedUnitPriceUnit,
                  },
                }
              : null,
        }
      : {}),
  };
}
