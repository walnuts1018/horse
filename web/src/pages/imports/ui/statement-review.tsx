import { useCallback, useEffect, useRef, useState } from "react";
import { fetchQuery } from "react-relay";

import {
  accountMovementsQuery,
  accountIncomesQuery,
  loanBalancesQuery,
  statementTransferCandidatesQuery,
} from "../../../entities/ledger";
import {
  getRelayEnvironment,
  type Account,
  type AccountMovementsQuery,
  type AccountIncomesQuery,
  type Household,
  type LoanBalancesQuery,
  type Purchase,
  type StatementTransferCandidatesQuery,
  type StatementRefundCorrectionsQuery,
} from "../../../shared/api";
import { statementRefundCorrectionsQuery } from "../api/operations";
import {
  statementConfirmationIssue,
  statementSourceReference,
  yen,
  type DocumentReview,
  type StatementEntry,
} from "../model/imports";

export function StatementReview({
  document,
  purchases,
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
  purchases: Purchase[];
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
  const [refundTargets, setRefundTargets] = useState<
    Record<string, { purchaseId: string; itemId: string }>
  >({});
  const [refundCorrectionTargets, setRefundCorrectionTargets] = useState<Record<string, string>>(
    {},
  );
  const [openRefundCorrections, setOpenRefundCorrections] = useState<Record<string, boolean>>({});
  const [refundCorrectionCandidates, setRefundCorrectionCandidates] = useState<
    Record<
      string,
      StatementRefundCorrectionsQuery["response"]["statementRefundCorrections"]["edges"]
    >
  >({});
  const [refundCorrectionPages, setRefundCorrectionPages] = useState<
    Record<string, { after: string | null; hasNextPage: boolean; loading: boolean }>
  >({});
  const [refundCorrectionErrors, setRefundCorrectionErrors] = useState<Record<string, string>>({});
  const [paymentAccounts, setPaymentAccounts] = useState<Record<string, string>>({});
  const [statementAccounts, setStatementAccounts] = useState<
    Record<
      string,
      {
        statementAccountId: string;
        otherAccountId: string;
        fromMemberId?: string;
        toMemberId?: string;
        loanId?: string;
        existingTransferId?: string;
        existingMovementId?: string;
      }
    >
  >({});
  const [loans, setLoans] = useState<
    Array<{ id: string; lenderMemberId: string; borrowerMemberId: string; balanceYen: number }>
  >([]);
  const [transferCandidates, setTransferCandidates] = useState<
    StatementTransferCandidatesQuery["response"]["ledger"]["edges"]
  >([]);
  const [transferCandidateCursor, setTransferCandidateCursor] = useState<string | null>(null);
  const [hasMoreTransferCandidates, setHasMoreTransferCandidates] = useState(false);
  const [loadingTransferCandidates, setLoadingTransferCandidates] = useState(false);
  const [movementCandidates, setMovementCandidates] = useState<
    AccountMovementsQuery["response"]["accountMovements"]["edges"]
  >([]);
  const [movementCandidateCursor, setMovementCandidateCursor] = useState<string | null>(null);
  const [hasMoreMovementCandidates, setHasMoreMovementCandidates] = useState(false);
  const [loadingMovementCandidates, setLoadingMovementCandidates] = useState(false);
  const [incomeCandidates, setIncomeCandidates] = useState<
    AccountIncomesQuery["response"]["accountIncomes"]["edges"]
  >([]);
  const [incomeCandidateCursor, setIncomeCandidateCursor] = useState<string | null>(null);
  const [hasMoreIncomeCandidates, setHasMoreIncomeCandidates] = useState(false);
  const [loadingIncomeCandidates, setLoadingIncomeCandidates] = useState(false);
  const [selectedIncomeIds, setSelectedIncomeIds] = useState<Record<string, string>>({});
  const [candidateLoadErrors, setCandidateLoadErrors] = useState<Record<string, boolean>>({});
  const [candidatePageLoadErrors, setCandidatePageLoadErrors] = useState<Record<string, boolean>>(
    {},
  );
  const candidateSubscriptions = useRef<Record<string, { unsubscribe: () => void } | undefined>>(
    {},
  );
  const loadRefundCorrections = (entry: StatementEntry, after?: string) => {
    const reference = statementSourceReference(entry);
    const key = `${document.id}:${reference}`;
    setRefundCorrectionErrors((current) => ({ ...current, [key]: "" }));
    setRefundCorrectionPages((current) => ({
      ...current,
      [key]: { after: current[key]?.after ?? null, hasNextPage: false, loading: true },
    }));
    fetchQuery<StatementRefundCorrectionsQuery>(
      getRelayEnvironment(),
      statementRefundCorrectionsQuery,
      {
        householdId: household.id,
        sourceDocumentId: document.id,
        sourceReference: reference,
        first: 50,
        after: after ?? null,
      },
    ).subscribe({
      next: (data) => {
        const page = data.statementRefundCorrections;
        setRefundCorrectionCandidates((current) => ({
          ...current,
          [key]: after ? [...(current[key] ?? []), ...page.edges] : page.edges,
        }));
        setRefundCorrectionPages((current) => ({
          ...current,
          [key]: {
            after: page.pageInfo.endCursor ?? null,
            hasNextPage: page.pageInfo.hasNextPage,
            loading: false,
          },
        }));
      },
      error: () => {
        setRefundCorrectionPages((current) => ({
          ...current,
          [key]: { after: current[key]?.after ?? null, hasNextPage: false, loading: false },
        }));
        setRefundCorrectionErrors((current) => ({
          ...current,
          [key]: "返金記録を読み込めませんでした",
        }));
      },
    });
  };
  const toggleRefundCorrections = (entry: StatementEntry) => {
    const key = `${document.id}:${statementSourceReference(entry)}`;
    if (openRefundCorrections[key]) {
      setOpenRefundCorrections((current) => ({ ...current, [key]: false }));
      return;
    }
    setOpenRefundCorrections((current) => ({ ...current, [key]: true }));
    if (!(key in refundCorrectionCandidates)) loadRefundCorrections(entry);
  };
  const loadLoanCandidates = useCallback(() => {
    candidateSubscriptions.current.loans?.unsubscribe();
    setLoans([]);
    setCandidateLoadErrors((current) => ({ ...current, loans: false }));
    candidateSubscriptions.current.loans = fetchQuery<LoanBalancesQuery>(
      getRelayEnvironment(),
      loanBalancesQuery,
      { householdId: household.id, first: 100 },
    ).subscribe({
      next: (data) =>
        setLoans(
          data.loanBalances.edges.map(({ node }) => ({
            id: node.id,
            lenderMemberId: node.lenderMemberId,
            borrowerMemberId: node.borrowerMemberId,
            balanceYen: Number(node.balanceYen),
          })),
        ),
      error: () => setCandidateLoadErrors((current) => ({ ...current, loans: true })),
    });
  }, [household.id]);
  useEffect(() => {
    const subscriptions = candidateSubscriptions.current;
    loadLoanCandidates();
    return () => subscriptions.loans?.unsubscribe();
  }, [loadLoanCandidates]);
  const loadIncomeCandidates = useCallback(() => {
    candidateSubscriptions.current.incomes?.unsubscribe();
    setIncomeCandidates([]);
    setIncomeCandidateCursor(null);
    setHasMoreIncomeCandidates(false);
    setCandidateLoadErrors((current) => ({ ...current, incomes: false }));
    candidateSubscriptions.current.incomes = fetchQuery<AccountIncomesQuery>(
      getRelayEnvironment(),
      accountIncomesQuery,
      { householdId: household.id, first: 100, after: null },
      { fetchPolicy: "network-only" },
    ).subscribe({
      next: (data) => {
        setIncomeCandidates(data.accountIncomes.edges);
        setIncomeCandidateCursor(data.accountIncomes.pageInfo.endCursor ?? null);
        setHasMoreIncomeCandidates(data.accountIncomes.pageInfo.hasNextPage);
      },
      error: () => setCandidateLoadErrors((current) => ({ ...current, incomes: true })),
    });
  }, [household.id]);
  useEffect(() => {
    const subscriptions = candidateSubscriptions.current;
    loadIncomeCandidates();
    return () => subscriptions.incomes?.unsubscribe();
  }, [loadIncomeCandidates]);
  const loadMovementCandidates = useCallback(() => {
    candidateSubscriptions.current.movements?.unsubscribe();
    setMovementCandidates([]);
    setMovementCandidateCursor(null);
    setHasMoreMovementCandidates(false);
    setCandidateLoadErrors((current) => ({ ...current, movements: false }));
    candidateSubscriptions.current.movements = fetchQuery<AccountMovementsQuery>(
      getRelayEnvironment(),
      accountMovementsQuery,
      {
        householdId: household.id,
        first: 100,
        after: null,
      },
      { fetchPolicy: "network-only" },
    ).subscribe({
      next: (data) => {
        setMovementCandidates(data.accountMovements.edges);
        setMovementCandidateCursor(data.accountMovements.pageInfo.endCursor ?? null);
        setHasMoreMovementCandidates(data.accountMovements.pageInfo.hasNextPage);
      },
      error: () => setCandidateLoadErrors((current) => ({ ...current, movements: true })),
    });
  }, [household.id]);
  useEffect(() => {
    const subscriptions = candidateSubscriptions.current;
    loadMovementCandidates();
    return () => subscriptions.movements?.unsubscribe();
  }, [loadMovementCandidates]);
  const loadTransferCandidates = useCallback(() => {
    candidateSubscriptions.current.transfers?.unsubscribe();
    setTransferCandidates([]);
    setTransferCandidateCursor(null);
    setHasMoreTransferCandidates(false);
    setCandidateLoadErrors((current) => ({ ...current, transfers: false }));
    candidateSubscriptions.current.transfers = fetchQuery<StatementTransferCandidatesQuery>(
      getRelayEnvironment(),
      statementTransferCandidatesQuery,
      { householdId: household.id, first: 100, after: null },
      { fetchPolicy: "network-only" },
    ).subscribe({
      next: (data) => {
        setTransferCandidates(data.ledger.edges);
        setTransferCandidateCursor(data.ledger.pageInfo.endCursor ?? null);
        setHasMoreTransferCandidates(data.ledger.pageInfo.hasNextPage);
      },
      error: () => setCandidateLoadErrors((current) => ({ ...current, transfers: true })),
    });
  }, [household.id]);
  useEffect(() => {
    const subscriptions = candidateSubscriptions.current;
    loadTransferCandidates();
    return () => subscriptions.transfers?.unsubscribe();
  }, [loadTransferCandidates]);
  const loadMoreTransferCandidates = () => {
    if (!transferCandidateCursor || loadingTransferCandidates) return;
    setCandidatePageLoadErrors((current) => ({ ...current, transfers: false }));
    setLoadingTransferCandidates(true);
    fetchQuery<StatementTransferCandidatesQuery>(
      getRelayEnvironment(),
      statementTransferCandidatesQuery,
      { householdId: household.id, first: 100, after: transferCandidateCursor },
      { fetchPolicy: "network-only" },
    ).subscribe({
      next: (data) => {
        setTransferCandidates((current) => [...current, ...data.ledger.edges]);
        setTransferCandidateCursor(data.ledger.pageInfo.endCursor ?? null);
        setHasMoreTransferCandidates(data.ledger.pageInfo.hasNextPage);
      },
      error: () => {
        setLoadingTransferCandidates(false);
        setCandidatePageLoadErrors((current) => ({ ...current, transfers: true }));
      },
      complete: () => setLoadingTransferCandidates(false),
    });
  };
  const loadMoreMovementCandidates = () => {
    if (!movementCandidateCursor || loadingMovementCandidates) return;
    setCandidatePageLoadErrors((current) => ({ ...current, movements: false }));
    setLoadingMovementCandidates(true);
    fetchQuery<AccountMovementsQuery>(
      getRelayEnvironment(),
      accountMovementsQuery,
      {
        householdId: household.id,
        first: 100,
        after: movementCandidateCursor,
      },
      { fetchPolicy: "network-only" },
    ).subscribe({
      next: (data) => {
        setMovementCandidates((current) => [...current, ...data.accountMovements.edges]);
        setMovementCandidateCursor(data.accountMovements.pageInfo.endCursor ?? null);
        setHasMoreMovementCandidates(data.accountMovements.pageInfo.hasNextPage);
      },
      error: () => {
        setLoadingMovementCandidates(false);
        setCandidatePageLoadErrors((current) => ({ ...current, movements: true }));
      },
      complete: () => setLoadingMovementCandidates(false),
    });
  };
  const loadMoreIncomeCandidates = () => {
    if (!incomeCandidateCursor || loadingIncomeCandidates) return;
    setCandidatePageLoadErrors((current) => ({ ...current, incomes: false }));
    setLoadingIncomeCandidates(true);
    fetchQuery<AccountIncomesQuery>(
      getRelayEnvironment(),
      accountIncomesQuery,
      { householdId: household.id, first: 100, after: incomeCandidateCursor },
      { fetchPolicy: "network-only" },
    ).subscribe({
      next: (data) => {
        setIncomeCandidates((current) => [...current, ...data.accountIncomes.edges]);
        setIncomeCandidateCursor(data.accountIncomes.pageInfo.endCursor ?? null);
        setHasMoreIncomeCandidates(data.accountIncomes.pageInfo.hasNextPage);
      },
      error: () => {
        setLoadingIncomeCandidates(false);
        setCandidatePageLoadErrors((current) => ({ ...current, incomes: true }));
      },
      complete: () => setLoadingIncomeCandidates(false),
    });
  };
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
      {statements.map((entry) => {
        const reference = statementSourceReference(entry);
        const key = `${document.id}:${reference}`;
        const confirmed = confirmedStatements.has(key);
        const issue = statementConfirmationIssue(entry);
        const refundTarget = refundTargets[key] ?? { purchaseId: "", itemId: "" };
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
          return [{ id: node.id, description: node.description, occurredAt: node.occurredAt }];
        });
        const matchingMovements = movementCandidates.flatMap(({ node }) => {
          const expectedKind = isTransfer ? "TRANSFER" : "CARD_BILL_PAYMENT";
          if ((!isTransfer && !isCardBillPayment) || node.kind !== expectedKind) return [];
          if (
            Number(node.amountYen) !== Math.abs(entry.amount) ||
            Date.parse(node.occurredAt) !== Date.parse(entry.occurredAt)
          )
            return [];
          return [{ id: node.id, description: node.description, occurredAt: node.occurredAt }];
        });
        const matchingIncomes = incomeCandidates.flatMap(({ node }) => {
          if (
            !isExternalIncome ||
            node.accountId !== paymentAccountId ||
            Number(node.amountYen) !== entry.amount ||
            Date.parse(node.occurredAt) !== Date.parse(entry.occurredAt)
          )
            return [];
          return [{ id: node.id, description: node.description, occurredAt: node.occurredAt }];
        });
        const selectedTransferId = transferAccounts.existingTransferId ?? "";
        const selectedMovementId = transferAccounts.existingMovementId ?? "";
        const loanDraft = transferAccounts;
        const refundCandidates = refundCorrectionCandidates[key] ?? [];
        const refundCorrectionPage = refundCorrectionPages[key];
        return (
          <div
            className="statement-row"
            key={`${entry.row}-${entry.transactionId ?? entry.occurredAt}`}
          >
            <div>
              {entry.region ? (
                <button type="button" className="text-button" onClick={() => onSelectRegion(entry)}>
                  {entry.merchant || "利用先未入力"}
                </button>
              ) : (
                <b>{entry.merchant || "利用先未入力"}</b>
              )}
              <small>
                {entry.occurredAt.slice(0, 10)} ·{" "}
                {isRefund
                  ? "返品・返金"
                  : isTransfer
                    ? "口座間振替"
                    : isCardBillPayment
                      ? "カード引落し"
                      : isExternalIncome
                        ? "外部入金"
                        : isLoan
                          ? "貸付"
                          : isRepayment
                            ? "貸付返済"
                            : entry.kind === "CARD_PURCHASE"
                              ? "カード利用"
                              : entry.kind}
                {entry.cardLast4 ? ` · •••• ${entry.cardLast4}` : ""}
              </small>
            </div>
            <div className="statement-row-action">
              <strong>{yen(entry.amount)}</strong>
              {isRefund ? (
                issue ? (
                  <small>{issue}</small>
                ) : confirmed ? (
                  <small>返品登録済み</small>
                ) : (
                  <>
                    <label>
                      <span>返品先</span>
                      <select
                        value={refundTarget.purchaseId}
                        disabled={pending}
                        onChange={(event) =>
                          setRefundTargets((current) => ({
                            ...current,
                            [key]: { purchaseId: event.target.value, itemId: "" },
                          }))
                        }
                      >
                        <option value="">買い物を選択</option>
                        {purchases
                          .filter((purchase) => purchase.status !== "DRAFT")
                          .map((purchase) => (
                            <option key={purchase.id} value={purchase.id}>
                              {new Date(purchase.occurredAt).toLocaleDateString("ja-JP", {
                                timeZone: "Asia/Tokyo",
                              })}{" "}
                              {purchase.merchant} · {yen(purchase.totalYen)}
                            </option>
                          ))}
                      </select>
                    </label>
                    {refundTarget.purchaseId && (
                      <label>
                        <span>品目</span>
                        <select
                          value={refundTarget.itemId}
                          disabled={pending}
                          onChange={(event) =>
                            setRefundTargets((current) => ({
                              ...current,
                              [key]: { ...refundTarget, itemId: event.target.value },
                            }))
                          }
                        >
                          <option value="">買い物全体</option>
                          {purchases
                            .find((purchase) => purchase.id === refundTarget.purchaseId)
                            ?.items.map((item) => (
                              <option key={item.id} value={item.id}>
                                {item.name} · {yen(item.amountYen)}
                              </option>
                            ))}
                        </select>
                      </label>
                    )}
                    <button
                      type="button"
                      className="text-button"
                      disabled={pending || !refundTarget.purchaseId}
                      onClick={() =>
                        onConfirmRefund(
                          entry,
                          refundTarget.purchaseId,
                          refundTarget.itemId || undefined,
                        )
                      }
                    >
                      返品を記録
                    </button>
                    <button
                      type="button"
                      className="text-button"
                      disabled={pending || refundCorrectionPage.loading}
                      aria-expanded={openRefundCorrections[key]}
                      onClick={() => toggleRefundCorrections(entry)}
                    >
                      {refundCorrectionPage.loading
                        ? "読み込み中"
                        : openRefundCorrections[key]
                          ? "候補を閉じる"
                          : "既存の返金記録を探す"}
                    </button>
                    {openRefundCorrections[key] && (
                      <>
                        {refundCandidates.length > 0 ? (
                          <>
                            <label>
                              <span>既存の返金記録</span>
                              <select
                                value={refundCorrectionTargets[key] ?? ""}
                                disabled={pending}
                                onChange={(event) =>
                                  setRefundCorrectionTargets((current) => ({
                                    ...current,
                                    [key]: event.target.value,
                                  }))
                                }
                              >
                                <option value="">返金記録を選択</option>
                                {refundCandidates.map(({ node }) => {
                                  const label = [
                                    `${new Date(node.occurredAt).toLocaleDateString("ja-JP", { timeZone: "Asia/Tokyo" })} ${node.merchant} · ${yen(Number(node.amountYen))}`,
                                    node.sourceFilename,
                                  ]
                                    .filter(Boolean)
                                    .join(" · ");
                                  return (
                                    <option key={node.id} value={node.id}>
                                      {label}
                                    </option>
                                  );
                                })}
                              </select>
                            </label>
                            <button
                              type="button"
                              className="text-button"
                              disabled={pending || !refundCorrectionTargets[key]}
                              onClick={() =>
                                onLinkRefund(entry, refundCorrectionTargets[key] ?? "")
                              }
                            >
                              既存の返金に紐付ける
                            </button>
                          </>
                        ) : null}
                        {refundCorrectionErrors[key] && (
                          <small role="alert">{refundCorrectionErrors[key]}</small>
                        )}
                        {refundCorrectionPage.hasNextPage && (
                          <button
                            type="button"
                            className="text-button"
                            disabled={pending || refundCorrectionPage.loading}
                            onClick={() =>
                              loadRefundCorrections(entry, refundCorrectionPage.after ?? undefined)
                            }
                          >
                            {refundCorrectionPage.loading ? "読み込み中" : "候補をさらに表示"}
                          </button>
                        )}
                      </>
                    )}
                  </>
                )
              ) : isTransfer || isCardBillPayment || isLoan || isRepayment ? (
                issue ? (
                  <small>{issue}</small>
                ) : confirmed ? (
                  <small>
                    {isCardBillPayment
                      ? "カード引落し登録済み"
                      : isLoan
                        ? "貸付登録済み"
                        : isRepayment
                          ? "返済登録済み"
                          : "振替登録済み"}
                  </small>
                ) : (
                  <>
                    <label>
                      <span>
                        {isLoan || isRepayment
                          ? "明細の銀行口座"
                          : isCardBillPayment
                            ? "引落口座"
                            : entry.amount < 0
                              ? "振替元の明細口座"
                              : "振替先の明細口座"}
                      </span>
                      <select
                        value={transferAccounts.statementAccountId}
                        disabled={pending}
                        onChange={(event) =>
                          setStatementAccounts((current) => ({
                            ...current,
                            [key]: { statementAccountId: event.target.value, otherAccountId: "" },
                          }))
                        }
                      >
                        <option value="">口座を選択</option>
                        {accounts
                          .filter((account) =>
                            isCardBillPayment || isLoan || isRepayment
                              ? account.kind === "BANK"
                              : account.kind !== "CARD",
                          )
                          .map((account) => (
                            <option key={account.id} value={account.id}>
                              {account.name}
                            </option>
                          ))}
                      </select>
                    </label>
                    {(isTransfer || isCardBillPayment) && (
                      <label>
                        <span>
                          {isCardBillPayment
                            ? "引落対象のカード"
                            : entry.amount < 0
                              ? "振替先"
                              : "振替元"}
                        </span>
                        <select
                          value={transferAccounts.otherAccountId}
                          disabled={pending || !transferAccounts.statementAccountId}
                          onChange={(event) =>
                            setStatementAccounts((current) => ({
                              ...current,
                              [key]: { ...transferAccounts, otherAccountId: event.target.value },
                            }))
                          }
                        >
                          <option value="">口座を選択</option>
                          {accounts
                            .filter(
                              (account) =>
                                (isCardBillPayment
                                  ? account.kind === "CARD"
                                  : account.kind !== "CARD") &&
                                account.id !== transferAccounts.statementAccountId,
                            )
                            .map((account) => (
                              <option key={account.id} value={account.id}>
                                {account.name}
                              </option>
                            ))}
                        </select>
                      </label>
                    )}
                    {isLoan && (
                      <>
                        <label>
                          <span>貸主</span>
                          <select
                            value={loanDraft.fromMemberId ?? ""}
                            disabled={pending}
                            onChange={(event) =>
                              setStatementAccounts((current) => ({
                                ...current,
                                [key]: { ...transferAccounts, fromMemberId: event.target.value },
                              }))
                            }
                          >
                            <option value="">メンバーを選択</option>
                            {household.members.map((member) => (
                              <option key={member.id} value={member.id}>
                                {member.displayName}
                              </option>
                            ))}
                          </select>
                        </label>
                        <label>
                          <span>借主</span>
                          <select
                            value={loanDraft.toMemberId ?? ""}
                            disabled={pending}
                            onChange={(event) =>
                              setStatementAccounts((current) => ({
                                ...current,
                                [key]: { ...transferAccounts, toMemberId: event.target.value },
                              }))
                            }
                          >
                            <option value="">メンバーを選択</option>
                            {household.members
                              .filter((member) => member.id !== loanDraft.fromMemberId)
                              .map((member) => (
                                <option key={member.id} value={member.id}>
                                  {member.displayName}
                                </option>
                              ))}
                          </select>
                        </label>
                      </>
                    )}
                    {isRepayment && (
                      <label>
                        <span>返済対象の貸付</span>
                        <select
                          value={loanDraft.loanId ?? ""}
                          disabled={pending}
                          onChange={(event) =>
                            setStatementAccounts((current) => ({
                              ...current,
                              [key]: { ...transferAccounts, loanId: event.target.value },
                            }))
                          }
                        >
                          <option value="">貸付を選択</option>
                          {loans
                            .filter((loan) => loan.balanceYen > 0)
                            .map((loan) => (
                              <option key={loan.id} value={loan.id}>
                                {
                                  household.members.find(
                                    (member) => member.id === loan.lenderMemberId,
                                  )?.displayName
                                }{" "}
                                →{" "}
                                {
                                  household.members.find(
                                    (member) => member.id === loan.borrowerMemberId,
                                  )?.displayName
                                }{" "}
                                · {yen(loan.balanceYen)}
                              </option>
                            ))}
                        </select>
                      </label>
                    )}
                    {matchingTransfers.length > 0 && (
                      <label>
                        <span>既存の取引</span>
                        <select
                          value={selectedTransferId}
                          disabled={pending}
                          onChange={(event) =>
                            setStatementAccounts((current) => ({
                              ...current,
                              [key]: {
                                ...transferAccounts,
                                existingTransferId: event.target.value,
                              },
                            }))
                          }
                        >
                          <option value="">新しい取引として記録</option>
                          {matchingTransfers.map((transfer) => (
                            <option key={transfer.id} value={transfer.id}>
                              {transfer.occurredAt.slice(0, 10)} · {transfer.description}
                            </option>
                          ))}
                        </select>
                      </label>
                    )}
                    {(isTransfer || isCardBillPayment) && matchingMovements.length > 0 && (
                      <label>
                        <span>既存の口座移動</span>
                        <select
                          value={selectedMovementId}
                          disabled={pending}
                          onChange={(event) =>
                            setStatementAccounts((current) => ({
                              ...current,
                              [key]: {
                                ...transferAccounts,
                                existingMovementId: event.target.value,
                              },
                            }))
                          }
                        >
                          <option value="">新しい口座移動を記録</option>
                          {matchingMovements.map((movement) => (
                            <option key={movement.id} value={movement.id}>
                              {movement.occurredAt.slice(0, 10)} · {movement.description}
                            </option>
                          ))}
                        </select>
                      </label>
                    )}
                    <button
                      type="button"
                      className="text-button"
                      disabled={
                        pending ||
                        !transferAccounts.statementAccountId ||
                        (!selectedTransferId &&
                          !selectedMovementId &&
                          (isTransfer || isCardBillPayment
                            ? !transferAccounts.otherAccountId
                            : isLoan
                              ? !loanDraft.fromMemberId ||
                                !loanDraft.toMemberId ||
                                loanDraft.fromMemberId === loanDraft.toMemberId
                              : !loans.find(
                                  (loan) => loan.id === loanDraft.loanId && loan.balanceYen > 0,
                                )))
                      }
                      onClick={() =>
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
                    >
                      {selectedMovementId
                        ? "既存の口座移動に紐付け"
                        : selectedTransferId
                          ? "既存の取引に紐付け"
                          : isCardBillPayment
                            ? "カード引落しを記録"
                            : isLoan
                              ? "貸付を記録"
                              : isRepayment
                                ? "返済を記録"
                                : "振替を記録"}
                    </button>
                  </>
                )
              ) : (
                (entry.kind === "CARD_PURCHASE" || isExternalIncome) &&
                (issue ? (
                  <small>{issue}</small>
                ) : confirmed ? (
                  <small>{isExternalIncome ? "入金登録済み" : "追加済み"}</small>
                ) : (
                  <>
                    <label>
                      <span>{isExternalIncome ? "入金先の銀行口座" : "決済口座"}</span>
                      <select
                        value={paymentAccountId}
                        disabled={pending}
                        onChange={(event) => {
                          setPaymentAccounts((current) => ({
                            ...current,
                            [key]: event.target.value,
                          }));
                          setSelectedIncomeIds((current) => ({ ...current, [key]: "" }));
                        }}
                      >
                        <option value="">口座を選択</option>
                        {accounts
                          .filter((account) => !isExternalIncome || account.kind === "BANK")
                          .map((account) => (
                            <option key={account.id} value={account.id}>
                              {account.name}
                            </option>
                          ))}
                      </select>
                    </label>
                    {isExternalIncome && matchingIncomes.length > 0 && (
                      <label>
                        <span>既存の入金</span>
                        <select
                          value={selectedIncomeId}
                          disabled={pending}
                          onChange={(event) =>
                            setSelectedIncomeIds((current) => ({
                              ...current,
                              [key]: event.target.value,
                            }))
                          }
                        >
                          <option value="">新しい入金として記録</option>
                          {matchingIncomes.map((income) => (
                            <option key={income.id} value={income.id}>
                              {income.occurredAt.slice(0, 10)} · {income.description}
                            </option>
                          ))}
                        </select>
                      </label>
                    )}
                    {isExternalIncome && hasMoreIncomeCandidates && (
                      <button
                        type="button"
                        className="text-button"
                        disabled={loadingIncomeCandidates}
                        onClick={loadMoreIncomeCandidates}
                      >
                        {loadingIncomeCandidates ? "読み込み中" : "過去の入金を読み込む"}
                      </button>
                    )}
                    {isExternalIncome && candidatePageLoadErrors.incomes && (
                      <p className="inline-error" role="alert">
                        過去の入金を読み込めませんでした
                        <button
                          type="button"
                          className="text-button"
                          onClick={loadMoreIncomeCandidates}
                        >
                          再試行
                        </button>
                      </p>
                    )}
                    <button
                      type="button"
                      className="text-button"
                      disabled={pending || !paymentAccountId}
                      onClick={() =>
                        selectedIncomeId
                          ? onLinkExistingIncome(entry, paymentAccountId, selectedIncomeId)
                          : onConfirm(entry, paymentAccountId)
                      }
                    >
                      {isExternalIncome
                        ? selectedIncomeId
                          ? "既存の入金に紐付け"
                          : "入金を記録"
                        : "購入に追加"}
                    </button>
                  </>
                ))
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
