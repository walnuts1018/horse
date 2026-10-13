import { useCallback, useState } from "react";

export function useSettlementLoanState({
  setSuccess,
  reload,
}: {
  setSuccess: (success: string) => void;
  reload: () => Promise<void>;
}) {
  const [hasUnsavedRepayment, setHasUnsavedRepayment] = useState(false);
  const [hasPendingRepayment, setHasPendingRepayment] = useState(false);
  const [refreshSignal, setRefreshSignal] = useState(0);

  const onTransferRecorded = useCallback(() => {
    setRefreshSignal((current) => current + 1);
  }, []);

  const onRepaymentRecorded = useCallback(() => {
    setSuccess("返済を記録しました");
    void reload();
  }, [reload, setSuccess]);

  return {
    hasPendingRepayment,
    hasUnsavedRepayment,
    onRepaymentRecorded,
    onTransferRecorded,
    refreshSignal,
    setHasPendingRepayment,
    setHasUnsavedRepayment,
  };
}
