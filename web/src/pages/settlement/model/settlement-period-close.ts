import { useState } from "react";

import type { Settlement } from "../../../shared/api";
import type { SettlementSnapshot } from "./settlement-period";

import { errorMessage } from "../../../shared/lib";
import { closeSettlementPeriod } from "../api/settlement-mutations";

export function useSettlementPeriodClose({
  settlement,
  snapshot,
  period,
  setPending,
  setError,
  setSuccess,
  reload,
}: {
  settlement: Settlement | null;
  snapshot: SettlementSnapshot | null;
  period: string;
  setPending: (pending: boolean) => void;
  setError: (error: string) => void;
  setSuccess: (success: string) => void;
  reload: () => Promise<void>;
}) {
  const [confirmationOpen, setConfirmationOpen] = useState(false);

  const close = async () => {
    if (!settlement || !snapshot || snapshot.period !== period) return;
    setPending(true);
    setSuccess("");
    try {
      await closeSettlementPeriod({
        householdId: snapshot.householdId,
        period: snapshot.period,
        expectedRevision: settlement.revision,
      });
      setConfirmationOpen(false);
      await reload();
    } catch (reason) {
      setError(errorMessage(reason));
    } finally {
      setPending(false);
    }
  };

  return { close, confirmationOpen, setConfirmationOpen };
}
