import { useEffect, useState } from "react";
import { purchasesQuery, readGraphql, type Connection, type Purchase } from "./household";

export function useHouseholdPurchases(householdId: string, first = 100) {
  const [purchases, setPurchases] = useState<Purchase[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const refresh = async () => {
    const data = await readGraphql<{ purchases: Connection<Purchase> }>(purchasesQuery, {
      householdId,
      first,
    });
    setPurchases(data.purchases.edges.map((edge) => edge.node));
  };
  useEffect(() => {
    let active = true;
    void readGraphql<{ purchases: Connection<Purchase> }>(purchasesQuery, { householdId, first })
      .then((data) => {
        if (active) setPurchases(data.purchases.edges.map((edge) => edge.node));
      })
      .catch((reason) => active && setError((reason as Error).message))
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
    };
  }, [householdId, first]);
  useEffect(() => {
    const events = new EventSource(`/events?householdId=${encodeURIComponent(householdId)}`);
    events.onmessage = () => {
      void refresh().catch((reason) => setError((reason as Error).message));
    };
    return () => events.close();
  }, [householdId, first]);
  return { purchases, loading, error, refresh };
}
