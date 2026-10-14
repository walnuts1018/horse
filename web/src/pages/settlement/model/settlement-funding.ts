import { useEffect, useState } from "react";

import { makeIdempotencyKey, type Household, type Settlement } from "../../../shared/api";
import { errorMessage, japanMonth } from "../../../shared/lib";
import { recordSettlementFunding } from "../api/settlement-mutations";
import {
  clearFundingIntent,
  getFundingIntent,
  saveFundingIntent,
  type FundingIntent,
} from "./pending-ledger-mutations";
import { formatSettlementYen } from "./settlement-view";

export function useSettlementFunding({
  household,
  settlement,
  setPending,
  setError,
  setSuccess,
  onRecorded,
}: {
  household: Household;
  settlement: Settlement | null;
  setPending: (pending: boolean) => void;
  setError: (error: string) => void;
  setSuccess: (success: string) => void;
  onRecorded: () => Promise<void>;
}) {
  const [pendingFunding, setPendingFunding] = useState<Record<string, FundingIntent>>({});
  const [fundingSourceAccounts, setFundingSourceAccounts] = useState<Record<string, string>>({});
  const [fundingDestinationAccountId, setFundingDestinationAccountId] = useState("");
  const [savedFundingSourceAccounts, setSavedFundingSourceAccounts] = useState<
    Record<string, string>
  >({});
  const [savedFundingDestinationAccountId, setSavedFundingDestinationAccountId] = useState("");
  const hasUnsavedFunding =
    household.members.some(
      (member) =>
        (fundingSourceAccounts[member.id] ?? "") !== (savedFundingSourceAccounts[member.id] ?? ""),
    ) || fundingDestinationAccountId !== savedFundingDestinationAccountId;

  useEffect(() => {
    //保存済み入金意図の復元と同期setStateはeffect直後ではなくマイクロタスクで走らせる
    queueMicrotask(() => {
      try {
        const funding: Record<string, FundingIntent> = {};
        for (const member of household.members) {
          const intent = getFundingIntent(household.id, member.id);
          if (intent) funding[member.id] = intent;
        }
        setPendingFunding(funding);
      } catch (reason) {
        setError(errorMessage(reason));
      }
    });
  }, [household.id, household.members, setError]);

  const record = async (memberId: string) => {
    const line = settlement?.lines.find((row) => row.memberId === memberId);
    const amountYen = line?.amountDueYen ?? 0;
    const sourceAccountId = fundingSourceAccounts[memberId] ?? "";
    const destinationAccountId = fundingDestinationAccountId;
    setPending(true);
    setError("");
    setSuccess("");
    try {
      const existingIntent = getFundingIntent(household.id, memberId);
      if (amountYen <= 0 && !existingIntent) return;
      const idempotencyKey = makeIdempotencyKey();
      const intent = existingIntent ?? {
        idempotencyKey,
        input: {
          householdId: household.id,
          memberId,
          idempotencyKey,
          poolId:
            household.pools.find(
              (pool) =>
                pool.__typename === "SharedPool" || pool.__typename === "UnconfiguredSharedPool",
            )?.id ?? household.pools[0]?.id,
          amountYen,
          occurredAt: new Date().toISOString(),
          ...(sourceAccountId ? { sourceAccountId } : {}),
          ...(destinationAccountId ? { destinationAccountId } : {}),
        },
      };
      if (!intent.input.poolId) throw new Error("入金先のプールがありません");
      saveFundingIntent(household.id, memberId, intent);
      setPendingFunding((current) => ({ ...current, [memberId]: intent }));
      await recordSettlementFunding(intent.input);
      clearFundingIntent(household.id, memberId);
      setPendingFunding((current) => {
        const next = { ...current };
        delete next[memberId];
        return next;
      });
      setSavedFundingSourceAccounts((current) => ({
        ...current,
        [memberId]: intent.input.sourceAccountId ?? "",
      }));
      setSavedFundingDestinationAccountId(intent.input.destinationAccountId ?? "");
      const memberName = household.members.find((member) => member.id === memberId)?.displayName;
      setSuccess(
        `${memberName ?? "メンバー"}の入金${formatSettlementYen(intent.input.amountYen)}を${japanMonth(new Date(intent.input.occurredAt))}分として記録しました`,
      );
      await onRecorded();
    } catch (reason) {
      setError(errorMessage(reason));
    } finally {
      setPending(false);
    }
  };

  const restore = (memberId: string) => {
    setFundingSourceAccounts((current) => ({
      ...current,
      [memberId]: savedFundingSourceAccounts[memberId] ?? "",
    }));
    setFundingDestinationAccountId(savedFundingDestinationAccountId);
  };

  return {
    fundingDestinationAccountId,
    fundingSourceAccounts,
    hasUnsavedFunding,
    pendingFunding,
    record,
    restore,
    savedFundingDestinationAccountId,
    savedFundingSourceAccounts,
    setFundingDestinationAccountId,
    setFundingSourceAccounts,
  };
}
