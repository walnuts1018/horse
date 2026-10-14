import { useEffect, useMemo, useState } from "react";
import { fetchQuery } from "react-relay";

import { getRelayEnvironment, type PurchaseDefaultsQuery } from "../../../shared/api";
import {
  initialPurchaseDefaultsFromHistory,
  merchantsFromHistory,
  paymentAccountFromHistory,
  type PurchaseDefaultHistory,
} from "./purchase-defaults";
import { purchaseDefaultsQuery } from "./purchase-defaults-operations";

export function usePurchaseDefaultHistory(householdId: string, accountIds: string[]) {
  const accountIdsKey = accountIds.join("\u0000");
  const stableAccountIds = useMemo(
    () => accountIdsKey.split("\u0000").filter(Boolean),
    [accountIdsKey],
  );
  const [history, setHistory] = useState<PurchaseDefaultHistory[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let active = true;
    void fetchQuery<PurchaseDefaultsQuery>(getRelayEnvironment(), purchaseDefaultsQuery, {
      householdId,
    })
      .toPromise()
      .then((result) => {
        if (!active) return;
        setHistory(
          (result?.purchaseDefaults ?? []).map((entry) => ({
            merchant: entry.merchant,
            paymentAccountId: entry.paymentAccountId,
            purchaseCount: entry.purchaseCount,
            lastUsedAt: entry.lastUsedAt,
          })),
        );
      })
      .catch(() => {})
      .finally(() => {
        if (active) setLoaded(true);
      });
    return () => {
      active = false;
    };
  }, [householdId]);

  return {
    loaded,
    initialDefaults: initialPurchaseDefaultsFromHistory(history, stableAccountIds),
    paymentAccountForMerchant: (merchant: string) =>
      paymentAccountFromHistory(history, merchant, stableAccountIds),
    recentMerchants: merchantsFromHistory(history),
  };
}
