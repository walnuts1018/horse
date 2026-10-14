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
import type { Purchase, PurchaseCorrection, PurchaseOrigin } from "../../../shared/api";

import { getRelayEnvironment } from "../../../shared/api";
import { isOneOf } from "../../../shared/lib";
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

type RelayPurchaseCorrection =
  PurchaseCorrectionsPaginationQuery["response"]["purchaseCorrections"]["edges"][number]["node"];

type PurchaseCorrectionPageState = {
  corrections: PurchaseCorrection[];
  after: string | null;
  hasNextPage: boolean;
  loading: boolean;
  error: string;
};

/** Relayが返す訂正履歴を画面で使うPurchaseCorrectionへ変換する。 */
function toPurchaseCorrection(correction: RelayPurchaseCorrection) {
  const fields = {
    id: correction.id,
    amountDeltaYen: correction.amountDeltaYen,
    reason: correction.reason,
    occurredAt: correction.occurredAt,
    settlementMonth: correction.settlementMonth,
    paymentAllocations: correction.paymentAllocations.map((allocation) => ({
      paymentAccountId: allocation.paymentAccountId,
      amountYen: allocation.amountYen,
      account: allocation.account,
    })),
  };
  if (correction.__typename === "PurchaseItemCorrection" && correction.itemId !== undefined) {
    return { ...fields, __typename: "PurchaseItemCorrection", itemId: correction.itemId } as const;
  }
  return { ...fields, __typename: "PurchaseWideCorrection" } as const;
}

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
  }, [requestKey]);
  const [correctionPages, setCorrectionPages] = useState<
    Record<string, PurchaseCorrectionPageState>
  >({});
  const loadingCorrectionPages = useRef(new Set<string>());
  const purchases: Purchase[] = data.purchases.edges.map(({ node }) => {
    return {
      ...node,
      origin: isOneOf(node.origin, ["AI", "MANUAL", "UNKNOWN"]) ? node.origin : "UNKNOWN",
      sourceKinds: [...node.sourceKinds],
      paymentAllocations: [...node.paymentAllocations],
      status: node.status,
      corrections: correctionPages[node.id]?.corrections ?? [],
      items: node.items.map(mapPurchaseItem),
    };
  });
  const loadCorrections = useCallback(async (purchaseId: string, after?: string) => {
    const requestId = `${purchaseId}:${after ?? "first"}`;
    if (loadingCorrectionPages.current.has(requestId)) return;
    loadingCorrectionPages.current.add(requestId);
    setCorrectionPages((current) => ({
      ...current,
      [purchaseId]: { ...current[purchaseId], loading: true, error: "" },
    }));
    try {
      const result = await fetchQuery<PurchaseCorrectionsPaginationQuery>(
        getRelayEnvironment(),
        purchaseCorrectionsPaginationQuery,
        { purchaseId, first: 50, after: after ?? null },
        { fetchPolicy: "network-only" },
      ).toPromise();
      if (!result) throw new Error("購入の訂正履歴を取得できませんでした");
      const connection = result.purchaseCorrections;
      const corrections = connection.edges.map(({ node }) => toPurchaseCorrection(node));
      setCorrectionPages((current) => {
        const previous = current[purchaseId];
        return {
          ...current,
          [purchaseId]: {
            corrections: after ? [...(previous?.corrections ?? []), ...corrections] : corrections,
            after: connection.pageInfo.endCursor ?? null,
            hasNextPage: connection.pageInfo.hasNextPage,
            loading: false,
            error: "",
          },
        };
      });
    } catch (error) {
      setCorrectionPages((current) => ({
        ...current,
        [purchaseId]: {
          corrections: current[purchaseId]?.corrections ?? [],
          after: current[purchaseId]?.after ?? null,
          hasNextPage: current[purchaseId]?.hasNextPage ?? false,
          loading: false,
          error: error instanceof Error ? error.message : "購入の訂正履歴を取得できませんでした",
        },
      }));
    } finally {
      loadingCorrectionPages.current.delete(requestId);
    }
  }, []);
  const refresh = useCallback(() => {
    setMoreErrorState(null);
    setCorrectionPages({});
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
    if (options.loadAll && hasNext && !isLoadingNext && !moreError) {
      // Relayのページングは同期的にsetStateするため、effect直後ではなくマイクロタスクで起動する
      queueMicrotask(loadMore);
    }
  }, [hasNext, isLoadingNext, loadMore, moreError, options.loadAll]);

  return {
    purchases,
    loadingMore: isLoadingNext,
    moreError,
    hasNextPage: hasNext,
    totalCount: data.purchases.totalCount,
    correctionPages,
    loadCorrections,
    loadMore,
    refresh,
  };
}
