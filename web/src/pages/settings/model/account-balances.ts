import { useEffect, useState } from "react";

import type { AccountBalance, Household } from "../../../shared/api";

import { readAccountBalances } from "../api/read-account-balances";

export function useSettingsAccountBalances({
  household,
  isOwner,
  onNotice,
}: {
  household: Household;
  isOwner: boolean;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
}) {
  const [balances, setBalances] = useState<AccountBalance[]>([]);
  const [historyRevision, setHistoryRevision] = useState(0);

  const refresh = async () => {
    setBalances(await readAccountBalances(household.id));
  };

  useEffect(() => {
    void refresh().catch((reason) => onNotice((reason as Error).message, "error"));
  }, [household.id, isOwner]);

  const refreshAfterActivity = async () => {
    await refresh();
    setHistoryRevision((revision) => revision + 1);
  };

  return { balances, historyRevision, refresh, refreshAfterActivity };
}
