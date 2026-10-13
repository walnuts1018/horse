import { useCallback, useEffect, useRef, useState } from "react";
import type { Settlement } from "../../../shared/api";
import { japanMonth } from "../../../shared/lib";
import { readSettlement } from "../api/read-settlement";

export type SettlementSnapshot = {
  householdId: string;
  period: string;
  value: Settlement;
};

export function useSettlementPeriod(householdId: string, refreshLedger: () => void) {
  const [period, setPeriod] = useState(() => japanMonth(new Date()));
  const [snapshot, setSnapshot] = useState<SettlementSnapshot | null>(null);
  const [loadError, setLoadError] = useState("");
  const activePeriod = useRef(period);
  activePeriod.current = period;
  const activeHouseholdId = useRef(householdId);
  activeHouseholdId.current = householdId;
  const loadRequest = useRef(0);
  const settlement =
    snapshot?.householdId === householdId && snapshot.period === period ? snapshot.value : null;

  const load = useCallback(async () => {
    const request = ++loadRequest.current;
    const requestedPeriod = activePeriod.current;
    const requestedHouseholdId = activeHouseholdId.current;
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
      if (request === loadRequest.current) setLoadError((reason as Error).message);
    }
  }, [refreshLedger]);

  useEffect(() => {
    void load();
  }, [householdId, load, period]);

  return { load, loadError, period, setPeriod, settlement, snapshot };
}
