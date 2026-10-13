import { useEffect, useState } from "react";

import { readPurchaseEvidence, type PurchaseEvidence } from "../api/purchase-evidence";

export function usePurchaseEvidence(purchaseId: string | null) {
  const [evidence, setEvidence] = useState<PurchaseEvidence[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!purchaseId) {
      setEvidence([]);
      setError("");
      return;
    }

    const controller = new AbortController();
    setEvidence([]);
    setError("");
    void readPurchaseEvidence(purchaseId, controller.signal)
      .then(setEvidence)
      .catch((reason) => {
        if (!controller.signal.aborted) setError((reason as Error).message);
      });
    return () => controller.abort();
  }, [purchaseId]);

  return { evidence, error };
}
