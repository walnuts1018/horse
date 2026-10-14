import type { CreatePurchaseInput } from "../../../shared/api";
import type { DocumentReview, ExtractedItem, ReceiptPaymentDraft } from "./imports";

type ReceiptConfirmationInput = {
  document: DocumentReview;
  householdId: string;
  occurredAt: string | null;
  candidateId: string;
  requiresCandidateSelection: boolean;
  requiresOverrideReason: boolean;
  overrideReason: string;
  paymentDraft: ReceiptPaymentDraft[];
  accountCount: number;
  manualAmounts: Record<string, number> | undefined;
  allocations: Record<string, Array<{ poolId: string; weight: number }>> | undefined;
  suggestedCategoryIds: Array<string | undefined>;
};

export function effectiveReceiptItemAmount(
  item: ExtractedItem,
  index: number,
  candidateAmounts: number[] | undefined,
  manualAmountOverrides: Record<string, number> | undefined,
) {
  const itemKey = item.key ?? `item-${index}`;
  return manualAmountOverrides?.[itemKey] ?? candidateAmounts?.[index] ?? item.grossAmount;
}

export function createReceiptPurchaseInput({
  document,
  householdId,
  occurredAt,
  candidateId,
  requiresCandidateSelection,
  requiresOverrideReason,
  overrideReason,
  paymentDraft,
  accountCount,
  manualAmounts,
  allocations,
  suggestedCategoryIds,
}: ReceiptConfirmationInput): Omit<CreatePurchaseInput, "idempotencyKey"> {
  const receipt = document.result?.receipt;
  const items = receipt?.items ?? [];
  const candidate = document.result?.candidates?.find((item) => item.id === candidateId);
  const candidateAmounts = candidate?.amounts;
  const itemAmounts = items.map((item, index) =>
    effectiveReceiptItemAmount(item, index, candidateAmounts, manualAmounts),
  );
  const totalYen = itemAmounts.reduce((total, amount) => total + amount, 0);

  if (!items.length) throw new Error("確定できる品目がありません");
  if (!occurredAt) throw new Error("購入日を入力してください");
  if (requiresCandidateSelection && !candidateId) throw new Error("税計算候補を選択してください");
  if (requiresOverrideReason && overrideReason.length < 3) {
    throw new Error("金額の訂正理由を入力してください");
  }
  if (
    accountCount > 0 &&
    (paymentDraft.some(
      (payment) =>
        !payment.paymentAccountId ||
        !Number.isSafeInteger(payment.amountYen) ||
        payment.amountYen < 0,
    ) ||
      paymentDraft.reduce((total, payment) => total + payment.amountYen, 0) !== totalYen)
  ) {
    throw new Error("決済内訳と購入額を一致させてください");
  }

  return {
    householdId,
    origin: "AI",
    sourceDocumentId: document.id,
    occurredAt,
    merchant: receipt?.merchant ?? "",
    paymentAllocations: paymentDraft
      .filter((payment) => payment.paymentAccountId && payment.amountYen > 0)
      .map((payment) => ({
        paymentAccountId: payment.paymentAccountId,
        amountYen: payment.amountYen,
      })),
    receiptCandidateId: candidateId || null,
    manualAmountOverrideReason: overrideReason || null,
    items: items.map((item, index) => {
      const itemKey = item.key ?? `item-${index}`;
      const itemAllocations = allocations?.[itemKey] ?? [];
      return {
        name: item.name,
        amountYen: itemAmounts[index],
        quantityNumerator: item.quantityNumerator,
        quantityDenominator: item.quantityDenominator,
        quantityUnit: item.quantityUnit,
        printedUnitPriceYen: item.printedUnitPriceYen,
        printedUnitPriceQuantityNumerator: item.printedUnitPriceQuantityNumerator,
        printedUnitPriceQuantityDenominator: item.printedUnitPriceQuantityDenominator,
        printedUnitPriceUnit: item.printedUnitPriceUnit,
        categoryId:
          item.categoryId != null ? item.categoryId || null : suggestedCategoryIds[index] || null,
        productVariantId: item.productVariantId || null,
        allocations: itemAllocations.length ? itemAllocations : null,
      };
    }),
  };
}
