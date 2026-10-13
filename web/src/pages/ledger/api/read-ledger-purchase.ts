import { fetchQuery } from "react-relay";

import type { Purchase, PurchaseHistoryQuery } from "../../../shared/api";

import { mapPurchase, purchaseHistoryQuery } from "../../../entities/purchase";
import { getRelayEnvironment } from "../../../shared/api";

export async function readLedgerPurchase(
  householdId: string,
  purchaseId: string,
): Promise<Purchase | undefined> {
  let after: string | null = null;
  for (;;) {
    const response: PurchaseHistoryQuery["response"] | undefined =
      await fetchQuery<PurchaseHistoryQuery>(
        getRelayEnvironment(),
        purchaseHistoryQuery,
        { filter: { householdId }, first: 100, after },
        { fetchPolicy: "network-only" },
      ).toPromise();
    if (!response) return undefined;
    const match = response.purchases.edges.find(({ node }) => node.id === purchaseId)?.node;
    if (match) return mapPurchase(match);
    const page: PurchaseHistoryQuery["response"]["purchases"]["pageInfo"] =
      response.purchases.pageInfo;
    if (!page.hasNextPage || !page.endCursor || page.endCursor === after) return undefined;
    after = page.endCursor;
  }
}
