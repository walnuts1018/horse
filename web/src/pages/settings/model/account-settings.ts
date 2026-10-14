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
  setAccountCardLast4,
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
  const [accountCardLast4, setAccountCardLast4Draft] = useState("");
  const [cardLast4Drafts, setCardLast4Drafts] = useState<Record<string, string | undefined>>({});
  const [openingBalanceYen, setOpeningBalanceYen] = useState("");
  const [openingBalanceAt, setOpeningBalanceAt] = useState(() => {
    const date = new Date(Date.now() - new Date().getTimezoneOffset() * 60_000);
    return date.toISOString().slice(0, 10);
  });
  const [fundingDrafts, setFundingDrafts] = useState<FundingDrafts>(() =>
    reconcileFundingDrafts(household, household.id, {}),
  );
  const [fundingRuleErrors, setFundingRuleErrors] = useState<Record<string, string>>({});
  const fundingDraftHouseholdId = useRef(household.id);
  const cardLast4DraftHouseholdId = useRef(household.id);
  const [pending, setPending] = useState(false);

  useEffect(() => {
    onNoticeRef.current = onNotice;
    onFormCleanRef.current = onFormClean;
  }, [onFormClean, onNotice]);

  useEffect(() => {
    const currentHouseholdId = fundingDraftHouseholdId.current;
    fundingDraftHouseholdId.current = household.id;
    // 同期setStateをマイクロタスクで遅延
    queueMicrotask(() => {
      setFundingDrafts((current) => reconcileFundingDrafts(household, currentHouseholdId, current));
      const currentAccountHouseholdId = cardLast4DraftHouseholdId.current;
      cardLast4DraftHouseholdId.current = household.id;
      setCardLast4Drafts((current) =>
        Object.fromEntries(
          household.accounts.map((account) => [
            account.id,
            currentAccountHouseholdId === household.id && account.id in current
              ? current[account.id]
              : (account.cardLast4 ?? ""),
          ]),
        ),
      );
    });
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
      ...(accountKind === "CARD" && accountCardLast4 ? { cardLast4: accountCardLast4 } : {}),
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
        setAccountCardLast4Draft("");
        setOpeningBalanceYen("");
        await onRefresh();
        await onRefreshBalances();
        onNoticeRef.current("口座を追加しました", "success");
      })
      .catch((reason) => onNoticeRef.current(errorMessage(reason), "error"))
      .finally(() => setPending(false));
  };

  const saveCardLast4 = async (event: SubmitEvent<HTMLFormElement>, accountId: string) => {
    event.preventDefault();
    const cardLast4 = cardLast4Drafts[accountId]?.trim() ?? "";
    if (cardLast4 !== "" && !/^\d{4}$/.test(cardLast4)) {
      onNoticeRef.current("末尾4桁を半角数字4桁で入力してください", "warning");
      return;
    }
    setPending(true);
    try {
      await setAccountCardLast4({ accountId, cardLast4: cardLast4 || null });
      onFormCleanRef.current(`card-last4:${accountId}`);
      await onRefresh();
      onNoticeRef.current("カード末尾4桁を保存しました", "success");
    } catch (reason) {
      onNoticeRef.current(errorMessage(reason), "error");
    } finally {
      setPending(false);
    }
  };

  const saveFundingRule = async (event: SubmitEvent<HTMLFormElement>, poolId: string) => {
    event.preventDefault();
    const pool = household.pools.find((item) => item.id === poolId);
    if (!pool) return;
    const draft = fundingDrafts[poolId] ?? initialFundingDraft(pool, household);
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
      setFundingRuleErrors((current) => ({
        ...current,
        [poolId]: "拠出比率を1人以上入力してください。",
      }));
      return;
    }
    setFundingRuleErrors((current) => {
      const next = { ...current };
      delete next[poolId];
      return next;
    });
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

  const updateFundingDraft = (poolId: string, update: Partial<FundingDraft>) => {
    const pool = household.pools.find((item) => item.id === poolId);
    if (!pool) return;
    setFundingDrafts((current) => ({
      ...current,
      [poolId]: {
        ...(current[poolId] ?? initialFundingDraft(pool, household)),
        ...update,
      },
    }));
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
    accountCardLast4,
    setAccountCardLast4Draft,
    cardLast4Drafts,
    setCardLast4Drafts,
    openingBalanceYen,
    setOpeningBalanceYen,
    openingBalanceAt,
    setOpeningBalanceAt,
    fundingDrafts,
    updateFundingDraft,
    fundingRuleErrors,
    setFundingShareWeight: (poolId: string, memberId: string, weight: string) => {
      const pool = household.pools.find((item) => item.id === poolId);
      if (!pool) return;
      setFundingRuleErrors((current) => {
        const next = { ...current };
        delete next[poolId];
        return next;
      });
      setFundingDrafts((current) => ({
        ...current,
        [poolId]: {
          ...(current[poolId] ?? initialFundingDraft(pool, household)),
          weights: {
            ...(current[poolId] ?? initialFundingDraft(pool, household)).weights,
            [memberId]: weight,
          },
        },
      }));
    },
    pending,
    addPool,
    addAccount,
    saveCardLast4,
    saveFundingRule,
  };
}
