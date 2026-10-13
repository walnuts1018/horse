import { useCallback, useEffect, useMemo, useState } from "react";
import { requestSubscription, useLazyLoadQuery, usePaginationFragment } from "react-relay";
import type { Purchase, PurchaseItem, PurchaseOrigin } from "../../../shared/api";
import type { PurchaseChangesSubscription, PurchaseListFragment$key, PurchasesQuery } from "..";
import { getRelayEnvironment } from "../../../shared/api";
import { purchaseChangesSubscription, purchaseListFragment, purchasesQuery } from "./operations";

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
  const purchases: Purchase[] = data.purchases.edges.map(
    ({ node }) =>
      ({
        ...node,
        origin: node.origin as PurchaseOrigin,
        status: node.status,
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
      }) as Purchase,
  );
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
