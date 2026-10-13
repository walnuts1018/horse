export type LedgerItemDraft = {
  name: string;
  amountYen: string;
  quantity: string;
  quantityUnit: string;
  printedUnitPriceYen: string;
  printedBasisQuantity: string;
  printedUnitPriceUnit: string;
  categoryId: string;
  productVariantId: string;
  productLearning: "ITEM_ONLY" | "VARIANT_NAME" | "MERCHANT_ALIAS";
  variantName: string;
};
