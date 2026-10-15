import { useCallback, useEffect, useRef, useState } from "react";

import { errorMessage, japanMonth } from "../../../shared/lib";
import { readSettlement, type SettlementWithEvidence } from "../api/read-settlement";

export type SettlementSnapshot = {
  householdId: string;
  period: string;
  value: SettlementWithEvidence;
};

export function useSettlementPeriod(householdId: string, refreshLedger: () => void) {
  const [period, setPeriod] = useState(() => japanMonth(new Date()));
  const [snapshot, setSnapshot] = useState<SettlementSnapshot | null>(null);
  const [loadError, setLoadError] = useState("");
  const loadRequest = useRef(0);
  const settlement =
    snapshot?.householdId === householdId && snapshot.period === period ? snapshot.value : null;

  const load = useCallback(async () => {
    const request = ++loadRequest.current;
    const requestedPeriod = period;
    const requestedHouseholdId = householdId;
    setLoadError("");
    try {
      refreshLedger();
      const settlementData = await readSettlement(requestedHouseholdId, requestedPeriod);
      if (request !== loadRequest.current) return;
      setSnapshot({
        householdId: requestedHouseholdId,
        period: requestedPeriod,
        value: settlementData,
      });
    } catch (reason) {
      if (request === loadRequest.current) setLoadError(errorMessage(reason));
    }
  }, [householdId, period, refreshLedger]);

  useEffect(() => {
    //読取の開始時に行う同期setStateはeffect直後ではなくマイクロタスクで走らせる
    queueMicrotask(() => void load());
  }, [load]);

  return { load, loadError, period, setPeriod, settlement, snapshot };
}
