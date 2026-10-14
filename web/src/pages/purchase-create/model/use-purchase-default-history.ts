import { useEffect, useMemo, useState } from "react";
import { fetchQuery } from "react-relay";

import { getRelayEnvironment, type PurchaseDefaultsQuery } from "../../../shared/api";
import {
  initialPurchaseDefaultsFromHistory,
  merchantsFromHistory,
  paymentAccountFromHistory,
  purchaseDefaultHistoryForHousehold,
  type PurchaseDefaultHistoryState,
} from "./purchase-defaults";
import { purchaseDefaultsQuery } from "./purchase-defaults-operations";

export function usePurchaseDefaultHistory(householdId: string, accountIds: string[]) {
  const accountIdsKey = accountIds.join("\u0000");
  const stableAccountIds = useMemo(
    () => accountIdsKey.split("\u0000").filter(Boolean),
    [accountIdsKey],
  );
  const [state, setState] = useState<PurchaseDefaultHistoryState>(() => ({
    householdId,
    history: [],
    loaded: false,
  }));

  useEffect(() => {
    let active = true;
    void fetchQuery<PurchaseDefaultsQuery>(getRelayEnvironment(), purchaseDefaultsQuery, {
      householdId,
    })
      .toPromise()
      .then((result) => {
        if (!active) return;
        setState({
          householdId,
          history: (result?.purchaseDefaults ?? []).map((entry) => ({
            merchant: entry.merchant,
            paymentAccountId: entry.paymentAccountId,
            purchaseCount: entry.purchaseCount,
            lastUsedAt: entry.lastUsedAt,
          })),
          loaded: true,
        });
      })
      .catch(() => {
        if (active) setState({ householdId, history: [], loaded: true });
      });
    return () => {
      active = false;
    };
  }, [householdId]);

  const currentState = purchaseDefaultHistoryForHousehold(state, householdId);

  return {
    loaded: currentState.loaded,
    initialDefaults: initialPurchaseDefaultsFromHistory(currentState.history, stableAccountIds),
    paymentAccountForMerchant: (merchant: string) =>
      paymentAccountFromHistory(currentState.history, merchant, stableAccountIds),
    recentMerchants: merchantsFromHistory(currentState.history),
  };
}
