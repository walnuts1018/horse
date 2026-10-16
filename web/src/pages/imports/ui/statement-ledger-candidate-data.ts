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
  type AccountMovementsQuery,
  type AccountIncomesQuery,
  type Household,
  type LoanBalancesQuery,
  type StatementTransferCandidatesQuery,
} from "../../../shared/api";

export function useStatementLedgerCandidateData(household: Household) {
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
    Array<{
      id: string;
      lenderMemberId: string;
      borrowerMemberId: string;
      balanceYen: number;
    }>
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
      error: () =>
        setCandidateLoadErrors((current) => ({
          ...current,
          movements: true,
        })),
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
      error: () =>
        setCandidateLoadErrors((current) => ({
          ...current,
          transfers: true,
        })),
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
        setCandidatePageLoadErrors((current) => ({
          ...current,
          transfers: true,
        }));
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
        setCandidatePageLoadErrors((current) => ({
          ...current,
          movements: true,
        }));
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
        setCandidatePageLoadErrors((current) => ({
          ...current,
          incomes: true,
        }));
      },
      complete: () => setLoadingIncomeCandidates(false),
    });
  };

  return {
    paymentAccounts,
    setPaymentAccounts,
    statementAccounts,
    setStatementAccounts,
    loans,
    transferCandidates,
    hasMoreTransferCandidates,
    loadingTransferCandidates,
    movementCandidates,
    hasMoreMovementCandidates,
    loadingMovementCandidates,
    incomeCandidates,
    hasMoreIncomeCandidates,
    loadingIncomeCandidates,
    selectedIncomeIds,
    setSelectedIncomeIds,
    candidateLoadErrors,
    candidatePageLoadErrors,
    loadLoanCandidates,
    loadIncomeCandidates,
    loadMovementCandidates,
    loadTransferCandidates,
    loadMoreTransferCandidates,
    loadMoreMovementCandidates,
    loadMoreIncomeCandidates,
  };
}
