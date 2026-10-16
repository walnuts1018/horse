import type { Account, Household } from "../../../shared/api";
import type { DocumentReview, StatementEntry } from "../model/imports";

import { StatementEntryList } from "./statement-entry-list";
import { useStatementReviewCandidateData } from "./statement-review-candidate-data";

export function StatementReview({
  document,
  household,
  accounts,
  confirmedStatements,
  pending,
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
  document: DocumentReview;
  household: Household;
  accounts: Account[];
  confirmedStatements: Set<string>;
  pending: boolean;
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
  const candidateData = useStatementReviewCandidateData({
    documentId: document.id,
    household,
  });
  const {
    candidateLoadErrors,
    candidatePageLoadErrors,
    hasMoreTransferCandidates,
    hasMoreMovementCandidates,
    loadingTransferCandidates,
    loadingMovementCandidates,
    loadLoanCandidates,
    loadIncomeCandidates,
    loadMovementCandidates,
    loadTransferCandidates,
    loadMoreTransferCandidates,
    loadMoreMovementCandidates,
  } = candidateData;
  const statements = document.result?.statements;
  if (!statements) return null;

  return (
    <div className="statement-review">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">明細行</span>
          <h3>{statements.length}件</h3>
        </div>
      </div>
      {[
        ...(statements.some((entry) => entry.kind === "REPAYMENT") ? ["loans"] : []),
        ...(statements.some((entry) => entry.kind === "LOAN" || entry.kind === "REPAYMENT")
          ? ["transfers"]
          : []),
        ...(statements.some(
          (entry) => entry.kind === "ACCOUNT_TRANSFER" || entry.kind === "CARD_BILL_PAYMENT",
        )
          ? ["movements"]
          : []),
        ...(statements.some((entry) => entry.kind === "EXTERNAL_INCOME") ? ["incomes"] : []),
      ]
        .filter((name) => candidateLoadErrors[name])
        .map((name) => (
          <p className="inline-error" role="alert" key={name}>
            {name === "loans"
              ? "貸付候補を読み込めませんでした"
              : name === "transfers"
                ? "取引候補を読み込めませんでした"
                : name === "movements"
                  ? "口座移動候補を読み込めませんでした"
                  : "入金候補を読み込めませんでした"}
            <button
              type="button"
              className="text-button"
              onClick={
                name === "loans"
                  ? loadLoanCandidates
                  : name === "transfers"
                    ? loadTransferCandidates
                    : name === "movements"
                      ? loadMovementCandidates
                      : loadIncomeCandidates
              }
            >
              再試行
            </button>
          </p>
        ))}
      {hasMoreTransferCandidates && (
        <button
          type="button"
          className="text-button"
          disabled={loadingTransferCandidates}
          onClick={loadMoreTransferCandidates}
        >
          {loadingTransferCandidates ? "読み込み中" : "過去の取引を読み込む"}
        </button>
      )}
      {candidatePageLoadErrors.transfers && (
        <p className="inline-error" role="alert">
          過去の取引を読み込めませんでした
          <button type="button" className="text-button" onClick={loadMoreTransferCandidates}>
            再試行
          </button>
        </p>
      )}
      {hasMoreMovementCandidates &&
        statements.some(
          (entry) => entry.kind === "ACCOUNT_TRANSFER" || entry.kind === "CARD_BILL_PAYMENT",
        ) && (
          <button
            type="button"
            className="text-button"
            disabled={loadingMovementCandidates}
            onClick={loadMoreMovementCandidates}
          >
            {loadingMovementCandidates ? "読み込み中" : "過去の口座移動を読み込む"}
          </button>
        )}
      {candidatePageLoadErrors.movements && (
        <p className="inline-error" role="alert">
          過去の口座移動を読み込めませんでした
          <button type="button" className="text-button" onClick={loadMoreMovementCandidates}>
            再試行
          </button>
        </p>
      )}
      <StatementEntryList
        documentId={document.id}
        household={household}
        accounts={accounts}
        entries={statements}
        confirmedStatements={confirmedStatements}
        pending={pending}
        candidates={candidateData}
        onConfirm={onConfirm}
        onConfirmRefund={onConfirmRefund}
        onLinkRefund={onLinkRefund}
        onConfirmTransfer={onConfirmTransfer}
        onConfirmLoan={onConfirmLoan}
        onLinkExistingTransfer={onLinkExistingTransfer}
        onLinkExistingMovement={onLinkExistingMovement}
        onLinkExistingIncome={onLinkExistingIncome}
        onSelectRegion={onSelectRegion}
      />
    </div>
  );
}
