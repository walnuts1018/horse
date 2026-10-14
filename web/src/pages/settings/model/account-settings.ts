import { useEffect, useRef, useState, type SubmitEvent } from "react";

import {
  isPersonalPool,
  isSharedPool,
  type Household,
  type Pool,
  type SharedPoolSettlementPolicy,
} from "../../../shared/api";
import { errorMessage } from "../../../shared/lib";
import {
  createAccount,
  createPool,
  setPoolFundingRule,
  type AccountKind,
  type PoolKind,
} from "../api/account-settings";

type FundingDraft = {
  ownerMemberId: string;
  policy: SharedPoolSettlementPolicy;
  weights: Record<string, string>;
};

type FundingDrafts = Record<string, FundingDraft>;

function initialFundingDraft(pool: Pool, household: Household): FundingDraft {
  return {
    ownerMemberId: pool.__typename === "PersonalPool" ? pool.ownerMemberId : "",
    policy: pool.__typename === "SharedPool" ? pool.settlementPolicy : "BALANCE_TO_STANDARD",
    weights: Object.fromEntries(
      household.members.map((member) => [
        member.id,
        String(pool.fundingShares.find((share) => share.memberId === member.id)?.weight ?? ""),
      ]),
    ),
  };
}

export function reconcileFundingDrafts(
  household: Household,
  currentHouseholdId: string,
  current: FundingDrafts,
): FundingDrafts {
  return Object.fromEntries(
    household.pools.map((pool) => {
      const initial = initialFundingDraft(pool, household);
      const existing = currentHouseholdId === household.id ? current[pool.id] : undefined;
      if (!existing) return [pool.id, initial];

      return [
        pool.id,
        {
          ...existing,
          weights: Object.fromEntries(
            household.members.map((member) => [
              member.id,
              member.id in existing.weights
                ? existing.weights[member.id]
                : initial.weights[member.id],
            ]),
          ),
        },
      ];
    }),
  );
}

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
  const [fundingDrafts, setFundingDrafts] = useState<FundingDrafts>({});
  const fundingDraftHouseholdId = useRef(household.id);
  const [pending, setPending] = useState(false);

  useEffect(() => {
    onNoticeRef.current = onNotice;
    onFormCleanRef.current = onFormClean;
  }, [onFormClean, onNotice]);

  useEffect(() => {
    const currentHouseholdId = fundingDraftHouseholdId.current;
    fundingDraftHouseholdId.current = household.id;
    setFundingDrafts((current) => reconcileFundingDrafts(household, currentHouseholdId, current));
  }, [household]);

  const addPool = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    setPending(true);
    void createPool({ householdId: household.id, name: poolName.trim(), kind: poolKind })
      .then(async () => {
        onFormCleanRef.current("create-pool");
        setPoolName("");
        await onRefresh();
        onNoticeRef.current("設定を保存しました", "success");
      })
      .catch((reason) => onNoticeRef.current(errorMessage(reason), "error"))
      .finally(() => setPending(false));
  };

  const addAccount = (event: SubmitEvent<HTMLFormElement>) => {
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
      .catch((reason) => onNoticeRef.current(errorMessage(reason), "error"))
      .finally(() => setPending(false));
  };

  const saveFundingRule = async (event: SubmitEvent<HTMLFormElement>, poolId: string) => {
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
      onNoticeRef.current(errorMessage(reason), "error");
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
