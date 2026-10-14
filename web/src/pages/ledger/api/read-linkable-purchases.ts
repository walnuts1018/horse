import { fetchQuery } from "react-relay";

import type { Purchase, PurchaseHistoryQuery } from "../../../shared/api";

import { mapPurchase, purchaseHistoryQuery } from "../../../entities/purchase";
import { getRelayEnvironment } from "../../../shared/api";

const pageSize = 100;

export async function readLinkablePurchases(
  householdId: string,
  search: string,
  after?: string | null,
) {
  const response = await fetchQuery<PurchaseHistoryQuery>(
    getRelayEnvironment(),
    purchaseHistoryQuery,
    {
      filter: { householdId, search: search.trim() || undefined },
      first: pageSize,
      after,
    },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!response) throw new Error("購入候補を読み込めませんでした");

  return {
    purchases: response.purchases.edges.map(({ node }): Purchase => mapPurchase(node)),
    after: response.purchases.pageInfo.endCursor,
    hasNextPage: response.purchases.pageInfo.hasNextPage,
  };
}
