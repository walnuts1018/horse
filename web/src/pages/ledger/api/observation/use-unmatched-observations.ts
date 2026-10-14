import { useCallback, useMemo, useState } from "react";
import { useLazyLoadQuery, usePaginationFragment } from "react-relay";

import type {
  SourceKind,
  UnmatchedObservation,
  UnmatchedObservationState,
  UnmatchedObservationsFragment$data,
  UnmatchedObservationsFragment$key,
  UnmatchedObservationsQuery,
} from "../../../../shared/api";

import { unmatchedObservationPageSize } from "../../model/observation-page-size";
import { unmatchedObservationsFragment, unmatchedObservationsQuery } from "./operations";

type ObservationRecord =
  UnmatchedObservationsFragment$data["unmatchedObservations"]["edges"][number]["node"];

type Options = {
  enabled?: boolean;
  from?: string;
  to?: string;
  sourceKind?: SourceKind;
  state?: UnmatchedObservationState;
  first?: number;
};

export function useUnmatchedObservations(householdId: string, options: Options = {}) {
  const variables = useMemo(
    () => ({
      householdId,
      from: options.from,
      to: options.to,
      sourceKind: options.sourceKind,
      state: options.state,
      first: unmatchedObservationPageSize(options.enabled, options.first),
    }),
    [
      householdId,
      options.enabled,
      options.first,
      options.from,
      options.sourceKind,
      options.state,
      options.to,
    ],
  );
  const query = useLazyLoadQuery<UnmatchedObservationsQuery>(
    unmatchedObservationsQuery,
    variables,
    { fetchPolicy: "store-or-network" },
  );
  const { data, hasNext, isLoadingNext, loadNext, refetch } = usePaginationFragment<
    UnmatchedObservationsQuery,
    UnmatchedObservationsFragment$key
  >(unmatchedObservationsFragment, query);
  const [loading, setLoading] = useState(false);
  const [moreError, setMoreError] = useState("");
  const [refreshError, setRefreshError] = useState("");
  const page = data.unmatchedObservations;
  const observations = page.edges.map(({ node }) => mapObservation(node));

  const refresh = useCallback(() => {
    setLoading(true);
    setRefreshError("");
    setMoreError("");
    refetch(variables, {
      fetchPolicy: "network-only",
      onComplete: (error) => {
        if (error) setRefreshError(error.message || "未照合候補を取得できませんでした");
        setLoading(false);
      },
    });
  }, [refetch, variables]);

  const loadMore = useCallback(() => {
    if (!hasNext || isLoadingNext) return;
    setMoreError("");
    loadNext(options.first ?? 50, {
      onComplete: (error) => {
        if (error) setMoreError(error.message || "未照合候補を取得できませんでした");
      },
    });
  }, [hasNext, isLoadingNext, loadNext, options.first]);

  return {
    observations: options.enabled === false ? [] : observations,
    totalCount: options.enabled === false ? 0 : page.totalCount,
    hasNextPage: options.enabled !== false && hasNext,
    loading,
    loadingMore: isLoadingNext,
    error: refreshError,
    moreError,
    refresh,
    loadMore,
  };
}

function mapObservation(record: ObservationRecord): UnmatchedObservation {
  const fields = {
    documentId: record.documentId,
    sourceReference: record.sourceReference,
    sourceKind: mapSourceKind(record.sourceKind),
    merchant: record.merchant,
    occurredAt: record.occurredAt,
    amountYen: Number(record.amountYen),
    orderId: record.orderId,
    transactionId: record.transactionId,
  };
  switch (record.__typename) {
    case "UnmatchedObservation":
      return { __typename: record.__typename, ...fields };
    case "ConfirmedObservation":
      if (!record.purchase) throw new Error("照合済み候補に購入情報がありません");
      return {
        __typename: record.__typename,
        ...fields,
        purchase: {
          id: record.purchase.id,
          merchant: record.purchase.merchant,
          occurredAt: record.purchase.occurredAt,
        },
      };
    default:
      throw new Error(`未対応の照合候補型です: ${record.__typename}`);
  }
}

function mapSourceKind(sourceKind: ObservationRecord["sourceKind"]): SourceKind {
  switch (sourceKind) {
    case "BANK":
    case "CARD":
    case "EMAIL":
    case "ORDER":
    case "RECEIPT":
      return sourceKind;
    case "%future added value":
    default:
      throw new Error(`未対応の照合元です: ${sourceKind}`);
  }
}
