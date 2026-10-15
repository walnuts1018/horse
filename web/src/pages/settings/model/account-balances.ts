import { useCallback, useEffect, useRef, useState } from "react";

import type { AccountBalance, Household } from "../../../shared/api";

import { errorMessage } from "../../../shared/lib";
import { readAccountBalances } from "../api/read-account-balances";

export function useSettingsAccountBalances({
  household,
  onNotice,
  enabled = true,
}: {
  household: Household;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  enabled?: boolean;
}) {
  const [balances, setBalances] = useState<AccountBalance[]>([]);
  const [historyRevision, setHistoryRevision] = useState(0);

  // onNoticeは毎レンダーで再生成されるためref経由で参照し、取得effectの依存を安定させる
  const onNoticeRef = useRef(onNotice);
  useEffect(() => {
    onNoticeRef.current = onNotice;
  }, [onNotice]);

  const refresh = useCallback(async () => {
    setBalances(await readAccountBalances(household.id));
  }, [household.id]);

  useEffect(() => {
    if (!enabled) return;
    // 取得をマイクロタスクで遅延し、同期setStateを排除
    queueMicrotask(() => {
      void refresh().catch((reason) => onNoticeRef.current(errorMessage(reason), "error"));
    });
  }, [enabled, refresh]);

  const refreshAfterActivity = async () => {
    await refresh();
    setHistoryRevision((revision) => revision + 1);
  };

  return { balances, historyRevision, refresh, refreshAfterActivity };
}
