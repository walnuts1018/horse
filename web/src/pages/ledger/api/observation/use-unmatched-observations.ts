import { useCallback, useEffect, useRef, useState } from "react";
import {
  readGraphql,
  type Connection,
  type SourceKind,
  type UnmatchedObservation,
  type UnmatchedObservationState,
} from "../../../../shared/api";
import { unmatchedObservationsQuery } from "./operations";

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
      const result = await readGraphql<{ unmatchedObservations: Connection<UnmatchedObservation> }>(
        unmatchedObservationsQuery,
        {
          householdId,
          from: options.from,
          to: options.to,
          sourceKind: options.sourceKind,
          state: options.state,
          first: options.first ?? 50,
        },
      );
      if (generation.current !== currentGeneration) return;
      const page = result.unmatchedObservations;
      setObservations(page.edges.map((edge) => edge.node));
      setTotalCount(page.totalCount);
      setDataKey(queryKey);
      cursor.current = page.pageInfo.endCursor ?? null;
      setHasNextPage(page.pageInfo.hasNextPage && Boolean(cursor.current));
    } catch (reason) {
      if (generation.current === currentGeneration)
        setError((reason as Error).message || "未照合候補を取得できませんでした");
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
      const result = await readGraphql<{ unmatchedObservations: Connection<UnmatchedObservation> }>(
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
      );
      if (generation.current !== currentGeneration) return;
      const page = result.unmatchedObservations;
      setObservations((current) => [...current, ...page.edges.map((edge) => edge.node)]);
      setTotalCount(page.totalCount);
      setDataKey(queryKey);
      cursor.current = page.pageInfo.endCursor ?? null;
      setHasNextPage(
        page.pageInfo.hasNextPage && Boolean(cursor.current) && cursor.current !== after,
      );
    } catch (reason) {
      if (generation.current === currentGeneration)
        setMoreError((reason as Error).message || "未照合候補を取得できませんでした");
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
    void refresh();
    return () => {
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
