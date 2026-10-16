import { useMemo, useState } from "react";
import { useLazyLoadQuery, usePaginationFragment } from "react-relay";

import type {
  AnalysisEvidenceQuery,
  AnalysisUnbackedPurchasesFragment$key,
  AnalysisUnmatchedObservationsFragment$key,
} from "../../../shared/api";

import {
  analysisEvidenceQuery,
  analysisUnbackedPurchasesFragment,
  analysisUnmatchedObservationsFragment,
} from "../api/operations";
import { exclusiveMonthEnd } from "./use-analysis-period";

export function useAnalysisEvidence(householdId: string, fromMonth: string, toMonth: string) {
  const variables = useMemo(
    () => ({
      householdId,
      from: fromMonth,
      to: toMonth,
      purchaseFilter: {
        householdId,
        from: `${fromMonth}-01T00:00:00+09:00`,
        to: exclusiveMonthEnd(toMonth),
        status: "POSTED" as const,
        hasEvidence: false,
      },
    }),
    [fromMonth, householdId, toMonth],
  );
  const query = useLazyLoadQuery<AnalysisEvidenceQuery>(analysisEvidenceQuery, variables, {
    fetchPolicy: "network-only",
  });
  const unbacked = usePaginationFragment<
    AnalysisEvidenceQuery,
    AnalysisUnbackedPurchasesFragment$key
  >(analysisUnbackedPurchasesFragment, query);
  const unmatched = usePaginationFragment<
    AnalysisEvidenceQuery,
    AnalysisUnmatchedObservationsFragment$key
  >(analysisUnmatchedObservationsFragment, query);
  const requestKey = JSON.stringify(variables);
  const [errorState, setErrorState] = useState<{ key: string; message: string } | null>(null);
  const error = errorState?.key === requestKey ? errorState.message : "";

  const loadMore = (load: typeof unbacked.loadNext) => {
    load(8, {
      onComplete: (loadError) => {
        if (loadError) setErrorState({ key: requestKey, message: loadError.message });
      },
    });
  };

  return {
    unbackedPurchases: unbacked.data.purchases.edges.map(({ node }) => ({
      id: node.id,
      occurredAt: node.occurredAt,
      merchant: node.merchant,
      totalYen: Number(node.totalYen),
    })),
    unbackedCount: unbacked.data.purchases.totalCount,
    hasMoreUnbacked: unbacked.hasNext,
    loadingMoreUnbacked: unbacked.isLoadingNext,
    loadMoreUnbacked: () => loadMore(unbacked.loadNext),
    unmatchedObservations: unmatched.data.unmatchedObservations.edges.map(({ node }) => {
      if (
        node.__typename !== "UnmatchedObservation" ||
        node.sourceKind == null ||
        node.merchant == null ||
        node.amountYen == null ||
        node.orderId == null ||
        node.transactionId == null
      ) {
        throw new Error("未照合取引の応答が不正です");
      }
      return {
        id: node.id,
        sourceKind: node.sourceKind,
        merchant: node.merchant,
        occurredAt: node.occurredAt ?? null,
        amountYen: Number(node.amountYen),
        orderId: node.orderId,
        transactionId: node.transactionId,
      };
    }),
    unmatchedCount: unmatched.data.unmatchedObservations.totalCount,
    hasMoreUnmatched: unmatched.hasNext,
    loadingMoreUnmatched: unmatched.isLoadingNext,
    loadMoreUnmatched: () => loadMore(unmatched.loadNext),
    error,
  };
}
