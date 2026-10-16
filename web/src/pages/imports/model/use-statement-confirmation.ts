import type { Dispatch, SetStateAction } from "react";

import type { Household } from "../../../shared/api";

import {
  useCorrectPurchaseFromStatementMutation,
  useCreateAndPostPurchase,
} from "../../../entities/purchase";
import {
  importIdempotencyKey,
  linkStatementTransfer as linkStatementTransferRequest,
  linkStatementAccountMovement as linkStatementAccountMovementRequest,
  linkStatementRefundCorrection as linkStatementRefundCorrectionRequest,
  linkStatementAccountIncome as linkStatementAccountIncomeRequest,
  recordStatementAccountMovement,
  recordStatementExternalIncome,
  recordStatementLoan,
  recordStatementRepayment,
} from "../api/documents";
import {
  statementConfirmationIssue,
  statementSourceReference,
  type DocumentReview,
  type StatementEntry,
} from "./imports";
import {
  createStatementExternalIncomeInput,
  createStatementPurchaseInput,
  statementConfirmationKey,
  statementOccurredAt,
} from "./statement-confirmation";

type StatementConfirmationOptions = {
  household: Household;
  confirmedStatements: Set<string>;
  setConfirmedStatements: Dispatch<SetStateAction<Set<string>>>;
  createAndPostPurchase: ReturnType<typeof useCreateAndPostPurchase>;
  correctPurchaseFromStatement: ReturnType<
    typeof useCorrectPurchaseFromStatementMutation
  >["mutate"];
  refreshPurchases: () => void;
  setHasUnsavedChanges: (unsaved: boolean) => void;
  onAdded: (keepOpen?: boolean) => void;
};

export function useStatementConfirmation(options: StatementConfirmationOptions) {
  const {
    household,
    confirmedStatements,
    setConfirmedStatements,
    createAndPostPurchase,
    correctPurchaseFromStatement,
    refreshPurchases,
    setHasUnsavedChanges,
    onAdded,
  } = options;
  const confirmStatement = async (
    document: DocumentReview,
    entry: StatementEntry,
    paymentAccountId: string,
  ) => {
    if (!document.result || (entry.kind !== "CARD_PURCHASE" && entry.kind !== "EXTERNAL_INCOME"))
      return;
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    const account = household.accounts.find((candidate) => candidate.id === paymentAccountId);
    const isExternalIncome = entry.kind === "EXTERNAL_INCOME";
    if (!account || (isExternalIncome ? account.kind !== "BANK" : false)) {
      throw new Error(
        isExternalIncome ? "入金先の銀行口座を選択してください" : "決済口座を選択してください",
      );
    }
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    if (isExternalIncome) {
      await recordStatementExternalIncome(
        createStatementExternalIncomeInput({
          document,
          householdId: household.id,
          accountId: account.id,
          entry,
          idempotencyKey: await importIdempotencyKey(document.id, reference),
        }),
      );
      setConfirmedStatements((current) => new Set(current).add(key));
      setHasUnsavedChanges(false);
      onAdded(true);
      return;
    }
    await createAndPostPurchase(
      createStatementPurchaseInput({
        document,
        householdId: household.id,
        idempotencyKey: await importIdempotencyKey(document.id, reference),
        entry,
        paymentAccountId,
      }),
    );
    refreshPurchases();
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const confirmStatementRefund = async (
    document: DocumentReview,
    entry: StatementEntry,
    purchaseId: string,
    itemId?: string,
  ) => {
    if (entry.kind !== "REFUND") throw new Error("返金明細を選択してください");
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    const target = itemId ? { item: { purchaseId, itemId } } : { purchase: { purchaseId } };
    await correctPurchaseFromStatement({
      input: {
        householdId: household.id,
        sourceDocumentId: document.id,
        sourceReference: reference,
        target,
        reason: "明細の返品・返金",
        idempotencyKey: await importIdempotencyKey(document.id, reference),
      },
    });
    refreshPurchases();
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const linkStatementRefundCorrection = async (
    document: DocumentReview,
    entry: StatementEntry,
    correctionId: string,
  ) => {
    if (entry.kind !== "REFUND" || entry.amount >= 0) throw new Error("返金明細を選択してください");
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    await linkStatementRefundCorrectionRequest({
      householdId: household.id,
      correctionId,
      sourceDocumentId: document.id,
      sourceReference: reference,
    });
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const confirmStatementTransfer = async (
    document: DocumentReview,
    entry: StatementEntry,
    statementAccountId: string,
    otherAccountId: string,
  ) => {
    if (entry.kind !== "ACCOUNT_TRANSFER" && entry.kind !== "CARD_BILL_PAYMENT") {
      throw new Error("口座移動の明細を選択してください");
    }
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    const statementAccount = household.accounts.find(
      (account) => account.id === statementAccountId,
    );
    const otherAccount = household.accounts.find((account) => account.id === otherAccountId);
    const isCardBillPayment = entry.kind === "CARD_BILL_PAYMENT";
    if (!statementAccount || !otherAccount || statementAccount.id === otherAccount.id) {
      throw new Error("振替元と振替先の口座を選択してください");
    }
    if (
      (isCardBillPayment && (statementAccount.kind === "CARD" || otherAccount.kind !== "CARD")) ||
      (!isCardBillPayment && (statementAccount.kind === "CARD" || otherAccount.kind === "CARD"))
    ) {
      throw new Error("明細の口座種別に合う口座を選択してください");
    }
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    const fromAccountId =
      isCardBillPayment || entry.amount < 0 ? statementAccount.id : otherAccount.id;
    const toAccountId =
      isCardBillPayment || entry.amount < 0 ? otherAccount.id : statementAccount.id;
    await recordStatementAccountMovement({
      householdId: household.id,
      kind: isCardBillPayment ? "CARD_BILL_PAYMENT" : "TRANSFER",
      fromAccountId,
      toAccountId,
      amountYen: Math.abs(entry.amount),
      occurredAt: statementOccurredAt(entry),
      description: entry.merchant,
      idempotencyKey: await importIdempotencyKey(document.id, reference),
      sourceDocumentId: document.id,
      sourceReference: reference,
      sourceAccountId: statementAccount.id,
    });
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const confirmStatementLoan = async (
    document: DocumentReview,
    entry: StatementEntry,
    statementAccountId: string,
    fromMemberId: string,
    toMemberId: string,
    loanId?: string,
  ) => {
    if (entry.kind !== "LOAN" && entry.kind !== "REPAYMENT")
      throw new Error("貸付または返済の明細を選択してください");
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    if (
      !household.accounts.some(
        (account) => account.id === statementAccountId && account.kind === "BANK",
      )
    )
      throw new Error("銀行口座を選択してください");
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    const input = {
      householdId: household.id,
      fromMemberId,
      toMemberId,
      amountYen: Math.abs(entry.amount),
      occurredAt: statementOccurredAt(entry),
      description: entry.merchant,
      idempotencyKey: await importIdempotencyKey(document.id, reference),
      sourceDocumentId: document.id,
      sourceReference: reference,
      sourceAccountId: statementAccountId,
      ...(entry.amount < 0
        ? { paymentAccountId: statementAccountId }
        : { destinationAccountId: statementAccountId }),
    };
    if (entry.kind === "LOAN") {
      await recordStatementLoan({ ...input, kind: "LOAN" });
    } else {
      if (!loanId) throw new Error("返済対象の貸付を選択してください");
      await recordStatementRepayment({ ...input, loanId });
    }
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const linkStatementTransfer = async (
    document: DocumentReview,
    entry: StatementEntry,
    statementAccountId: string,
    transferId: string,
  ) => {
    if (entry.kind !== "LOAN" && entry.kind !== "REPAYMENT")
      throw new Error("貸付または返済の明細を選択してください");
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    if (
      !household.accounts.some(
        (account) => account.id === statementAccountId && account.kind === "BANK",
      )
    )
      throw new Error("銀行口座を選択してください");
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    await linkStatementTransferRequest({
      householdId: household.id,
      ledgerEntryId: transferId,
      sourceDocumentId: document.id,
      sourceReference: reference,
      sourceAccountId: statementAccountId,
    });
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const linkStatementAccountMovement = async (
    document: DocumentReview,
    entry: StatementEntry,
    statementAccountId: string,
    movementId: string,
  ) => {
    if (entry.kind !== "ACCOUNT_TRANSFER" && entry.kind !== "CARD_BILL_PAYMENT")
      throw new Error("口座移動の明細を選択してください");
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    if (
      !household.accounts.some(
        (account) => account.id === statementAccountId && account.kind === "BANK",
      )
    )
      throw new Error("銀行口座を選択してください");
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    await linkStatementAccountMovementRequest({
      householdId: household.id,
      accountMovementId: movementId,
      sourceDocumentId: document.id,
      sourceReference: reference,
      sourceAccountId: statementAccountId,
    });
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const linkStatementAccountIncome = async (
    document: DocumentReview,
    entry: StatementEntry,
    statementAccountId: string,
    incomeId: string,
  ) => {
    if (entry.kind !== "EXTERNAL_INCOME") throw new Error("外部入金の明細を選択してください");
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    if (
      !household.accounts.some(
        (account) => account.id === statementAccountId && account.kind === "BANK",
      )
    )
      throw new Error("銀行口座を選択してください");
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    await linkStatementAccountIncomeRequest({
      householdId: household.id,
      accountIncomeId: incomeId,
      sourceDocumentId: document.id,
      sourceReference: reference,
      sourceAccountId: statementAccountId,
    });
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };
  return {
    confirmStatement,
    confirmStatementRefund,
    linkStatementRefundCorrection,
    confirmStatementTransfer,
    confirmStatementLoan,
    linkStatementTransfer,
    linkStatementAccountMovement,
    linkStatementAccountIncome,
  };
}
