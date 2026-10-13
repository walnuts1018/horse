import { useEffect, useState } from "react";
import { Check, Plus } from "lucide-react";
import type { Household } from "../../../shared/api";
import { useHouseholdLedger } from "../../../entities/ledger";
import "./settlement-page.css";
import { settlementTotalDue } from "../model/settlement-view";
import { SettlementLoans } from "./settlement-loans";
import { SettlementCloseConfirmation } from "./settlement-close-confirmation";
import { SettlementPeriodPicker } from "./settlement-period-picker";
import { SettlementPreview } from "./settlement-preview";
import { SettlementTransferForm } from "./settlement-transfer-form";
import { SettlementLedger } from "./settlement-ledger";
import { SettlementFeedback } from "./settlement-feedback";
import { useSettlementTransfer } from "../model/settlement-transfer";
import { useSettlementFunding } from "../model/settlement-funding";
import { useSettlementPeriod } from "../model/settlement-period";
import { useSettlementPeriodClose } from "../model/settlement-period-close";
import { useSettlementLoanState } from "../model/settlement-loan-state";

export function SettlementPage({
  household,
  onUnsavedChanges,
}: {
  household: Household;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [pending, setPending] = useState(false);
  const {
    entries,
    hasNextPage: ledgerHasNextPage,
    loadingMore: ledgerLoadingMore,
    moreError: ledgerMoreError,
    loadMore: loadMoreLedger,
    refresh: refreshLedger,
  } = useHouseholdLedger(household.id);
  const periodState = useSettlementPeriod(household.id, refreshLedger);
  const { load, loadError, period, setPeriod, settlement, snapshot } = periodState;
  const loanState = useSettlementLoanState({ setSuccess, reload: load });
  const funding = useSettlementFunding({
    household,
    settlement,
    setPending,
    setError,
    setSuccess,
    onRecorded: load,
  });
  const transfer = useSettlementTransfer({
    household,
    setPending,
    setError,
    setSuccess,
    onRecorded: async () => {
      loanState.onTransferRecorded();
      await load();
    },
  });
  useEffect(() => {
    onUnsavedChanges(
      transfer.hasUnsavedTransfer || funding.hasUnsavedFunding || loanState.hasUnsavedRepayment,
    );
    return () => onUnsavedChanges(false);
  }, [
    funding.hasUnsavedFunding,
    loanState.hasUnsavedRepayment,
    transfer.hasUnsavedTransfer,
    onUnsavedChanges,
  ]);
  const periodClose = useSettlementPeriodClose({
    settlement,
    snapshot,
    period,
    setPending,
    setError,
    setSuccess,
    reload: load,
  });
  const totalDue = settlementTotalDue(settlement);
  const parsedTransferAmount = Number(transfer.transferAmount);
  const hasPendingTransfer = transfer.hasPendingTransfer || loanState.hasPendingRepayment;
  const invalidTransferAmount =
    !Number.isSafeInteger(parsedTransferAmount) || parsedTransferAmount < 1;
  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">入金と立て替え</span>
          <h1>精算</h1>
          <p>次回の入金額と未精算の記録</p>
        </div>
        <SettlementPeriodPicker
          period={period}
          onChange={(nextPeriod) => {
            setPeriod(nextPeriod);
            periodClose.setConfirmationOpen(false);
          }}
        />
      </div>
      <SettlementFeedback
        error={error}
        success={success}
        loadError={loadError}
        onRetryLoad={() => void load()}
      />
      <SettlementPreview
        household={household}
        settlement={settlement}
        period={period}
        totalDue={totalDue}
        pending={pending}
        pendingFunding={funding.pendingFunding}
        fundingSourceAccounts={funding.fundingSourceAccounts}
        fundingDestinationAccountId={funding.fundingDestinationAccountId}
        savedFundingSourceAccounts={funding.savedFundingSourceAccounts}
        savedFundingDestinationAccountId={funding.savedFundingDestinationAccountId}
        onFundingSourceChange={(memberId, accountId) =>
          funding.setFundingSourceAccounts((current) => ({ ...current, [memberId]: accountId }))
        }
        onFundingDestinationChange={funding.setFundingDestinationAccountId}
        onRecordFunding={(memberId) => void funding.record(memberId)}
        onRestoreFunding={funding.restore}
      />
      <section className="panel settlement-panel">
        <div className="settlement-actions">
          <button
            className="button button-soft"
            disabled={!settlement || pending || settlement.__typename === "ClosedSettlement"}
            onClick={() => {
              periodClose.setConfirmationOpen(true);
            }}
          >
            <Check size={16} />
            期間を締める
          </button>
          <button
            className="button button-primary"
            disabled={loanState.hasPendingRepayment}
            onClick={() => {
              transfer.toggle(loanState.hasPendingRepayment);
            }}
          >
            <Plus size={16} />
            送金を記録
          </button>
        </div>
        {periodClose.confirmationOpen && settlement?.__typename === "OpenSettlement" && (
          <SettlementCloseConfirmation
            settlement={settlement}
            period={period}
            totalDue={totalDue}
            pending={pending}
            onClose={() => periodClose.setConfirmationOpen(false)}
            onConfirm={() => void periodClose.close()}
          />
        )}
        {transfer.transferOpen && (
          <SettlementTransferForm
            household={household}
            pending={pending}
            hasPendingTransfer={hasPendingTransfer}
            transferKind={transfer.transferKind}
            transferFrom={transfer.transferFrom}
            transferTo={transfer.transferTo}
            transferPaymentAccountId={transfer.transferPaymentAccountId}
            transferDestinationAccountId={transfer.transferDestinationAccountId}
            transferAmount={transfer.transferAmount}
            transferDescription={transfer.transferDescription}
            invalidTransferAmount={invalidTransferAmount}
            onSubmit={transfer.save}
            onKindChange={(kind) => {
              transfer.markUnsaved();
              transfer.setTransferKind(kind);
              transfer.setTransferAmount("");
              setError("");
            }}
            onFromChange={(memberId) => {
              transfer.markUnsaved();
              transfer.setTransferFrom(memberId);
            }}
            onToChange={(memberId) => {
              transfer.markUnsaved();
              transfer.setTransferTo(memberId);
            }}
            onPaymentAccountChange={(accountId) => {
              transfer.markUnsaved();
              transfer.setTransferPaymentAccountId(accountId);
            }}
            onDestinationAccountChange={(accountId) => {
              transfer.markUnsaved();
              transfer.setTransferDestinationAccountId(accountId);
            }}
            onAmountChange={(amount) => {
              transfer.markUnsaved();
              transfer.setTransferAmount(amount);
            }}
            onDescriptionChange={(description) => {
              transfer.markUnsaved();
              transfer.setTransferDescription(description);
            }}
          />
        )}
      </section>
      <SettlementLoans
        household={household}
        refreshSignal={loanState.refreshSignal}
        canRecordLoan={!hasPendingTransfer}
        canManageRepayments={!transfer.pendingTransfer}
        onRecordLoan={() => {
          transfer.prepareLoan();
        }}
        onPendingIntentChange={loanState.setHasPendingRepayment}
        onRecorded={loanState.onRepaymentRecorded}
        onUnsavedChanges={loanState.setHasUnsavedRepayment}
      />
      <SettlementLedger
        household={household}
        entries={entries}
        error={ledgerMoreError}
        hasNextPage={ledgerHasNextPage}
        loadingMore={ledgerLoadingMore}
        onLoadMore={loadMoreLedger}
      />
    </>
  );
}
