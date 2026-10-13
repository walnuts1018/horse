import type {
  CreateAndPostPurchaseMutation$variables,
  CreatePurchaseInput,
} from "../../../shared/api";

export function mapCreateAndPostPurchaseInput(
  input: CreatePurchaseInput,
): CreateAndPostPurchaseMutation$variables["input"] {
  return {
    ...input,
    items: input.items.map((item) => {
      const {
        quantityNumerator,
        quantityDenominator,
        quantityUnit,
        printedUnitPriceYen,
        printedUnitPriceQuantityNumerator,
        printedUnitPriceQuantityDenominator,
        printedUnitPriceUnit,
        ...rest
      } = item;
      const hasQuantity =
        quantityNumerator != null || quantityDenominator != null || quantityUnit != null;
      const hasPrintedUnitPrice =
        printedUnitPriceYen != null ||
        printedUnitPriceQuantityNumerator != null ||
        printedUnitPriceQuantityDenominator != null ||
        printedUnitPriceUnit != null;
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
    }),
  };
}
