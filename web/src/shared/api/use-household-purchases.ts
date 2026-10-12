import { useCallback, useEffect, useRef, useState } from "react";
import { purchasesQuery, readGraphql, type Connection, type Purchase } from "./household";

type PurchasePage = Connection<Purchase>;

export function useHouseholdPurchases(
  householdId: string,
  first = 100,
  options: {
    from?: string;
    to?: string;
    origin?: Purchase["origin"];
    hasEvidence?: boolean;
    loadAll?: boolean;
  } = {},
) {
  const [purchases, setPurchases] = useState<Purchase[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");
  const [moreError, setMoreError] = useState("");
  const [hasNextPage, setHasNextPage] = useState(false);
  const [totalCount, setTotalCount] = useState(0);
  const [dataQueryKey, setDataQueryKey] = useState("");
  const cursor = useRef<string | null>(null);
  const generation = useRef(0);
  const requestInProgress = useRef(false);
  const loadedPages = useRef(1);
  const pageSetKey = useRef("");
  const currentQueryKey = `${householdId}:${first}:${options.from ?? ""}:${options.to ?? ""}:${options.origin ?? ""}:${options.hasEvidence ?? ""}`;

  const refresh = useCallback(async () => {
    const key = currentQueryKey;
    const pagesToRestore = pageSetKey.current === key ? loadedPages.current : 1;
    pageSetKey.current = key;
    const currentGeneration = ++generation.current;
    cursor.current = null;
    loadedPages.current = 1;
    requestInProgress.current = pagesToRestore > 1;
    setLoading(true);
    setLoadingMore(pagesToRestore > 1);
    setError("");
    setMoreError("");
    setHasNextPage(false);
    let after: string | null = null;
    let accumulated: Purchase[] = [];
    let pageIndex = 0;
    try {
      while (pageIndex < pagesToRestore) {
        const result: { purchases: PurchasePage } = await readGraphql(purchasesQuery, {
          householdId,
          first,
          after,
          from: options.from,
          to: options.to,
          origin: options.origin,
          hasEvidence: options.hasEvidence,
        });
        if (generation.current !== currentGeneration) return;
        const page: PurchasePage = result.purchases;
        const seen = new Set(accumulated.map((purchase) => purchase.id));
        accumulated = [
          ...accumulated,
          ...page.edges.map((edge) => edge.node).filter((purchase) => !seen.has(purchase.id)),
        ];
        setPurchases(accumulated);
        setTotalCount(page.totalCount);
        setDataQueryKey(key);
        const nextCursor: string | null = page.pageInfo.endCursor ?? null;
        cursor.current = nextCursor;
        const more = page.pageInfo.hasNextPage && Boolean(nextCursor) && nextCursor !== after;
        setHasNextPage(more);
        loadedPages.current = pageIndex + 1;
        pageIndex += 1;
        if (!more) break;
        after = nextCursor;
      }
    } catch (reason) {
      if (generation.current === currentGeneration && pageIndex === 0)
        setError((reason as Error).message || "購入履歴を取得できませんでした");
      else if (generation.current === currentGeneration)
        setMoreError((reason as Error).message || "購入履歴を取得できませんでした");
    } finally {
      if (generation.current === currentGeneration) {
        requestInProgress.current = false;
        setLoading(false);
        setLoadingMore(false);
      }
    }
  }, [
    currentQueryKey,
    householdId,
    first,
    options.from,
    options.to,
    options.origin,
    options.hasEvidence,
  ]);

  const loadMore = useCallback(async () => {
    if (!cursor.current || requestInProgress.current || !hasNextPage) return;
    const after = cursor.current;
    const currentGeneration = generation.current;
    requestInProgress.current = true;
    setLoadingMore(true);
    setMoreError("");
    try {
      const result = await readGraphql<{ purchases: PurchasePage }>(purchasesQuery, {
        householdId,
        first,
        after,
        from: options.from,
        to: options.to,
        origin: options.origin,
        hasEvidence: options.hasEvidence,
      });
      if (generation.current !== currentGeneration) return;
      const page = result.purchases;
      setTotalCount(page.totalCount);
      setDataQueryKey(currentQueryKey);
      setPurchases((current) => {
        const seen = new Set(current.map((purchase) => purchase.id));
        return [
          ...current,
          ...page.edges.map((edge) => edge.node).filter((purchase) => !seen.has(purchase.id)),
        ];
      });
      const nextCursor = page.pageInfo.endCursor ?? null;
      cursor.current = nextCursor;
      setHasNextPage(page.pageInfo.hasNextPage && Boolean(nextCursor) && nextCursor !== after);
      if (nextCursor && nextCursor !== after) loadedPages.current += 1;
    } catch (reason) {
      if (generation.current === currentGeneration)
        setMoreError((reason as Error).message || "購入履歴を取得できませんでした");
    } finally {
      if (generation.current === currentGeneration) {
        requestInProgress.current = false;
        setLoadingMore(false);
      }
    }
  }, [
    first,
    hasNextPage,
    householdId,
    options.from,
    options.to,
    options.origin,
    options.hasEvidence,
    currentQueryKey,
  ]);

  useEffect(() => {
    void refresh();
    return () => {
      generation.current += 1;
      requestInProgress.current = false;
    };
  }, [refresh]);

  useEffect(() => {
    const events = new EventSource(`/events?householdId=${encodeURIComponent(householdId)}`);
    events.onmessage = () => void refresh();
    return () => events.close();
  }, [householdId, refresh]);

  useEffect(() => {
    if (options.loadAll && hasNextPage && !loadingMore && !moreError) void loadMore();
  }, [hasNextPage, loadMore, loadingMore, moreError, options.loadAll]);

  return {
    purchases: dataQueryKey === currentQueryKey ? purchases : [],
    loading,
    loadingMore,
    error,
    moreError,
    hasNextPage,
    totalCount: dataQueryKey === currentQueryKey ? totalCount : 0,
    loadMore,
    refresh,
  };
}
