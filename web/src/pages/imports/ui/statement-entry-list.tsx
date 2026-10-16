import type { Account, Household } from "../../../shared/api";

import {
  statementConfirmationIssue,
  statementSourceReference,
  yen,
  type StatementEntry,
} from "../model/imports";
import { PaymentEntryEditor } from "./payment-entry-editor";
import { RefundEntryEditor } from "./refund-entry-editor";
import { StatementEntrySummary } from "./statement-entry-summary";
import { useStatementReviewCandidateData } from "./statement-review-candidate-data";
import { TransferEntryEditor } from "./transfer-entry-editor";

export function StatementEntryList({
  documentId,
  household,
  accounts,
  entries,
  confirmedStatements,
  pending,
  candidates,
  onConfirm,
  onConfirmRefund,
  onLinkRefund,
  onConfirmTransfer,
  onConfirmLoan,
  onLinkExistingTransfer,
  onLinkExistingMovement,
  onLinkExistingIncome,
  onSelectRegion,
}: {
  documentId: string;
  household: Household;
  accounts: Account[];
  entries: StatementEntry[];
  confirmedStatements: Set<string>;
  pending: boolean;
  candidates: ReturnType<typeof useStatementReviewCandidateData>;
  onConfirm: (entry: StatementEntry, accountId: string) => void;
  onConfirmRefund: (entry: StatementEntry, purchaseId: string, itemId?: string) => void;
  onLinkRefund: (entry: StatementEntry, correctionId: string) => void;
  onConfirmTransfer: (
    entry: StatementEntry,
    statementAccountId: string,
    otherAccountId: string,
  ) => void;
  onConfirmLoan: (
    entry: StatementEntry,
    accountId: string,
    fromMemberId: string,
    toMemberId: string,
    loanId?: string,
  ) => void;
  onLinkExistingTransfer: (entry: StatementEntry, accountId: string, transferId: string) => void;
  onLinkExistingMovement: (entry: StatementEntry, accountId: string, movementId: string) => void;
  onLinkExistingIncome: (entry: StatementEntry, accountId: string, incomeId: string) => void;
  onSelectRegion: (entry: StatementEntry) => void;
}) {
  const {
    refundTargets,
    setRefundTargets,
    refundPurchaseCandidates,
    refundPurchaseCandidateState,
    refundCorrectionTargets,
    setRefundCorrectionTargets,
    openRefundCorrections,
    refundCorrectionCandidates,
    refundCorrectionPages,
    refundCorrectionErrors,
    paymentAccounts,
    setPaymentAccounts,
    statementAccounts,
    setStatementAccounts,
    loans,
    transferCandidates,
    movementCandidates,
    loadingIncomeCandidates,
    incomeCandidates,
    hasMoreIncomeCandidates,
    selectedIncomeIds,
    setSelectedIncomeIds,
    candidatePageLoadErrors,
    loadRefundCorrections,
    toggleRefundCorrections,
    loadMoreIncomeCandidates,
    loadRefundPurchaseCandidates,
  } = candidates;

  return (
    <>
      {entries.map((entry) => {
        const reference = statementSourceReference(entry);
        const key = `${documentId}:${reference}`;
        const confirmed = confirmedStatements.has(key);
        const issue = statementConfirmationIssue(entry);
        const refundTarget = refundTargets[key] ?? {
          purchaseId: "",
          itemId: "",
        };
        const paymentAccountId = paymentAccounts[key] ?? "";
        const selectedIncomeId = selectedIncomeIds[key] ?? "";
        const transferAccounts = statementAccounts[key] ?? {
          statementAccountId: "",
          otherAccountId: "",
        };
        const isRefund = entry.kind === "REFUND";
        const isTransfer = entry.kind === "ACCOUNT_TRANSFER";
        const isCardBillPayment = entry.kind === "CARD_BILL_PAYMENT";
        const isExternalIncome = entry.kind === "EXTERNAL_INCOME";
        const isLoan = entry.kind === "LOAN";
        const isRepayment = entry.kind === "REPAYMENT";
        const matchingTransfers = transferCandidates.flatMap(({ node }) => {
          const expectedTypename = isLoan
            ? "LoanTransferLedgerEntry"
            : "RepaymentTransferLedgerEntry";
          if ((!isLoan && !isRepayment) || node.__typename !== expectedTypename) return [];
          if (
            Number(node.amountYen) !== Math.abs(entry.amount) ||
            Date.parse(node.occurredAt) !== Date.parse(entry.occurredAt)
          )
            return [];
          return [
            {
              id: node.id,
              description: node.description,
              occurredAt: node.occurredAt,
            },
          ];
        });
        const matchingMovements = movementCandidates.flatMap(({ node }) => {
          const expectedKind = isTransfer ? "TRANSFER" : "CARD_BILL_PAYMENT";
          if ((!isTransfer && !isCardBillPayment) || node.kind !== expectedKind) return [];
          if (
            Number(node.amountYen) !== Math.abs(entry.amount) ||
            Date.parse(node.occurredAt) !== Date.parse(entry.occurredAt)
          )
            return [];
          return [
            {
              id: node.id,
              description: node.description,
              occurredAt: node.occurredAt,
            },
          ];
        });
        const matchingIncomes = incomeCandidates.flatMap(({ node }) => {
          if (
            !isExternalIncome ||
            node.accountId !== paymentAccountId ||
            Number(node.amountYen) !== entry.amount ||
            Date.parse(node.occurredAt) !== Date.parse(entry.occurredAt)
          )
            return [];
          return [
            {
              id: node.id,
              description: node.description,
              occurredAt: node.occurredAt,
            },
          ];
        });
        const selectedTransferId = transferAccounts.existingTransferId ?? "";
        const selectedMovementId = transferAccounts.existingMovementId ?? "";
        const loanDraft = transferAccounts;
        const refundCandidates = refundCorrectionCandidates[key] ?? [];
        const purchaseCandidateKey = `${household.id}:${key}`;
        const purchaseCandidates = refundPurchaseCandidates[purchaseCandidateKey] ?? [];
        const purchaseCandidateState = refundPurchaseCandidateState[purchaseCandidateKey];
        const refundCorrectionPage = refundCorrectionPages[key];
        return (
          <div
            className="statement-row"
            key={`${entry.row}-${entry.transactionId ?? entry.occurredAt}`}
          >
            <StatementEntrySummary entry={entry} onSelectRegion={onSelectRegion} />
            <div className="statement-row-action">
              <strong>{yen(entry.amount)}</strong>
              {isRefund ? (
                <RefundEntryEditor
                  entry={entry}
                  pending={pending}
                  confirmed={confirmed}
                  issue={issue}
                  target={refundTarget}
                  purchases={purchaseCandidates}
                  purchaseState={purchaseCandidateState}
                  correctionPage={refundCorrectionPage}
                  corrections={[...refundCandidates]}
                  correctionOpen={openRefundCorrections[key] ?? false}
                  correctionError={refundCorrectionErrors[key]}
                  selectedCorrectionId={refundCorrectionTargets[key] ?? ""}
                  onLoadPurchases={() => loadRefundPurchaseCandidates(entry)}
                  onSelectPurchase={(purchaseId) =>
                    setRefundTargets((current) => ({
                      ...current,
                      [key]: { purchaseId, itemId: "" },
                    }))
                  }
                  onSelectItem={(itemId) =>
                    setRefundTargets((current) => ({
                      ...current,
                      [key]: { ...refundTarget, itemId },
                    }))
                  }
                  onConfirm={onConfirmRefund}
                  onToggleCorrections={() => toggleRefundCorrections(entry)}
                  onSelectCorrection={(correctionId) =>
                    setRefundCorrectionTargets((current) => ({
                      ...current,
                      [key]: correctionId,
                    }))
                  }
                  onLinkCorrection={onLinkRefund}
                  onLoadCorrections={(after) => loadRefundCorrections(entry, after)}
                />
              ) : isTransfer || isCardBillPayment || isLoan || isRepayment ? (
                <TransferEntryEditor
                  entry={entry}
                  household={household}
                  accounts={accounts}
                  loans={loans}
                  draft={transferAccounts}
                  matchingTransfers={matchingTransfers}
                  matchingMovements={matchingMovements}
                  pending={pending}
                  confirmed={confirmed}
                  issue={issue}
                  onChange={(update) =>
                    setStatementAccounts((current) => ({
                      ...current,
                      [key]: { ...transferAccounts, ...update },
                    }))
                  }
                  onSubmit={() =>
                    selectedMovementId
                      ? onLinkExistingMovement(
                          entry,
                          transferAccounts.statementAccountId,
                          selectedMovementId,
                        )
                      : selectedTransferId
                        ? onLinkExistingTransfer(
                            entry,
                            transferAccounts.statementAccountId,
                            selectedTransferId,
                          )
                        : isLoan || isRepayment
                          ? onConfirmLoan(
                              entry,
                              transferAccounts.statementAccountId,
                              isLoan
                                ? loanDraft.fromMemberId!
                                : loans.find((loan) => loan.id === loanDraft.loanId)!
                                    .borrowerMemberId,
                              isLoan
                                ? loanDraft.toMemberId!
                                : loans.find((loan) => loan.id === loanDraft.loanId)!
                                    .lenderMemberId,
                              isRepayment ? loanDraft.loanId : undefined,
                            )
                          : onConfirmTransfer(
                              entry,
                              transferAccounts.statementAccountId,
                              transferAccounts.otherAccountId,
                            )
                  }
                />
              ) : (
                (entry.kind === "CARD_PURCHASE" || isExternalIncome) && (
                  <PaymentEntryEditor
                    entry={entry}
                    accounts={accounts}
                    pending={pending}
                    confirmed={confirmed}
                    issue={issue}
                    accountId={paymentAccountId}
                    selectedIncomeId={selectedIncomeId}
                    matchingIncomes={matchingIncomes}
                    hasMoreIncomeCandidates={hasMoreIncomeCandidates}
                    loadingIncomeCandidates={loadingIncomeCandidates}
                    incomePageError={candidatePageLoadErrors.incomes ?? false}
                    onSelectAccount={(accountId) => {
                      setPaymentAccounts((current) => ({
                        ...current,
                        [key]: accountId,
                      }));
                      setSelectedIncomeIds((current) => ({
                        ...current,
                        [key]: "",
                      }));
                    }}
                    onSelectIncome={(incomeId) =>
                      setSelectedIncomeIds((current) => ({
                        ...current,
                        [key]: incomeId,
                      }))
                    }
                    onLoadMoreIncomes={loadMoreIncomeCandidates}
                    onConfirm={onConfirm}
                    onLinkIncome={onLinkExistingIncome}
                  />
                )
              )}
            </div>
          </div>
        );
      })}
    </>
  );
}
