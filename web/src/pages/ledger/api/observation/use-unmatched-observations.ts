import { useCallback, useEffect, useRef, useState } from "react";
import { fetchQuery } from "react-relay";

import {
  getRelayEnvironment,
  type SourceKind,
  type UnmatchedObservationsQuery,
  type UnmatchedObservationsQuery$data,
  type UnmatchedObservation,
  type UnmatchedObservationState,
} from "../../../../shared/api";
import { errorMessage } from "../../../../shared/lib";
import { unmatchedObservationsQuery } from "./operations";

type ObservationRecord =
  UnmatchedObservationsQuery$data["unmatchedObservations"]["edges"][number]["node"];

type Options = {
  enabled?: boolean;
  from?: string;
  to?: string;
  sourceKind?: SourceKind;
  state?: UnmatchedObservationState;
  first?: number;
};

export function useUnmatchedObservations(householdId: string, options: Options = {}) {
  const [observations, setObservations] = useState<UnmatchedObservation[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [hasNextPage, setHasNextPage] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");
  const [moreError, setMoreError] = useState("");
  const [dataKey, setDataKey] = useState("");
  const cursor = useRef<string | null>(null);
  const generation = useRef(0);
  const requestInProgress = useRef(false);
  const queryKey = [
    householdId,
    options.from ?? "",
    options.to ?? "",
    options.sourceKind ?? "",
    options.state ?? "",
    options.first ?? 50,
  ].join(":");

  const refresh = useCallback(async () => {
    if (options.enabled === false) {
      setLoading(false);
      return;
    }
    const currentGeneration = ++generation.current;
    cursor.current = null;
    requestInProgress.current = true;
    setLoading(true);
    setLoadingMore(false);
    setError("");
    setMoreError("");
    try {
      const result: UnmatchedObservationsQuery$data | undefined =
        await fetchQuery<UnmatchedObservationsQuery>(
          getRelayEnvironment(),
          unmatchedObservationsQuery,
          {
            householdId,
            from: options.from,
            to: options.to,
            sourceKind: options.sourceKind,
            state: options.state,
            first: options.first ?? 50,
          },
          { fetchPolicy: "network-only" },
        ).toPromise();
      if (!result) throw new Error("未照合候補を取得できませんでした");
      if (generation.current !== currentGeneration) return;
      const page = result.unmatchedObservations;
      setObservations(page.edges.map(({ node }) => mapObservation(node)));
      setTotalCount(page.totalCount);
      setDataKey(queryKey);
      cursor.current = page.pageInfo.endCursor ?? null;
      setHasNextPage(page.pageInfo.hasNextPage && Boolean(cursor.current));
    } catch (reason) {
      if (generation.current === currentGeneration)
        setError(errorMessage(reason) || "未照合候補を取得できませんでした");
    } finally {
      if (generation.current === currentGeneration) {
        requestInProgress.current = false;
        setLoading(false);
      }
    }
  }, [
    householdId,
    options.enabled,
    options.from,
    options.to,
    options.sourceKind,
    options.state,
    options.first,
    queryKey,
  ]);

  const loadMore = useCallback(async () => {
    const after = cursor.current;
    if (options.enabled === false || !after || requestInProgress.current || !hasNextPage) return;
    const currentGeneration = generation.current;
    requestInProgress.current = true;
    setLoadingMore(true);
    setMoreError("");
    try {
      const result: UnmatchedObservationsQuery$data | undefined =
        await fetchQuery<UnmatchedObservationsQuery>(
          getRelayEnvironment(),
          unmatchedObservationsQuery,
          {
            householdId,
            from: options.from,
            to: options.to,
            sourceKind: options.sourceKind,
            state: options.state,
            after,
            first: options.first ?? 50,
          },
          { fetchPolicy: "network-only" },
        ).toPromise();
      if (!result) throw new Error("未照合候補を取得できませんでした");
      if (generation.current !== currentGeneration) return;
      const page = result.unmatchedObservations;
      setObservations((current) => [
        ...current,
        ...page.edges.map(({ node }) => mapObservation(node)),
      ]);
      setTotalCount(page.totalCount);
      setDataKey(queryKey);
      cursor.current = page.pageInfo.endCursor ?? null;
      setHasNextPage(
        page.pageInfo.hasNextPage && Boolean(cursor.current) && cursor.current !== after,
      );
    } catch (reason) {
      if (generation.current === currentGeneration)
        setMoreError(errorMessage(reason) || "未照合候補を取得できませんでした");
    } finally {
      if (generation.current === currentGeneration) {
        requestInProgress.current = false;
        setLoadingMore(false);
      }
    }
  }, [
    hasNextPage,
    householdId,
    options.enabled,
    options.from,
    options.to,
    options.sourceKind,
    options.state,
    options.first,
    queryKey,
  ]);

  useEffect(() => {
    // effect本体での同期的なsetStateを避けるため、取得の開始を1tick遅延させる
    let cancelled = false;
    queueMicrotask(() => {
      if (!cancelled) void refresh();
    });
    return () => {
      cancelled = true;
      generation.current += 1;
      requestInProgress.current = false;
    };
  }, [refresh]);

  return {
    observations: dataKey === queryKey ? observations : [],
    totalCount: dataKey === queryKey ? totalCount : 0,
    hasNextPage: dataKey === queryKey && hasNextPage,
    loading,
    loadingMore,
    error,
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
