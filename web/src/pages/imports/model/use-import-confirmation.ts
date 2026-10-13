import {
  useEffect,
  useState,
  type Dispatch,
  type MutableRefObject,
  type SetStateAction,
} from "react";

import type { Household } from "../../../shared/api";
import type {
  DocumentReview,
  ExtractedItem,
  ReceiptPaymentDraft,
  ReviewTarget,
  StatementEntry,
} from "./imports";

import { useCreateAndPostPurchase, useHouseholdPurchases } from "../../../entities/purchase";
import { importIdempotencyKey } from "../api/documents";
import {
  receiptDateInputValue,
  receiptOccurredAt,
  persistedStatementConfirmationKeys,
  statementConfirmationIssue,
  statementSourceReference,
} from "./imports";

export function useImportConfirmation({
  household,
  selectedDocument,
  setDocuments,
  editedReceiptDocuments,
  markUnsaved,
  setHasUnsavedChanges,
  onAdded,
}: {
  household: Household;
  selectedDocument: DocumentReview | undefined;
  setDocuments: Dispatch<SetStateAction<DocumentReview[]>>;
  editedReceiptDocuments: MutableRefObject<Set<string>>;
  markUnsaved: () => void;
  setHasUnsavedChanges: Dispatch<SetStateAction<boolean>>;
  onAdded: (keepOpen?: boolean) => void;
}) {
  const createAndPostPurchase = useCreateAndPostPurchase();
  const { purchases: recentPurchases, refresh: refreshPurchases } = useHouseholdPurchases(
    household.id,
    100,
  );
  const [locallyConfirmedStatements, setConfirmedStatements] = useState<Set<string>>(
    () => new Set(),
  );
  const confirmedStatements = new Set([
    ...locallyConfirmedStatements,
    ...persistedStatementConfirmationKeys(selectedDocument),
  ]);
  const [selectedCandidates, setSelectedCandidates] = useState<Record<string, string>>({});
  const [reviewTargetIndex, setReviewTargetIndex] = useState(0);
  const [manualAmountOverrides, setManualAmountOverrides] = useState<Record<string, boolean>>({});
  const [manualAmountOverrideReasons, setManualAmountOverrideReasons] = useState<
    Record<string, string>
  >({});
  const [receiptPayments, setReceiptPayments] = useState<Record<string, ReceiptPaymentDraft[]>>({});
  const [manualPurchaseDates, setManualPurchaseDates] = useState<Record<string, string>>({});
  const paymentAccountId = household.accounts[0]?.id ?? "";

  const normalizeName = (name: string) =>
    name.trim().replace(/\s+/g, " ").toLocaleLowerCase("ja-JP");
  const suggestedCategoryId = (item: ExtractedItem) => {
    const categories = household.categories.filter((category) => !category.archived);
    const hinted = categories.filter(
      (category) => normalizeName(category.name) === normalizeName(item.categoryHint ?? ""),
    );
    if (hinted.length === 1) return hinted[0].id;
    const matchingCategoryIds = new Set(
      recentPurchases
        .filter((purchase) => purchase.status !== "DRAFT")
        .flatMap((purchase) => purchase.items)
        .filter((purchaseItem) => normalizeName(purchaseItem.name) === normalizeName(item.name))
        .map((purchaseItem) => purchaseItem.categoryId)
        .filter((categoryId): categoryId is string => Boolean(categoryId)),
    );
    return matchingCategoryIds.size === 1 ? [...matchingCategoryIds][0] : "";
  };
  const receiptItemAmount = (document: DocumentReview, item: ExtractedItem, index: number) => {
    const candidateID = selectedCandidates[document.id] ?? "";
    const candidate = document.result?.candidates?.find((entry) => entry.id === candidateID);
    return manualAmountOverrides[document.id]
      ? (item.grossAmount ?? item.printedAmount)
      : (candidate?.amounts[index] ?? item.grossAmount ?? item.printedAmount);
  };
  const receiptTotal = (document: DocumentReview) =>
    (document.result?.receipt?.items ?? []).reduce(
      (total, item, index) => total + receiptItemAmount(document, item, index),
      0,
    );
  function paymentDraftFor(document: DocumentReview): ReceiptPaymentDraft[] {
    if (receiptPayments[document.id]) return receiptPayments[document.id];
    const recognizedPayments = document.result?.receipt?.payments ?? [];
    if (recognizedPayments.length) {
      return recognizedPayments.map((payment, index) => {
        const method = normalizeName(payment.method);
        const matchingAccounts = household.accounts.filter(
          (account) =>
            normalizeName(account.name) === method || normalizeName(account.kind) === method,
        );
        return {
          key: `recognized:${index}`,
          method: payment.method,
          cardLast4: payment.cardLast4,
          paymentAccountId: matchingAccounts.length === 1 ? matchingAccounts[0].id : "",
          amountYen: payment.amount,
        };
      });
    }
    return [
      {
        key: "default",
        paymentAccountId: household.accounts[0]?.id ?? "",
        amountYen: receiptTotal(document),
      },
    ];
  }

  const reviewTargets: ReviewTarget[] = selectedDocument?.result?.receipt?.items
    ? [
        ...selectedDocument.result.receipt.items.flatMap((item, index) =>
          item.verification.toUpperCase() === "UNRESOLVED" || item.confidence < 0.8
            ? [
                {
                  key: `item:${index}`,
                  label: `品目 ${index + 1}`,
                  elementId: `receipt-review-item-${index}`,
                },
              ]
            : [],
        ),
        ...(() => {
          const needsPurchaseDate = !receiptOccurredAt(
            selectedDocument.result?.receipt?.purchasedAt,
            manualPurchaseDates[selectedDocument.id],
          );
          const selectedCandidate = selectedDocument.result?.candidates?.find(
            (candidate) => candidate.id === selectedCandidates[selectedDocument.id],
          );
          const needsCandidateSelection =
            (selectedDocument.result?.candidates?.length ?? 0) > 0 && !selectedCandidate;
          const paymentsTotal = paymentDraftFor(selectedDocument).reduce(
            (total, payment) => total + payment.amountYen,
            0,
          );
          const needsAmountReview =
            manualAmountOverrides[selectedDocument.id] ||
            needsCandidateSelection ||
            (selectedCandidate
              ? !selectedCandidate.consistent
              : selectedDocument.result?.balanced === false);
          const needsPaymentReview =
            household.accounts.length > 0 && paymentsTotal !== receiptTotal(selectedDocument);
          return [
            ...(needsPurchaseDate
              ? [{ key: "date", label: "購入日", elementId: "receipt-review-purchase-date" }]
              : []),
            ...(needsAmountReview
              ? [
                  {
                    key: "amount",
                    label: "金額差異",
                    elementId: needsCandidateSelection
                      ? "receipt-review-candidates"
                      : "receipt-review-amount-reason",
                  },
                ]
              : []),
            ...(needsPaymentReview
              ? [{ key: "payments", label: "決済合計", elementId: "receipt-review-payments" }]
              : []),
          ];
        })(),
      ]
    : [];
  const activeReviewIndex = Math.min(reviewTargetIndex, Math.max(0, reviewTargets.length - 1));
  const activeReviewTarget = reviewTargets[activeReviewIndex];
  useEffect(() => {
    setReviewTargetIndex(0);
  }, [selectedDocument?.id]);
  useEffect(() => {
    if (!activeReviewTarget) return;
    const element = document.getElementById(activeReviewTarget.elementId);
    element?.scrollIntoView({ block: "center", behavior: "smooth" });
    if (element instanceof HTMLElement) element.focus({ preventScroll: true });
  }, [activeReviewTarget?.key, activeReviewTarget?.elementId]);

  const editItem = (documentId: string, index: number, patch: Partial<ExtractedItem>) => {
    editedReceiptDocuments.current.add(documentId);
    markUnsaved();
    setDocuments((current) =>
      current.map((document) =>
        document.id !== documentId || !document.result?.receipt?.items
          ? document
          : {
              ...document,
              result: {
                ...document.result,
                receipt: {
                  ...document.result.receipt,
                  items: document.result.receipt.items.map((item, itemIndex) => {
                    if (itemIndex !== index) return item;
                    const nameChanged = patch.name !== undefined && patch.name !== item.name;
                    return {
                      ...item,
                      ...patch,
                      ...(nameChanged ? { productVariantId: null } : {}),
                    };
                  }),
                },
              },
            },
      ),
    );
  };

  const editMerchant = (documentId: string, merchant: string) => {
    editedReceiptDocuments.current.add(documentId);
    markUnsaved();
    setDocuments((current) =>
      current.map((document) =>
        document.id === documentId && document.result
          ? {
              ...document,
              result: {
                ...document.result,
                receipt: { ...document.result.receipt, merchant },
              },
            }
          : document,
      ),
    );
  };

  const purchaseDateFor = (document: DocumentReview) =>
    manualPurchaseDates[document.id] ??
    receiptDateInputValue(document.result?.receipt?.purchasedAt);

  const editPurchaseDate = (documentId: string, date: string) => {
    editedReceiptDocuments.current.add(documentId);
    markUnsaved();
    setManualPurchaseDates((current) => ({ ...current, [documentId]: date }));
  };

  const confirm = async (document: DocumentReview) => {
    const receipt = document.result?.receipt;
    const occurredAt = receiptOccurredAt(receipt?.purchasedAt, manualPurchaseDates[document.id]);
    const items = receipt?.items ?? [];
    const candidateID = selectedCandidates[document.id] ?? "";
    const candidate = document.result?.candidates?.find((item) => item.id === candidateID);
    const overrideReason = manualAmountOverrideReasons[document.id]?.trim() ?? "";
    const requiresOverrideReason =
      manualAmountOverrides[document.id] ||
      (candidate ? !candidate.consistent : !document.result?.balanced);
    const paymentDraft = paymentDraftFor(document);
    const totalYen = items.reduce(
      (total, item, index) => total + receiptItemAmount(document, item, index),
      0,
    );
    if (!items.length) throw new Error("確定できる品目がありません");
    if (!occurredAt) throw new Error("購入日を入力してください");
    if (document.result?.candidates?.length && !selectedCandidates[document.id]) {
      throw new Error("税計算候補を選択してください");
    }
    if (requiresOverrideReason && overrideReason.length < 3) {
      throw new Error("金額の訂正理由を入力してください");
    }
    if (
      household.accounts.length > 0 &&
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
    const purchaseItems = items.map((item, index) => ({
      name: item.name,
      amountYen: receiptItemAmount(document, item, index),
      quantityNumerator: item.quantityNumerator,
      quantityDenominator: item.quantityDenominator,
      quantityUnit: item.quantityUnit,
      printedUnitPriceYen: item.printedUnitPriceYen,
      printedUnitPriceQuantityNumerator: item.printedUnitPriceQuantityNumerator,
      printedUnitPriceQuantityDenominator: item.printedUnitPriceQuantityDenominator,
      printedUnitPriceUnit: item.printedUnitPriceUnit,
      categoryId:
        item.categoryId != null ? item.categoryId || null : suggestedCategoryId(item) || null,
      productVariantId: item.productVariantId || null,
      allocations: null,
    }));
    await createAndPostPurchase({
      idempotencyKey: await importIdempotencyKey(document.id, "receipt"),
      householdId: household.id,
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
      receiptCandidateId: candidateID || null,
      manualAmountOverrideReason: overrideReason || null,
      items: purchaseItems,
    });
    editedReceiptDocuments.current.delete(document.id);
    setHasUnsavedChanges(false);
    await refreshPurchases();
    onAdded();
  };

  const confirmStatement = async (document: DocumentReview, entry: StatementEntry) => {
    if (!document.result || entry.kind !== "CARD_PURCHASE") return;
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    const reference = statementSourceReference(entry);
    const key = `${document.id}:${reference}`;
    if (confirmedStatements.has(key)) return;
    await createAndPostPurchase({
      idempotencyKey: await importIdempotencyKey(document.id, reference),
      householdId: household.id,
      origin: "AI",
      sourceDocumentId: document.id,
      sourceReference: reference,
      occurredAt: new Date(entry.occurredAt).toISOString(),
      merchant: entry.merchant,
      paymentAllocations: paymentAccountId ? [{ paymentAccountId, amountYen: entry.amount }] : [],
      items: [
        {
          name: entry.merchant || "カード利用",
          amountYen: entry.amount,
          allocations: null,
        },
      ],
    });
    await refreshPurchases();
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  return {
    recentPurchases,
    confirmedStatements,
    setConfirmedStatements,
    selectedCandidates,
    setSelectedCandidates,
    reviewTargetIndex,
    reviewTargets,
    activeReviewIndex,
    activeReviewTarget,
    setReviewTargetIndex,
    manualAmountOverrides,
    setManualAmountOverrides,
    manualAmountOverrideReasons,
    setManualAmountOverrideReasons,
    receiptPayments,
    setReceiptPayments,
    purchaseDateFor,
    editPurchaseDate,
    suggestedCategoryId,
    receiptItemAmount,
    receiptTotal,
    paymentDraftFor,
    editItem,
    editMerchant,
    confirm,
    confirmStatement,
  };
}
