import { useEffect, useRef, useState, type FormEvent } from "react";

import {
  isPersonalPool,
  isSharedPool,
  type Household,
  type SharedPoolSettlementPolicy,
} from "../../../shared/api";
import {
  createAccount,
  createPool,
  setPoolFundingRule,
  type AccountKind,
  type PoolKind,
} from "../api/account-settings";

export function useAccountSettings({
  household,
  onRefresh,
  onRefreshBalances,
  onNotice,
  onFormClean,
}: {
  household: Household;
  onRefresh: () => Promise<void>;
  onRefreshBalances: () => Promise<void>;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onFormClean: (formID: string) => void;
}) {
  const onNoticeRef = useRef(onNotice);
  const onFormCleanRef = useRef(onFormClean);
  const [poolName, setPoolName] = useState("");
  const [poolKind, setPoolKind] = useState<PoolKind>("PERSONAL");
  const [accountName, setAccountName] = useState("");
  const [accountPool, setAccountPool] = useState("");
  const [accountKind, setAccountKind] = useState<AccountKind>("BANK");
  const [openingBalanceYen, setOpeningBalanceYen] = useState("");
  const [openingBalanceAt, setOpeningBalanceAt] = useState(() => {
    const date = new Date(Date.now() - new Date().getTimezoneOffset() * 60_000);
    return date.toISOString().slice(0, 10);
  });
  const [fundingDrafts, setFundingDrafts] = useState<
    Record<
      string,
      {
        ownerMemberId: string;
        policy: SharedPoolSettlementPolicy;
        weights: Record<string, string>;
      }
    >
  >({});
  const [pending, setPending] = useState(false);

  useEffect(() => {
    onNoticeRef.current = onNotice;
    onFormCleanRef.current = onFormClean;
  }, [onFormClean, onNotice]);

  useEffect(() => {
    setFundingDrafts(
      Object.fromEntries(
        household.pools.map((pool) => [
          pool.id,
          {
            ownerMemberId: pool.__typename === "PersonalPool" ? pool.ownerMemberId : "",
            policy:
              pool.__typename === "SharedPool" ? pool.settlementPolicy : "BALANCE_TO_STANDARD",
            weights: Object.fromEntries(
              household.members.map((member) => [
                member.id,
                String(
                  pool.fundingShares.find((share) => share.memberId === member.id)?.weight ?? "",
                ),
              ]),
            ),
          },
        ]),
      ),
    );
  }, [household.id, household.pools, household.members]);

  const addPool = (event: FormEvent) => {
    event.preventDefault();
    setPending(true);
    void createPool({ householdId: household.id, name: poolName.trim(), kind: poolKind })
      .then(async () => {
        onFormCleanRef.current("create-pool");
        setPoolName("");
        await onRefresh();
        onNoticeRef.current("設定を保存しました", "success");
      })
      .catch((reason) => onNoticeRef.current((reason as Error).message, "error"))
      .finally(() => setPending(false));
  };

  const addAccount = (event: FormEvent) => {
    event.preventDefault();
    if (
      openingBalanceYen !== "" &&
      (!Number.isSafeInteger(Number(openingBalanceYen)) || Number(openingBalanceYen) < 1)
    ) {
      return;
    }
    setPending(true);
    void createAccount({
      householdId: household.id,
      name: accountName.trim(),
      kind: accountKind,
      poolId: accountPool || null,
      ...(openingBalanceYen !== ""
        ? {
            openingBalance: {
              amountYen: Number(openingBalanceYen),
              at: openingBalanceAt,
            },
          }
        : {}),
    })
      .then(async () => {
        onFormCleanRef.current("create-account");
        setAccountName("");
        setOpeningBalanceYen("");
        await onRefresh();
        await onRefreshBalances();
        onNoticeRef.current("口座を追加しました", "success");
      })
      .catch((reason) => onNoticeRef.current((reason as Error).message, "error"))
      .finally(() => setPending(false));
  };

  const saveFundingRule = async (event: FormEvent, poolId: string) => {
    event.preventDefault();
    const pool = household.pools.find((item) => item.id === poolId);
    const draft = fundingDrafts[poolId];
    if (!pool || !draft) return;
    const fundingShares = household.members
      .map((member) => ({
        memberId: member.id,
        weight: Math.max(0, Number(draft.weights[member.id]) || 0),
      }))
      .filter((share) => share.weight > 0);
    if (isPersonalPool(pool) && !draft.ownerMemberId) {
      onNoticeRef.current("所有者を選択してください", "warning");
      return;
    }
    if (isSharedPool(pool) && fundingShares.length === 0) {
      onNoticeRef.current("拠出比率を入力してください", "warning");
      return;
    }
    setPending(true);
    try {
      await setPoolFundingRule({
        householdId: household.id,
        poolId,
        configuration: isPersonalPool(pool)
          ? { personal: { ownerMemberId: draft.ownerMemberId } }
          : { shared: { fundingShares, settlementPolicy: draft.policy } },
        expectedVersion: pool.fundingRuleVersion,
      });
      onFormCleanRef.current(`pool-funding:${poolId}`);
      await onRefresh();
      onNoticeRef.current("精算設定を保存しました", "success");
    } catch (reason) {
      onNoticeRef.current((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };

  return {
    poolName,
    setPoolName,
    poolKind,
    setPoolKind,
    accountName,
    setAccountName,
    accountPool,
    setAccountPool,
    accountKind,
    setAccountKind,
    openingBalanceYen,
    setOpeningBalanceYen,
    openingBalanceAt,
    setOpeningBalanceAt,
    fundingDrafts,
    setFundingDrafts,
    pending,
    addPool,
    addAccount,
    saveFundingRule,
  };
}
