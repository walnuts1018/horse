import { useEffect, useState } from "react";

import { errorMessage } from "../../../shared/lib";
import { readPurchaseEvidence, type PurchaseEvidence } from "../api/purchase-evidence";

// 取得結果をpurchaseId付きで保持し、purchaseIdと一致しない結果は導出値として破棄する
type PurchaseEvidenceResult = {
  purchaseId: string | null;
  evidence: PurchaseEvidence[];
  error: string;
};

export function usePurchaseEvidence(purchaseId: string | null) {
  const [result, setResult] = useState<PurchaseEvidenceResult>(() => ({
    purchaseId: null,
    evidence: [],
    error: "",
  }));

  useEffect(() => {
    if (!purchaseId) return;
    const controller = new AbortController();
    void readPurchaseEvidence(purchaseId, controller.signal)
      .then((evidence) => setResult({ purchaseId, evidence, error: "" }))
      .catch((reason) => {
        if (!controller.signal.aborted)
          setResult({ purchaseId, evidence: [], error: errorMessage(reason) });
      });
    return () => controller.abort();
  }, [purchaseId]);

  const evidence = result.purchaseId === purchaseId ? result.evidence : [];
  const error = result.purchaseId === purchaseId ? result.error : "";
  return { evidence, error };
}
