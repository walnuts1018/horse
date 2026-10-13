import { fetchQuery } from "react-relay";

import type { Purchase, PurchaseHistoryQuery } from "../../../shared/api";

import { getRelayEnvironment } from "../../../shared/api";
import { mapPurchase } from "./map-purchase";
import { purchaseHistoryQuery } from "./operations";

export async function readHouseholdPurchases(
  householdId: string,
  first = 100,
): Promise<Purchase[]> {
  const data = await fetchQuery<PurchaseHistoryQuery>(
    getRelayEnvironment(),
    purchaseHistoryQuery,
    { filter: { householdId }, first },
    { fetchPolicy: "network-only" },
  ).toPromise();
  return data?.purchases.edges.map(({ node }) => mapPurchase(node)) ?? [];
}
