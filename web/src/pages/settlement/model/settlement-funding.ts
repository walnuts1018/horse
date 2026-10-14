import { useEffect, useState } from "react";

import { makeIdempotencyKey, type Household, type Settlement } from "../../../shared/api";
import { errorMessage, japanMonth } from "../../../shared/lib";
import { recordSettlementFunding } from "../api/settlement-mutations";
import {
  clearFundingIntent,
  getFundingAccountDefaults,
  getFundingIntent,
  saveFundingAccountDefaults,
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
  const [initialAccountDefaults] = useState(() => getValidFundingAccountDefaults(household));
  const [pendingFunding, setPendingFunding] = useState<Record<string, FundingIntent>>({});
  const [fundingAmounts, setFundingAmounts] = useState<Partial<Record<string, string>>>({});
  const [fundingSourceAccounts, setFundingSourceAccounts] = useState<Record<string, string>>(
    initialAccountDefaults.sourceAccountIdsByMember,
  );
  const [fundingDestinationAccountId, setFundingDestinationAccountId] = useState(
    initialAccountDefaults.destinationAccountId,
  );
  const [savedFundingSourceAccounts, setSavedFundingSourceAccounts] = useState<
    Record<string, string>
  >(initialAccountDefaults.sourceAccountIdsByMember);
  const [savedFundingDestinationAccountId, setSavedFundingDestinationAccountId] = useState(
    initialAccountDefaults.destinationAccountId,
  );
  const hasUnsavedFunding =
    household.members.some(
      (member) =>
        (fundingSourceAccounts[member.id] ?? "") !== (savedFundingSourceAccounts[member.id] ?? ""),
    ) ||
    fundingDestinationAccountId !== savedFundingDestinationAccountId ||
    (settlement?.lines.some(
      (line) =>
        fundingAmounts[line.memberId] != null &&
        fundingAmounts[line.memberId] !== String(line.amountDueYen),
    ) ??
      false);

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

  useEffect(() => {
    const defaults = getValidFundingAccountDefaults(household);
    setFundingSourceAccounts(defaults.sourceAccountIdsByMember);
    setSavedFundingSourceAccounts(defaults.sourceAccountIdsByMember);
    setFundingDestinationAccountId(defaults.destinationAccountId);
    setSavedFundingDestinationAccountId(defaults.destinationAccountId);
    setFundingAmounts({});
  }, [household]);

  const record = async (memberId: string) => {
    const line = settlement?.lines.find((row) => row.memberId === memberId);
    let existingIntent: FundingIntent | null;
    try {
      existingIntent = getFundingIntent(household.id, memberId);
    } catch (reason) {
      setError(errorMessage(reason));
      return;
    }
    if (!existingIntent && (!line || line.amountDueYen <= 0)) return;
    const amountText = fundingAmounts[memberId] ?? String(line?.amountDueYen ?? 0);
    const amountYen = existingIntent?.input.amountYen ?? Number(amountText);
    const sourceAccountId = fundingSourceAccounts[memberId] ?? "";
    const destinationAccountId = fundingDestinationAccountId;
    if (
      !existingIntent &&
      (!Number.isSafeInteger(amountYen) || amountYen <= 0 || amountYen > 1_000_000_000_000)
    ) {
      setError("今回の入金額には1円以上の整数を入力してください");
      return;
    }
    setPending(true);
    setError("");
    setSuccess("");
    try {
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
      setFundingAmounts((current) => {
        const next = { ...current };
        delete next[memberId];
        return next;
      });
      setSavedFundingSourceAccounts((current) => ({
        ...current,
        [memberId]: intent.input.sourceAccountId ?? "",
      }));
      setSavedFundingDestinationAccountId(intent.input.destinationAccountId ?? "");
      saveFundingAccountDefaults(household.id, {
        sourceAccountIdsByMember: {
          ...savedFundingSourceAccounts,
          [memberId]: intent.input.sourceAccountId ?? "",
        },
        destinationAccountId: intent.input.destinationAccountId ?? "",
      });
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
    setFundingAmounts((current) => {
      const next = { ...current };
      delete next[memberId];
      return next;
    });
    setFundingSourceAccounts((current) => ({
      ...current,
      [memberId]: savedFundingSourceAccounts[memberId] ?? "",
    }));
    setFundingDestinationAccountId(savedFundingDestinationAccountId);
  };

  return {
    fundingDestinationAccountId,
    fundingAmounts,
    fundingSourceAccounts,
    hasUnsavedFunding,
    pendingFunding,
    record,
    restore,
    savedFundingDestinationAccountId,
    savedFundingSourceAccounts,
    setFundingDestinationAccountId,
    setFundingAmounts,
    setFundingSourceAccounts,
  };
}

function getValidFundingAccountDefaults(household: Household) {
  const stored = getFundingAccountDefaults(household.id);
  const sharedPoolIds = new Set(
    household.pools
      .filter(
        (pool) => pool.__typename === "SharedPool" || pool.__typename === "UnconfiguredSharedPool",
      )
      .map((pool) => pool.id),
  );
  const sourceAccountIdsByMember: Record<string, string> = {};
  for (const member of household.members) {
    const accountId = stored?.sourceAccountIdsByMember[member.id];
    if (!accountId) continue;
    const account = household.accounts.find((candidate) => candidate.id === accountId);
    const pool = household.pools.find((candidate) => candidate.id === account?.poolId);
    if (pool?.__typename === "PersonalPool" && pool.ownerMemberId === member.id) {
      sourceAccountIdsByMember[member.id] = accountId;
    }
  }
  const destination = household.accounts.find(
    (account) =>
      account.id === stored?.destinationAccountId &&
      account.poolId != null &&
      sharedPoolIds.has(account.poolId) &&
      account.kind !== "CARD",
  );
  return {
    sourceAccountIdsByMember,
    destinationAccountId: destination?.id ?? "",
  };
}
