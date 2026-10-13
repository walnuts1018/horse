import { useEffect, useRef, useState } from "react";
import { Check, Plus } from "lucide-react";
import type { Household, Settlement } from "../../../shared/api";
import { useHouseholdLedger } from "../../../entities/ledger";
import { InlineError } from "../../../shared/ui";
import { japanMonth } from "../../../shared/lib";
import "./settlement-page.css";
import { settlementTotalDue } from "../model/settlement-view";
import { readSettlement } from "../api/read-settlement";
import { closeSettlementPeriod } from "../api/settlement-mutations";
import { SettlementLoans } from "./settlement-loans";
import { SettlementCloseConfirmation } from "./settlement-close-confirmation";
import { SettlementPeriodPicker } from "./settlement-period-picker";
import { SettlementPreview } from "./settlement-preview";
import { SettlementTransferForm } from "./settlement-transfer-form";
import { SettlementLedger } from "./settlement-ledger";
import { useSettlementTransfer } from "../model/settlement-transfer";
import { useSettlementFunding } from "../model/settlement-funding";

export function SettlementPage({
  household,
  onUnsavedChanges,
}: {
  household: Household;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  const [period, setPeriod] = useState(() => japanMonth(new Date()));
  const [settlementSnapshot, setSettlementSnapshot] = useState<{
    householdId: string;
    period: string;
    value: Settlement;
  } | null>(null);
  const settlement =
    settlementSnapshot?.householdId === household.id && settlementSnapshot.period === period
      ? settlementSnapshot.value
      : null;
  const activePeriod = useRef(period);
  activePeriod.current = period;
  const activeHouseholdId = useRef(household.id);
  activeHouseholdId.current = household.id;
  const loadRequest = useRef(0);
  const {
    entries,
    hasNextPage: ledgerHasNextPage,
    loadingMore: ledgerLoadingMore,
    moreError: ledgerMoreError,
    loadMore: loadMoreLedger,
    refresh: refreshLedger,
  } = useHouseholdLedger(household.id);
  const [hasUnsavedLoanRepayment, setHasUnsavedLoanRepayment] = useState(false);
  const [hasPendingLoanRepayment, setHasPendingLoanRepayment] = useState(false);
  const [loanRefreshSignal, setLoanRefreshSignal] = useState(0);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [loadError, setLoadError] = useState("");
  const [pending, setPending] = useState(false);
  const [closeConfirmationOpen, setCloseConfirmationOpen] = useState(false);
  const load = async () => {
    const request = ++loadRequest.current;
    const requestedPeriod = activePeriod.current;
    const requestedHouseholdId = activeHouseholdId.current;
    setLoadError("");
    try {
      refreshLedger();
      const settlementData = await readSettlement(requestedHouseholdId, requestedPeriod);
      if (request !== loadRequest.current) return;
      setSettlementSnapshot({
        householdId: requestedHouseholdId,
        period: requestedPeriod,
        value: settlementData,
      });
    } catch (reason) {
      if (request === loadRequest.current) setLoadError((reason as Error).message);
    }
  };
  const funding = useSettlementFunding({
    household,
    settlement,
    setPending,
    setError,
    setSuccess,
    onRecorded: load,
  });
  useEffect(() => {
    void load();
  }, [household.id, period]);
  const transfer = useSettlementTransfer({
    household,
    setPending,
    setError,
    setSuccess,
    onRecorded: async () => {
      setLoanRefreshSignal((current) => current + 1);
      await load();
    },
  });
  useEffect(() => {
    onUnsavedChanges(
      transfer.hasUnsavedTransfer || funding.hasUnsavedFunding || hasUnsavedLoanRepayment,
    );
    return () => onUnsavedChanges(false);
  }, [
    funding.hasUnsavedFunding,
    hasUnsavedLoanRepayment,
    transfer.hasUnsavedTransfer,
    onUnsavedChanges,
  ]);
  const close = async () => {
    if (!settlement || !settlementSnapshot || settlementSnapshot.period !== period) return;
    const targetHouseholdId = settlementSnapshot.householdId;
    const targetPeriod = settlementSnapshot.period;
    const expectedRevision = settlement.revision;
    setPending(true);
    setSuccess("");
    try {
      await closeSettlementPeriod({
        householdId: targetHouseholdId,
        period: targetPeriod,
        expectedRevision,
      });
      setCloseConfirmationOpen(false);
      await load();
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const totalDue = settlementTotalDue(settlement);
  const parsedTransferAmount = Number(transfer.transferAmount);
  const hasPendingTransfer = transfer.hasPendingTransfer || hasPendingLoanRepayment;
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
            setCloseConfirmationOpen(false);
          }}
        />
      </div>
      {error && <InlineError message={error} />}
      {success && (
        <p className="settlement-confirmation" role="status">
          {success}
        </p>
      )}
      {loadError && (
        <div className="settlement-load-error">
          <InlineError message={loadError} />
          <button className="text-button" type="button" onClick={() => void load()}>
            再試行
          </button>
        </div>
      )}
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
              setCloseConfirmationOpen(true);
            }}
          >
            <Check size={16} />
            期間を締める
          </button>
          <button
            className="button button-primary"
            disabled={hasPendingLoanRepayment}
            onClick={() => {
              transfer.toggle(hasPendingLoanRepayment);
            }}
          >
            <Plus size={16} />
            送金を記録
          </button>
        </div>
        {closeConfirmationOpen && settlement?.__typename === "OpenSettlement" && (
          <SettlementCloseConfirmation
            settlement={settlement}
            period={period}
            totalDue={totalDue}
            pending={pending}
            onClose={() => setCloseConfirmationOpen(false)}
            onConfirm={() => void close()}
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
        refreshSignal={loanRefreshSignal}
        canRecordLoan={!hasPendingTransfer}
        canManageRepayments={!transfer.pendingTransfer}
        onRecordLoan={() => {
          transfer.prepareLoan();
        }}
        onPendingIntentChange={setHasPendingLoanRepayment}
        onRecorded={() => {
          setSuccess("返済を記録しました");
          void load();
        }}
        onUnsavedChanges={setHasUnsavedLoanRepayment}
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
