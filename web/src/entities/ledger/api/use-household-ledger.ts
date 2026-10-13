import { useCallback, useMemo, useState } from "react";
import { useLazyLoadQuery, usePaginationFragment } from "react-relay";

import type { HouseholdLedgerFragment$key, HouseholdLedgerQuery } from "..";
import type { LedgerEntry } from "../../../shared/api";

import { householdLedgerFragment, householdLedgerQuery } from "./operations";

export function useHouseholdLedger(householdId: string, first = 100) {
  const variables = useMemo(() => ({ householdId, first }), [first, householdId]);
  const query = useLazyLoadQuery<HouseholdLedgerQuery>(householdLedgerQuery, variables, {
    fetchPolicy: "store-or-network",
  });
  const { data, hasNext, isLoadingNext, loadNext, refetch } = usePaginationFragment<
    HouseholdLedgerQuery,
    HouseholdLedgerFragment$key
  >(householdLedgerFragment, query);
  const [moreError, setMoreError] = useState("");
  const entries: LedgerEntry[] = data.ledger.edges.map(({ node }) => {
    const kind =
      node.__typename === "FundingLedgerEntry"
        ? "FUNDING"
        : node.__typename === "PurchaseLedgerEntry"
          ? "PURCHASE"
          : node.__typename === "CorrectionLedgerEntry"
            ? "CORRECTION"
            : node.__typename === "AdvanceTransferLedgerEntry"
              ? "ADVANCE"
              : node.__typename === "LoanTransferLedgerEntry"
                ? "LOAN"
                : node.__typename === "RepaymentTransferLedgerEntry"
                  ? "REPAYMENT"
                  : "GIFT";
    return {
      id: node.id,
      kind,
      occurredAt: node.occurredAt,
      description: node.description,
      amountYen: node.amountYen,
      currency: node.currency,
      fromMemberId: node.fromMember?.id ?? null,
      toMemberId: node.toMember?.id ?? null,
      poolId: node.pool?.id ?? null,
      sourcePurchaseId: node.purchase?.id ?? null,
    };
  });
  const refresh = useCallback(() => {
    setMoreError("");
    refetch(variables, { fetchPolicy: "network-only" });
  }, [refetch, variables]);
  const loadMore = useCallback(() => {
    if (!hasNext || isLoadingNext) return;
    setMoreError("");
    loadNext(first, {
      onComplete: (error) => {
        if (error) setMoreError(error.message || "入金・送金の記録を取得できませんでした");
      },
    });
  }, [first, hasNext, isLoadingNext, loadNext]);

  return {
    entries,
    hasNextPage: hasNext,
    loadingMore: isLoadingNext,
    moreError,
    loadMore,
    refresh,
  };
}
