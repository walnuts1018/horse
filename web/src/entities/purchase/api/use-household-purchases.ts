import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  fetchQuery,
  requestSubscription,
  useLazyLoadQuery,
  usePaginationFragment,
} from "react-relay";

import type {
  PurchaseChangesSubscription,
  PurchaseCorrectionsPaginationQuery,
  PurchaseListFragment$key,
  PurchaseAmountTarget,
  PurchasesQuery,
} from "..";
import type { Purchase, PurchaseOrigin } from "../../../shared/api";

import { getRelayEnvironment } from "../../../shared/api";
import { mapPurchaseItem } from "./map-purchase-item";
import {
  purchaseChangesSubscription,
  purchaseCorrectionsPaginationQuery,
  purchaseListFragment,
  purchasesQuery,
} from "./operations";

type Options = {
  from?: string;
  to?: string;
  origin?: PurchaseOrigin;
  hasEvidence?: boolean;
  search?: string;
  categoryId?: string;
  uncategorized?: boolean;
  tagId?: string;
  paymentAccountId?: string;
  poolId?: string;
  minimumAmount?: number;
  maximumAmount?: number;
  amountTarget?: PurchaseAmountTarget | null;
  loadAll?: boolean;
};

export function useHouseholdPurchases(householdId: string, first = 100, options: Options = {}) {
  const variables = useMemo(
    () => ({
      filter: {
        householdId,
        from: options.from,
        to: options.to,
        origin: options.origin,
        hasEvidence: options.hasEvidence,
        search: options.search,
        categoryId: options.categoryId,
        uncategorized: options.uncategorized,
        tagId: options.tagId,
        paymentAccountId: options.paymentAccountId,
        poolId: options.poolId,
        minimumAmount: options.minimumAmount,
        maximumAmount: options.maximumAmount,
        amountTarget: options.amountTarget,
      },
      first,
    }),
    [
      householdId,
      first,
      options.from,
      options.to,
      options.origin,
      options.hasEvidence,
      options.search,
      options.categoryId,
      options.uncategorized,
      options.tagId,
      options.paymentAccountId,
      options.poolId,
      options.minimumAmount,
      options.maximumAmount,
      options.amountTarget,
    ],
  );
  const query = useLazyLoadQuery<PurchasesQuery>(purchasesQuery, variables, {
    fetchPolicy: "store-or-network",
  });
  const requestKey = JSON.stringify(variables);
  const activeRequestKey = useRef(requestKey);
  const { data, hasNext, isLoadingNext, loadNext, refetch } = usePaginationFragment<
    PurchasesQuery,
    PurchaseListFragment$key
  >(purchaseListFragment, query);
  const [moreErrorState, setMoreErrorState] = useState<{
    requestKey: string;
    message: string;
  } | null>(null);
  const moreError = moreErrorState?.requestKey === requestKey ? moreErrorState.message : "";
  useEffect(() => {
    activeRequestKey.current = requestKey;
    setMoreErrorState(null);
  }, [requestKey]);
  const [completeCorrections, setCompleteCorrections] = useState<
    Record<string, NonNullable<Purchase["corrections"]>>
  >({});
  const purchases: Purchase[] = data.purchases.edges.map(({ node }) => {
    const corrections =
      completeCorrections[`${node.id}:${node.revision}`] ??
      node.corrections.edges.map(({ node: correction }) => correction);
    return {
      ...node,
      origin: node.origin as PurchaseOrigin,
      status: node.status,
      corrections,
      items: node.items.map(mapPurchaseItem),
    } as Purchase;
  });
  useEffect(() => {
    let cancelled = false;
    const loadCorrections = async () => {
      const purchasesMissingCorrections = data.purchases.edges.filter(
        ({ node }) =>
          node.corrections.pageInfo.hasNextPage &&
          completeCorrections[`${node.id}:${node.revision}`] === undefined,
      );
      if (purchasesMissingCorrections.length === 0) return;
      setMoreErrorState(null);
      const loadedByPurchase: Record<string, NonNullable<Purchase["corrections"]>> = {};
      try {
        await Promise.all(
          purchasesMissingCorrections.map(async ({ node }) => {
            const corrections = node.corrections.edges.map(({ node: correction }) => correction);
            let after = node.corrections.pageInfo.endCursor;
            let hasNextPage = node.corrections.pageInfo.hasNextPage;
            while (hasNextPage && after) {
              const result = await fetchQuery<PurchaseCorrectionsPaginationQuery>(
                getRelayEnvironment(),
                purchaseCorrectionsPaginationQuery,
                { purchaseId: node.id, first: 100, after },
                { fetchPolicy: "network-only" },
              ).toPromise();
              if (!result) throw new Error("購入の訂正履歴を取得できませんでした");
              const connection = result.purchaseCorrections;
              corrections.push(...connection.edges.map(({ node: correction }) => correction));
              after = connection.pageInfo.endCursor;
              hasNextPage = connection.pageInfo.hasNextPage;
            }
            loadedByPurchase[`${node.id}:${node.revision}`] = corrections as NonNullable<
              Purchase["corrections"]
            >;
          }),
        );
        if (!cancelled) setCompleteCorrections((current) => ({ ...current, ...loadedByPurchase }));
      } catch (error) {
        if (!cancelled)
          setMoreErrorState({
            requestKey,
            message:
              error instanceof Error ? error.message : "購入の訂正履歴を取得できませんでした",
          });
      }
    };
    void loadCorrections();
    return () => {
      cancelled = true;
    };
  }, [completeCorrections, data.purchases.edges, requestKey]);
  const refresh = useCallback(() => {
    setMoreErrorState(null);
    refetch(variables, { fetchPolicy: "network-only" });
  }, [refetch, variables]);
  const loadMore = useCallback(() => {
    if (!hasNext || isLoadingNext) return;
    setMoreErrorState(null);
    loadNext(first, {
      onComplete: (error) => {
        if (error && activeRequestKey.current === requestKey)
          setMoreErrorState({
            requestKey,
            message: error.message || "購入履歴を取得できませんでした",
          });
      },
    });
  }, [first, hasNext, isLoadingNext, loadNext, requestKey]);

  useEffect(() => {
    const subscription = requestSubscription<PurchaseChangesSubscription>(getRelayEnvironment(), {
      subscription: purchaseChangesSubscription,
      variables: { householdId, after: data.purchases.syncCursor },
      onNext: refresh,
    });
    return () => subscription.dispose();
  }, [data.purchases.syncCursor, householdId, refresh]);

  useEffect(() => {
    if (options.loadAll && hasNext && !isLoadingNext && !moreError) loadMore();
  }, [hasNext, isLoadingNext, loadMore, moreError, options.loadAll]);

  return {
    purchases,
    loadingMore: isLoadingNext,
    moreError,
    hasNextPage: hasNext,
    totalCount: data.purchases.totalCount,
    loadMore,
    refresh,
  };
}
