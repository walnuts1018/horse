import { useCallback, useEffect, useMemo, useState } from "react";
import {
  fetchQuery,
  requestSubscription,
  useLazyLoadQuery,
  usePaginationFragment,
} from "react-relay";
import type { Purchase, PurchaseItem, PurchaseOrigin } from "../../../shared/api";
import type {
  PurchaseChangesSubscription,
  PurchaseCorrectionsPaginationQuery,
  PurchaseListFragment$key,
  PurchasesQuery,
} from "..";
import { getRelayEnvironment } from "../../../shared/api";
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
  loadAll?: boolean;
};

export function useHouseholdPurchases(householdId: string, first = 100, options: Options = {}) {
  const variables = useMemo(
    () => ({
      householdId,
      first,
      from: options.from,
      to: options.to,
      origin: options.origin,
      hasEvidence: options.hasEvidence,
    }),
    [householdId, first, options.from, options.to, options.origin, options.hasEvidence],
  );
  const query = useLazyLoadQuery<PurchasesQuery>(purchasesQuery, variables, {
    fetchPolicy: "store-or-network",
  });
  const { data, hasNext, isLoadingNext, loadNext, refetch } = usePaginationFragment<
    PurchasesQuery,
    PurchaseListFragment$key
  >(purchaseListFragment, query);
  const [moreError, setMoreError] = useState("");
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
      items: node.items.map((item) => ({
        ...item,
        quantityNumerator: item.quantity?.numerator ?? null,
        quantityDenominator: item.quantity?.denominator ?? null,
        quantityUnit: item.quantity?.unit ?? null,
        printedUnitPriceYen: item.printedUnitPrice?.amountYen ?? null,
        printedUnitPriceQuantityNumerator: item.printedUnitPrice?.basis.numerator ?? null,
        printedUnitPriceQuantityDenominator: item.printedUnitPrice?.basis.denominator ?? null,
        printedUnitPriceUnit: item.printedUnitPrice?.basis.unit ?? null,
        effectiveUnitPriceNumeratorYen: item.effectiveUnitPrice?.numeratorYen ?? null,
        effectiveUnitPriceDenominator: item.effectiveUnitPrice?.denominator ?? null,
      })) as unknown as PurchaseItem[],
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
      setMoreError("");
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
          setMoreError(
            error instanceof Error ? error.message : "購入の訂正履歴を取得できませんでした",
          );
      }
    };
    void loadCorrections();
    return () => {
      cancelled = true;
    };
  }, [completeCorrections, data.purchases.edges]);
  const refresh = useCallback(() => {
    setMoreError("");
    refetch(variables, { fetchPolicy: "network-only" });
  }, [refetch, variables]);
  const loadMore = useCallback(() => {
    if (!hasNext || isLoadingNext) return;
    setMoreError("");
    loadNext(first, {
      onComplete: (error) => {
        if (error) setMoreError(error?.message || "購入履歴を取得できませんでした");
      },
    });
  }, [first, hasNext, isLoadingNext, loadNext]);

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
