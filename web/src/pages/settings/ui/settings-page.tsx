import { useEffect, useState, type ChangeEvent, type FormEvent } from "react";
import { Activity, ArrowLeftRight, Check, ChevronRight, Plus, Wallet } from "lucide-react";
import {
  makeIdempotencyKey,
  MutationUserError,
  readGraphql,
  isPersonalPool,
  isSharedPool,
  type Connection,
  type SharedPoolSettlementPolicy,
  type AllocationRule,
  type ProductConcept,
  type Household,
  type AccountMovement,
  type AccountBalance,
  type AccountIncome,
} from "../../../shared/api";
import {
  allocationRulesQuery,
  createAccountMutation,
  createCategoryMutation,
  createPoolMutation,
  setAllocationRuleMutation,
  setCategoryArchivedMutation,
  setDefaultAllocationMutation,
  setPoolFundingRuleMutation,
} from "../api/operations";
import {
  createProductConceptMutation,
  createProductVariantMutation,
  readProductCatalog,
  setProductAliasMutation,
} from "../../../entities/product";
import { ManageHouseholdInvitations } from "./manage-household-invitations";
import { ManageBudgets } from "./manage-budgets";
import { ManageRecurringExpenses } from "./manage-recurring-expenses";
import { ManageTags } from "./manage-tags";
import { ViewAccountBalanceHistory } from "./view-account-balance-history";
import {
  accountMovementsQuery,
  accountBalancesQuery,
  accountIncomesQuery,
  recordAccountMovementMutation,
  recordExternalIncomeMutation,
} from "../../../entities/ledger";
import { InlineError } from "../../../shared/ui";
import {
  clearAccountIncomeIntent,
  clearAccountMovementIntent,
  getAccountIncomeIntent,
  getAccountMovementIntent,
  saveAccountIncomeIntent,
  saveAccountMovementIntent,
  type AccountIncomeIntent,
  type AccountMovementIntent,
} from "../model/pending-account-mutations";

const localDateTime = () => {
  const date = new Date(Date.now() - new Date().getTimezoneOffset() * 60_000);
  return date.toISOString().slice(0, 16);
};
const localDateTimeFromIso = (value: string) => {
  const date = new Date(Date.parse(value) - new Date().getTimezoneOffset() * 60_000);
  return date.toISOString().slice(0, 16);
};
const yen = (amount: number) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
type SettingsTab = "accounts" | "allocation" | "catalog" | "planning" | "members";

const settingsTabs: { id: SettingsTab; label: string }[] = [
  { id: "accounts", label: "口座" },
  { id: "allocation", label: "負担" },
  { id: "catalog", label: "分類" },
  { id: "planning", label: "予算" },
  { id: "members", label: "メンバー" },
];

const settingsTabByHash: Record<string, SettingsTab> = {
  "pool-settings": "accounts",
  "payment-source-settings": "accounts",
  "default-allocation-settings": "allocation",
};

export function SettingsPage({
  household,
  onRefresh,
  onNotice,
  onUnsavedChanges,
  isOwner,
}: {
  household: Household;
  onRefresh: () => Promise<void>;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onUnsavedChanges: (unsaved: boolean) => void;
  isOwner: boolean;
}) {
  const [activeSettingsTab, setActiveSettingsTab] = useState<SettingsTab>("accounts");
  const [setupHashTarget, setSetupHashTarget] = useState<string | null>(null);
  const [poolName, setPoolName] = useState("");
  const [poolKind, setPoolKind] = useState("PERSONAL");
  const [accountName, setAccountName] = useState("");
  const [accountPool, setAccountPool] = useState("");
  const [accountKind, setAccountKind] = useState("BANK");
  const [openingBalanceYen, setOpeningBalanceYen] = useState("");
  const [openingBalanceAt, setOpeningBalanceAt] = useState(localDateTime().slice(0, 10));
  const [categoryName, setCategoryName] = useState("");
  const [allocationRules, setAllocationRules] = useState<AllocationRule[]>([]);
  const [allocationRulesCount, setAllocationRulesCount] = useState(0);
  const [allocationRulesCursor, setAllocationRulesCursor] = useState<string | null>(null);
  const [allocationRulesHasNextPage, setAllocationRulesHasNextPage] = useState(false);
  const [allocationRulesPageLoading, setAllocationRulesPageLoading] = useState(false);
  const [ruleScope, setRuleScope] = useState<
    "CATEGORY" | "MERCHANT" | "PRODUCT" | "PRODUCT_VARIANT"
  >("CATEGORY");
  const [ruleCategoryId, setRuleCategoryId] = useState("");
  const [ruleMerchant, setRuleMerchant] = useState("");
  const [ruleWeights, setRuleWeights] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      household.pools.map((pool) => [pool.id, String(pool.defaultShareWeight || 1)]),
    ),
  );
  const [shareWeights, setShareWeights] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      household.pools.map((pool) => [pool.id, String(pool.defaultShareWeight || 1)]),
    ),
  );
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
  const [tagPending, setTagPending] = useState(false);
  const [invitationPending, setInvitationPending] = useState(false);
  const [dirtyForms, setDirtyForms] = useState<Set<string>>(() => new Set());
  const [catalog, setCatalog] = useState<ProductConcept[]>([]);
  const [conceptName, setConceptName] = useState("");
  const [conceptCategoryId, setConceptCategoryId] = useState("");
  const [variantConceptId, setVariantConceptId] = useState("");
  const [variantName, setVariantName] = useState("");
  const [variantBrand, setVariantBrand] = useState("");
  const [variantPackageSize, setVariantPackageSize] = useState("");
  const [aliasVariantId, setAliasVariantId] = useState("");
  const [aliasMerchant, setAliasMerchant] = useState("");
  const [aliasName, setAliasName] = useState("");
  const [ruleProductId, setRuleProductId] = useState("");
  const [ruleVariantId, setRuleVariantId] = useState("");
  const [budgetPending, setBudgetPending] = useState(false);
  const [recurringPending, setRecurringPending] = useState(false);
  const [movements, setMovements] = useState<AccountMovement[]>([]);
  const [incomes, setIncomes] = useState<AccountIncome[]>([]);
  const [movementCursor, setMovementCursor] = useState<string | null>(null);
  const [incomeCursor, setIncomeCursor] = useState<string | null>(null);
  const [movementHasNextPage, setMovementHasNextPage] = useState(false);
  const [incomeHasNextPage, setIncomeHasNextPage] = useState(false);
  const [movementPageLoading, setMovementPageLoading] = useState(false);
  const [incomePageLoading, setIncomePageLoading] = useState(false);
  const [movementPageError, setMovementPageError] = useState("");
  const [incomePageError, setIncomePageError] = useState("");
  const [balances, setBalances] = useState<AccountBalance[]>([]);
  const [balanceHistoryRevision, setBalanceHistoryRevision] = useState(0);
  const [incomeAccountId, setIncomeAccountId] = useState("");
  const [incomeAmount, setIncomeAmount] = useState("");
  const [incomeDescription, setIncomeDescription] = useState("");
  const [incomeDate, setIncomeDate] = useState(localDateTime);
  const [movementKind, setMovementKind] = useState<"TRANSFER" | "CARD_BILL_PAYMENT">("TRANSFER");
  const [movementFrom, setMovementFrom] = useState("");
  const [movementTo, setMovementTo] = useState("");
  const [movementAmount, setMovementAmount] = useState("");
  const [movementDescription, setMovementDescription] = useState("");
  const [movementDate, setMovementDate] = useState(localDateTime);
  const [pendingIncomeIntent, setPendingIncomeIntent] = useState<AccountIncomeIntent | null>(null);
  const [pendingMovementIntent, setPendingMovementIntent] = useState<AccountMovementIntent | null>(
    null,
  );
  useEffect(() => {
    onUnsavedChanges(
      pending ||
        tagPending ||
        budgetPending ||
        recurringPending ||
        invitationPending ||
        dirtyForms.size > 0,
    );
  }, [
    budgetPending,
    dirtyForms,
    invitationPending,
    onUnsavedChanges,
    pending,
    recurringPending,
    tagPending,
  ]);
  useEffect(() => () => onUnsavedChanges(false), [onUnsavedChanges]);
  useEffect(() => {
    const selectSettingsTabFromHash = () => {
      const sectionId = window.location.hash.slice(1);
      const tab = settingsTabByHash[sectionId];
      if (!tab) {
        setSetupHashTarget(null);
        return;
      }
      setActiveSettingsTab(tab);
      setSetupHashTarget(sectionId);
    };
    selectSettingsTabFromHash();
    window.addEventListener("hashchange", selectSettingsTabFromHash);
    return () => window.removeEventListener("hashchange", selectSettingsTabFromHash);
  }, []);
  useEffect(() => {
    if (!setupHashTarget || settingsTabByHash[setupHashTarget] !== activeSettingsTab) return;
    document.getElementById(setupHashTarget)?.scrollIntoView({ block: "start" });
    setSetupHashTarget(null);
  }, [activeSettingsTab, setupHashTarget]);
  const markFormDirty = (formID: string) => {
    setDirtyForms((current) => {
      if (current.has(formID)) return current;
      const next = new Set(current);
      next.add(formID);
      return next;
    });
  };
  const markFormClean = (formID: string) => {
    setDirtyForms((current) => {
      if (!current.has(formID)) return current;
      const next = new Set(current);
      next.delete(formID);
      return next;
    });
  };
  const trackFormChange = (event: ChangeEvent<HTMLDivElement>) => {
    if (!(event.target instanceof Element)) return;
    const form = event.target.closest<HTMLFormElement>("form[data-unsaved-id]");
    if (form?.dataset.unsavedId) markFormDirty(form.dataset.unsavedId);
  };
  const refreshRules = async () => {
    const result = await readGraphql<{ allocationRules: Connection<AllocationRule> }>(
      allocationRulesQuery,
      {
        householdId: household.id,
        first: 50,
      },
    );
    setAllocationRules(result.allocationRules.edges.map((edge) => edge.node));
    setAllocationRulesCount(result.allocationRules.totalCount);
    setAllocationRulesCursor(result.allocationRules.pageInfo.endCursor ?? null);
    setAllocationRulesHasNextPage(result.allocationRules.pageInfo.hasNextPage);
  };
  const loadMoreRules = async () => {
    if (!allocationRulesCursor || allocationRulesPageLoading) return;
    setAllocationRulesPageLoading(true);
    try {
      const result = await readGraphql<{ allocationRules: Connection<AllocationRule> }>(
        allocationRulesQuery,
        {
          householdId: household.id,
          after: allocationRulesCursor,
          first: 50,
        },
      );
      setAllocationRules((current) => [
        ...current,
        ...result.allocationRules.edges.map((edge) => edge.node),
      ]);
      setAllocationRulesCursor(result.allocationRules.pageInfo.endCursor ?? null);
      setAllocationRulesHasNextPage(result.allocationRules.pageInfo.hasNextPage);
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setAllocationRulesPageLoading(false);
    }
  };
  const refreshProducts = async () => {
    setCatalog(await readProductCatalog(household.id));
  };
  const refreshPlanning = async () => {
    const [movementResult, incomeResult, balanceResult] = await Promise.all([
      readGraphql<{
        accountMovements: {
          edges: { node: AccountMovement }[];
          pageInfo: { endCursor: string | null; hasNextPage: boolean };
        };
      }>(accountMovementsQuery, {
        householdId: household.id,
        first: 50,
      }),
      readGraphql<{
        accountIncomes: {
          edges: { node: AccountIncome }[];
          pageInfo: { endCursor: string | null; hasNextPage: boolean };
        };
      }>(accountIncomesQuery, {
        householdId: household.id,
        first: 50,
      }),
      readGraphql<{ accountBalances: AccountBalance[] }>(accountBalancesQuery, {
        householdId: household.id,
      }),
    ]);
    setMovements(movementResult.accountMovements.edges.map((edge) => edge.node));
    setMovementCursor(movementResult.accountMovements.pageInfo.endCursor);
    setMovementHasNextPage(movementResult.accountMovements.pageInfo.hasNextPage);
    setIncomes(incomeResult.accountIncomes.edges.map((edge) => edge.node));
    setIncomeCursor(incomeResult.accountIncomes.pageInfo.endCursor);
    setIncomeHasNextPage(incomeResult.accountIncomes.pageInfo.hasNextPage);
    setBalances(balanceResult.accountBalances);
  };
  const loadMoreMovements = async () => {
    if (!movementCursor || movementPageLoading) return;
    setMovementPageLoading(true);
    setMovementPageError("");
    try {
      const result = await readGraphql<{
        accountMovements: {
          edges: { node: AccountMovement }[];
          pageInfo: { endCursor: string | null; hasNextPage: boolean };
        };
      }>(accountMovementsQuery, { householdId: household.id, first: 50, after: movementCursor });
      setMovements((current) => [
        ...current,
        ...result.accountMovements.edges.map((edge) => edge.node),
      ]);
      setMovementCursor(result.accountMovements.pageInfo.endCursor);
      setMovementHasNextPage(result.accountMovements.pageInfo.hasNextPage);
    } catch {
      setMovementPageError("口座移動履歴を読み込めませんでした。");
    } finally {
      setMovementPageLoading(false);
    }
  };
  const loadMoreIncomes = async () => {
    if (!incomeCursor || incomePageLoading) return;
    setIncomePageLoading(true);
    setIncomePageError("");
    try {
      const result = await readGraphql<{
        accountIncomes: {
          edges: { node: AccountIncome }[];
          pageInfo: { endCursor: string | null; hasNextPage: boolean };
        };
      }>(accountIncomesQuery, { householdId: household.id, first: 50, after: incomeCursor });
      setIncomes((current) => [
        ...current,
        ...result.accountIncomes.edges.map((edge) => edge.node),
      ]);
      setIncomeCursor(result.accountIncomes.pageInfo.endCursor);
      setIncomeHasNextPage(result.accountIncomes.pageInfo.hasNextPage);
    } catch {
      setIncomePageError("入金履歴を読み込めませんでした。");
    } finally {
      setIncomePageLoading(false);
    }
  };
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
  useEffect(() => {
    void Promise.all([refreshRules(), refreshProducts(), refreshPlanning()]).catch((reason) =>
      onNotice((reason as Error).message, "error"),
    );
  }, [household.id, isOwner]);
  useEffect(() => {
    const incomeIntent = getAccountIncomeIntent(household.id);
    if (incomeIntent) {
      setPendingIncomeIntent(incomeIntent);
      setIncomeAccountId(incomeIntent.input.accountId);
      setIncomeAmount(String(incomeIntent.input.amountYen));
      setIncomeDate(localDateTimeFromIso(incomeIntent.input.occurredAt));
      setIncomeDescription(incomeIntent.input.description);
    } else {
      setPendingIncomeIntent(null);
    }
    const movementIntent = getAccountMovementIntent(household.id);
    if (movementIntent) {
      setPendingMovementIntent(movementIntent);
      setMovementKind(movementIntent.input.kind);
      setMovementFrom(movementIntent.input.fromAccountId);
      setMovementTo(movementIntent.input.toAccountId);
      setMovementAmount(String(movementIntent.input.amountYen));
      setMovementDate(localDateTimeFromIso(movementIntent.input.occurredAt));
      setMovementDescription(movementIntent.input.description);
    } else {
      setPendingMovementIntent(null);
    }
  }, [household.id]);
  useEffect(() => {
    const sourceAccounts = household.accounts.filter((account) =>
      movementKind === "TRANSFER" ? account.kind !== "CARD" : account.kind === "BANK",
    );
    const destinationAccounts = household.accounts.filter((account) =>
      movementKind === "TRANSFER" ? account.kind !== "CARD" : account.kind === "CARD",
    );
    if (!sourceAccounts.some((account) => account.id === movementFrom)) {
      setMovementFrom(sourceAccounts[0]?.id ?? "");
    }
    if (!destinationAccounts.some((account) => account.id === movementTo)) {
      setMovementTo(destinationAccounts[0]?.id ?? "");
    }
  }, [household.accounts, movementKind, movementFrom, movementTo]);
  const saveMutation = async (
    mutation: Parameters<typeof readGraphql>[0],
    input: Record<string, unknown>,
    formID: string,
  ): Promise<boolean> => {
    setPending(true);
    try {
      await readGraphql<Record<string, unknown>>(mutation, { input });
      await onRefresh();
      markFormClean(formID);
      onNotice("設定を保存しました", "success");
      return true;
    } catch (reason) {
      onNotice((reason as Error).message, "error");
      return false;
    } finally {
      setPending(false);
    }
  };
  const saveMovement = async (event: FormEvent) => {
    event.preventDefault();
    if (
      !pendingMovementIntent &&
      (!movementFrom || !movementTo || movementFrom === movementTo || !Number(movementAmount))
    )
      return;
    setPending(true);
    try {
      const intent =
        pendingMovementIntent ??
        (() => {
          const idempotencyKey = makeIdempotencyKey();
          return {
            idempotencyKey,
            input: {
              householdId: household.id,
              kind: movementKind,
              fromAccountId: movementFrom,
              toAccountId: movementTo,
              amountYen: Number(movementAmount),
              occurredAt: new Date(movementDate).toISOString(),
              description: movementDescription.trim(),
              idempotencyKey,
            },
          };
        })();
      saveAccountMovementIntent(household.id, intent);
      setPendingMovementIntent(intent);
      await readGraphql(recordAccountMovementMutation, { input: intent.input });
      clearAccountMovementIntent(household.id);
      setPendingMovementIntent(null);
      markFormClean("account-movement");
      setMovementAmount("");
      setMovementDescription("");
      setMovementDate(localDateTime());
      await refreshPlanning();
      setBalanceHistoryRevision((revision) => revision + 1);
      onNotice(
        movementKind === "TRANSFER" ? "口座移動を記録しました" : "カード請求の支払いを記録しました",
        "success",
      );
    } catch (reason) {
      if (reason instanceof MutationUserError) {
        clearAccountMovementIntent(household.id);
        setPendingMovementIntent(null);
      }
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };
  const saveIncome = async (event: FormEvent) => {
    event.preventDefault();
    if (!pendingIncomeIntent && (!incomeAccountId || !Number(incomeAmount))) return;
    setPending(true);
    try {
      const intent =
        pendingIncomeIntent ??
        (() => {
          const idempotencyKey = makeIdempotencyKey();
          return {
            idempotencyKey,
            input: {
              householdId: household.id,
              accountId: incomeAccountId,
              amountYen: Number(incomeAmount),
              occurredAt: new Date(incomeDate).toISOString(),
              description: incomeDescription.trim(),
              idempotencyKey,
            },
          };
        })();
      saveAccountIncomeIntent(household.id, intent);
      setPendingIncomeIntent(intent);
      await readGraphql(recordExternalIncomeMutation, { input: intent.input });
      clearAccountIncomeIntent(household.id);
      setPendingIncomeIntent(null);
      markFormClean("account-income");
      setIncomeAmount("");
      setIncomeDescription("");
      setIncomeDate(localDateTime());
      await refreshPlanning();
      setBalanceHistoryRevision((revision) => revision + 1);
      onNotice("入金を記録しました", "success");
    } catch (reason) {
      if (reason instanceof MutationUserError) {
        clearAccountIncomeIntent(household.id);
        setPendingIncomeIntent(null);
      }
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };
  const addPool = (event: FormEvent) => {
    event.preventDefault();
    void saveMutation(
      createPoolMutation,
      {
        householdId: household.id,
        name: poolName.trim(),
        kind: poolKind,
      },
      "create-pool",
    ).then((saved) => saved && setPoolName(""));
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
    void readGraphql(createAccountMutation, {
      input: {
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
      },
    })
      .then(async () => {
        markFormClean("create-account");
        setAccountName("");
        setOpeningBalanceYen("");
        await onRefresh();
        await refreshPlanning();
        onNotice("口座を追加しました", "success");
      })
      .catch((reason) => onNotice((reason as Error).message, "error"))
      .finally(() => setPending(false));
  };
  const addCategory = (event: FormEvent) => {
    event.preventDefault();
    void saveMutation(
      createCategoryMutation,
      {
        householdId: household.id,
        name: categoryName.trim(),
      },
      "create-category",
    ).then((saved) => saved && setCategoryName(""));
  };
  const saveAllocation = (event: FormEvent) => {
    event.preventDefault();
    void saveMutation(
      setDefaultAllocationMutation,
      {
        householdId: household.id,
        allocations: household.pools
          .map((pool) => ({
            poolId: pool.id,
            weight: Math.max(0, Number(shareWeights[pool.id]) || 0),
          }))
          .filter((item) => item.weight > 0),
      },
      "default-allocation",
    );
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
      onNotice("所有者を選択してください", "warning");
      return;
    }
    if (isSharedPool(pool) && fundingShares.length === 0) {
      onNotice("拠出比率を入力してください", "warning");
      return;
    }
    setPending(true);
    try {
      await readGraphql(setPoolFundingRuleMutation, {
        input: {
          householdId: household.id,
          poolId,
          configuration: isPersonalPool(pool)
            ? { personal: { ownerMemberId: draft.ownerMemberId } }
            : { shared: { fundingShares, settlementPolicy: draft.policy } },
          expectedVersion: pool.fundingRuleVersion,
        },
      });
      markFormClean(`pool-funding:${poolId}`);
      await onRefresh();
      onNotice("精算設定を保存しました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };
  const archiveCategory = async (categoryId: string, archived: boolean) => {
    setPending(true);
    try {
      await readGraphql(setCategoryArchivedMutation, { input: { categoryId, archived } });
      await onRefresh();
      onNotice(archived ? "カテゴリを非表示にしました" : "カテゴリを再表示しました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };
  const saveRule = async (event: FormEvent) => {
    event.preventDefault();
    const allocations = household.pools
      .map((pool) => ({ poolId: pool.id, weight: Math.max(0, Number(ruleWeights[pool.id]) || 0) }))
      .filter((allocation) => allocation.weight > 0);
    if (
      !allocations.length ||
      (ruleScope === "CATEGORY" && !ruleCategoryId) ||
      (ruleScope === "MERCHANT" && !ruleMerchant.trim()) ||
      (ruleScope === "PRODUCT" && !ruleProductId) ||
      (ruleScope === "PRODUCT_VARIANT" && !ruleVariantId)
    )
      return;
    setPending(true);
    try {
      const target =
        ruleScope === "CATEGORY"
          ? { categoryId: ruleCategoryId }
          : ruleScope === "MERCHANT"
            ? { merchant: ruleMerchant.trim() }
            : ruleScope === "PRODUCT"
              ? { productConceptId: ruleProductId }
              : { productVariantId: ruleVariantId };
      await readGraphql(setAllocationRuleMutation, {
        input: {
          householdId: household.id,
          target,
          allocations,
        },
      });
      markFormClean("allocation-rule");
      await refreshRules();
      onNotice("負担ルールを保存しました", "success");
      setRuleMerchant("");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };
  const createConcept = async (event: FormEvent) => {
    event.preventDefault();
    setPending(true);
    try {
      await readGraphql(createProductConceptMutation, {
        input: {
          householdId: household.id,
          name: conceptName.trim(),
          categoryId: conceptCategoryId || null,
        },
      });
      markFormClean("create-concept");
      setConceptName("");
      await refreshProducts();
      onNotice("商品を追加しました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };
  const createVariant = async (event: FormEvent) => {
    event.preventDefault();
    if (!variantConceptId) return;
    setPending(true);
    try {
      await readGraphql(createProductVariantMutation, {
        input: {
          householdId: household.id,
          conceptId: variantConceptId,
          name: variantName.trim(),
          brand: variantBrand.trim() || null,
          packageSize: variantPackageSize.trim() || null,
        },
      });
      markFormClean("create-variant");
      setVariantName("");
      setVariantBrand("");
      setVariantPackageSize("");
      await refreshProducts();
      onNotice("バリエーションを追加しました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };
  const createAlias = async (event: FormEvent) => {
    event.preventDefault();
    if (!aliasVariantId) return;
    setPending(true);
    try {
      await readGraphql(setProductAliasMutation, {
        input: {
          householdId: household.id,
          variantId: aliasVariantId,
          merchant: aliasMerchant.trim(),
          alias: aliasName.trim(),
        },
      });
      markFormClean("create-alias");
      setAliasName("");
      await refreshProducts();
      onNotice("店名の別表記を登録しました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };
  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">共有と分類</span>
          <h1>設定</h1>
          <p>{household.name}の口座、決済元、カテゴリ</p>
        </div>
      </div>
      <nav
        aria-label="設定項目"
        style={{ display: "flex", flexWrap: "wrap", gap: 8, marginBottom: 14 }}
      >
        {settingsTabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            className={`button ${activeSettingsTab === tab.id ? "button-primary" : "button-soft"}`}
            aria-pressed={activeSettingsTab === tab.id}
            onClick={() => {
              setActiveSettingsTab(tab.id);
              setSetupHashTarget(null);
            }}
          >
            {tab.label}
          </button>
        ))}
      </nav>
      <div className="settings-layout" onChangeCapture={trackFormChange}>
        <section
          className="panel settings-section"
          hidden={activeSettingsTab !== "accounts"}
          id="pool-settings"
          style={{ scrollMarginTop: 80 }}
        >
          <div className="panel-heading">
            <div>
              <span className="eyebrow">負担先</span>
              <h2>マネープール</h2>
            </div>
            <span className="count-badge">{household.pools.length}</span>
          </div>
          <div className="settings-rows">
            {household.pools.map((pool) => (
              <div className="pool-funding-card" key={pool.id}>
                <div className="setting-row">
                  <div className="pool-avatar">{pool.name.slice(0, 1)}</div>
                  <div>
                    <b>{pool.name}</b>
                    <small>
                      {isSharedPool(pool) ? "共用" : "個人"} · 品目負担の標準比{" "}
                      {pool.defaultShareWeight}
                    </small>
                  </div>
                  {pool.__typename === "UnconfiguredPersonalPool" ||
                  pool.__typename === "UnconfiguredSharedPool" ? (
                    <span className="period-chip">設定が必要</span>
                  ) : null}
                  <ChevronRight size={16} />
                </div>
                <form
                  className="pool-funding-form"
                  data-unsaved-id={`pool-funding:${pool.id}`}
                  onSubmit={(event) => void saveFundingRule(event, pool.id)}
                >
                  {isPersonalPool(pool) ? (
                    <label>
                      所有者
                      <select
                        required
                        value={fundingDrafts[pool.id]?.ownerMemberId ?? ""}
                        onChange={(event) =>
                          setFundingDrafts((current) => ({
                            ...current,
                            [pool.id]: { ...current[pool.id], ownerMemberId: event.target.value },
                          }))
                        }
                      >
                        <option value="">メンバーを選択</option>
                        {household.members.map((member) => (
                          <option key={member.id} value={member.id}>
                            {member.displayName}
                          </option>
                        ))}
                      </select>
                    </label>
                  ) : (
                    <>
                      <label>
                        精算方法
                        <select
                          value={fundingDrafts[pool.id]?.policy ?? "BALANCE_TO_STANDARD"}
                          onChange={(event) =>
                            setFundingDrafts((current) => ({
                              ...current,
                              [pool.id]: {
                                ...current[pool.id],
                                policy: event.target.value as SharedPoolSettlementPolicy,
                              },
                            }))
                          }
                        >
                          <option value="BALANCE_TO_STANDARD">負担割合の平準化</option>
                          <option value="REIMBURSE_PERSONAL">予算外支出の補填</option>
                        </select>
                      </label>
                      <div className="pool-funding-shares">
                        {household.members.map((member) => (
                          <label key={member.id}>
                            <span>{member.displayName}</span>
                            <div className="number-field">
                              <input
                                inputMode="numeric"
                                value={fundingDrafts[pool.id]?.weights[member.id] ?? ""}
                                onChange={(event) =>
                                  setFundingDrafts((current) => ({
                                    ...current,
                                    [pool.id]: {
                                      ...current[pool.id],
                                      weights: {
                                        ...current[pool.id].weights,
                                        [member.id]: event.target.value.replace(/\D/g, ""),
                                      },
                                    },
                                  }))
                                }
                              />
                              <small>比</small>
                            </div>
                          </label>
                        ))}
                      </div>
                    </>
                  )}
                  <button className="button button-soft" disabled={pending}>
                    <Check size={15} />
                    精算設定を保存
                  </button>
                </form>
              </div>
            ))}
          </div>
          <form className="inline-create" data-unsaved-id="create-pool" onSubmit={addPool}>
            <input
              value={poolName}
              onChange={(e) => setPoolName(e.target.value)}
              placeholder="プール名"
              required
            />
            <select value={poolKind} onChange={(e) => setPoolKind(e.target.value)}>
              <option value="PERSONAL">個人</option>
              <option value="SHARED">共用</option>
            </select>
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              追加
            </button>
          </form>
        </section>
        <ViewAccountBalanceHistory
          householdId={household.id}
          accounts={household.accounts}
          hidden={activeSettingsTab !== "accounts"}
          refreshRevision={balanceHistoryRevision}
        />
        <section
          className="panel settings-section"
          hidden={activeSettingsTab !== "accounts"}
          id="payment-source-settings"
          style={{ scrollMarginTop: 80 }}
        >
          <div className="panel-heading">
            <div>
              <span className="eyebrow">決済に使う口座・カード</span>
              <h2>決済元</h2>
            </div>
            <span className="count-badge">{household.accounts.length}</span>
          </div>
          <div className="settings-rows">
            {household.accounts.map((account) => (
              <div className="setting-row" key={account.id}>
                <div className="account-avatar">
                  <Wallet size={16} />
                </div>
                <div>
                  <b>{account.name}</b>
                  <small>
                    {household.pools.find((pool) => pool.id === account.poolId)?.name ?? "個人"} ·{" "}
                    {(
                      { BANK: "銀行", CASH: "現金", CARD: "カード", OTHER: "その他" } as Record<
                        string,
                        string
                      >
                    )[account.kind] ?? account.kind}{" "}
                    · 残高{" "}
                    {yen(
                      balances.find((balance) => balance.accountId === account.id)?.balanceYen ?? 0,
                    )}
                  </small>
                </div>
                <ChevronRight size={16} />
              </div>
            ))}
          </div>
          <form className="inline-create" data-unsaved-id="create-account" onSubmit={addAccount}>
            <input
              value={accountName}
              onChange={(e) => setAccountName(e.target.value)}
              placeholder="口座・カード名"
              required
            />
            <select value={accountKind} onChange={(event) => setAccountKind(event.target.value)}>
              <option value="BANK">銀行</option>
              <option value="CASH">現金</option>
              <option value="CARD">カード</option>
              <option value="OTHER">その他</option>
            </select>
            <select value={accountPool} onChange={(e) => setAccountPool(e.target.value)}>
              <option value="">紐づくプール</option>
              {household.pools.map((pool) => (
                <option key={pool.id} value={pool.id}>
                  {pool.name}
                </option>
              ))}
            </select>
            <input
              aria-label="初期残高"
              type="number"
              step="1"
              value={openingBalanceYen}
              onChange={(event) => setOpeningBalanceYen(event.target.value)}
              placeholder="初期残高"
            />
            {openingBalanceYen && (
              <label>
                基準日
                <input
                  aria-label="初期残高の基準日"
                  type="date"
                  value={openingBalanceAt}
                  onChange={(event) => setOpeningBalanceAt(event.target.value)}
                  required
                />
              </label>
            )}
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              追加
            </button>
          </form>
        </section>
        <section className="panel settings-section" hidden={activeSettingsTab !== "catalog"}>
          <div className="panel-heading">
            <div>
              <span className="eyebrow">品目の分類</span>
              <h2>カテゴリ</h2>
            </div>
            <span className="count-badge">
              {household.categories.filter((category) => !category.archived).length}
            </span>
          </div>
          <div className="category-management">
            {household.categories.map((category, index) => (
              <div className="category-management-row" key={category.id}>
                <span
                  className={`category-tag swatch-bg-${index % 6} ${category.archived ? "category-archived" : ""}`}
                >
                  {category.name}
                </span>
                <button
                  className="text-button"
                  disabled={pending}
                  onClick={() => void archiveCategory(category.id, !category.archived)}
                >
                  {category.archived ? "再表示" : "非表示"}
                </button>
              </div>
            ))}
          </div>
          <form className="inline-create" data-unsaved-id="create-category" onSubmit={addCategory}>
            <input
              value={categoryName}
              onChange={(e) => setCategoryName(e.target.value)}
              placeholder="カテゴリ名"
              required
            />
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              追加
            </button>
          </form>
        </section>
        <section
          className="panel settings-section"
          hidden={activeSettingsTab !== "allocation"}
          id="default-allocation-settings"
          style={{ scrollMarginTop: 80 }}
        >
          <div className="panel-heading">
            <div>
              <span className="eyebrow">買い物の初期値</span>
              <h2>標準の負担割合</h2>
            </div>
            <span className="count-badge">
              <Activity size={14} />
            </span>
          </div>
          <form data-unsaved-id="default-allocation" onSubmit={saveAllocation}>
            <div className="allocation-settings">
              {household.pools.map((pool) => (
                <label key={pool.id}>
                  <span>{pool.name}</span>
                  <div className="number-field">
                    <input
                      inputMode="numeric"
                      value={shareWeights[pool.id] ?? ""}
                      onChange={(e) =>
                        setShareWeights((current) => ({
                          ...current,
                          [pool.id]: e.target.value.replace(/\D/g, ""),
                        }))
                      }
                    />
                    <small>比</small>
                  </div>
                </label>
              ))}
            </div>
            <div className="settings-save">
              <span>変更後の買い物から適用</span>
              <button className="button button-primary" disabled={pending}>
                <Check size={16} />
                保存
              </button>
            </div>
          </form>
        </section>
        <section className="panel settings-section" hidden={activeSettingsTab !== "allocation"}>
          <div className="panel-heading">
            <div>
              <span className="eyebrow">自動負担</span>
              <h2>優先ルール</h2>
            </div>
            <span className="count-badge">{allocationRulesCount}</span>
          </div>
          <div className="settings-rows">
            {allocationRules.map((rule) => (
              <div className="setting-row" key={rule.id}>
                <div className="pool-avatar">
                  {rule.target.__typename === "Category"
                    ? "分"
                    : rule.target.__typename === "MerchantAllocationRuleTarget"
                      ? "店"
                      : rule.target.__typename === "ProductConcept"
                        ? "品"
                        : "規"}
                </div>
                <div>
                  <b>{rule.target.name}</b>
                  <small>
                    {rule.allocations
                      .map(
                        (allocation) =>
                          `${household.pools.find((pool) => pool.id === allocation.poolId)?.name ?? "プール"} ${allocation.weight}`,
                      )
                      .join(" / ")}
                  </small>
                </div>
              </div>
            ))}
          </div>
          {allocationRulesHasNextPage && (
            <button
              className="text-button"
              type="button"
              disabled={allocationRulesPageLoading}
              onClick={() => void loadMoreRules()}
            >
              {allocationRulesPageLoading ? "読み込み中" : "さらに表示"}
            </button>
          )}
          <form
            className="allocation-rule-form"
            data-unsaved-id="allocation-rule"
            onSubmit={saveRule}
          >
            <select
              value={ruleScope}
              onChange={(event) => setRuleScope(event.target.value as "CATEGORY" | "MERCHANT")}
            >
              <option value="CATEGORY">カテゴリ</option>
              <option value="MERCHANT">店名</option>
              <option value="PRODUCT">商品</option>
              <option value="PRODUCT_VARIANT">バリエーション</option>
            </select>
            {ruleScope === "CATEGORY" ? (
              <select
                value={ruleCategoryId}
                onChange={(event) => setRuleCategoryId(event.target.value)}
                required
              >
                <option value="">カテゴリを選択</option>
                {household.categories
                  .filter((category) => !category.archived)
                  .map((category) => (
                    <option key={category.id} value={category.id}>
                      {category.name}
                    </option>
                  ))}
              </select>
            ) : ruleScope === "MERCHANT" ? (
              <input
                value={ruleMerchant}
                onChange={(event) => setRuleMerchant(event.target.value)}
                placeholder="店名"
                required
              />
            ) : ruleScope === "PRODUCT" ? (
              <select
                value={ruleProductId}
                onChange={(event) => setRuleProductId(event.target.value)}
                required
              >
                <option value="">商品を選択</option>
                {catalog
                  .filter((product) => !product.archived)
                  .map((product) => (
                    <option key={product.id} value={product.id}>
                      {product.name}
                    </option>
                  ))}
              </select>
            ) : (
              <select
                value={ruleVariantId}
                onChange={(event) => setRuleVariantId(event.target.value)}
                required
              >
                <option value="">バリエーションを選択</option>
                {catalog.flatMap((product) =>
                  product.variants
                    .filter((variant) => !variant.archived)
                    .map((variant) => (
                      <option key={variant.id} value={variant.id}>
                        {product.name} · {variant.name}
                      </option>
                    )),
                )}
              </select>
            )}
            <div className="allocation-settings">
              {household.pools.map((pool) => (
                <label key={pool.id}>
                  <span>{pool.name}</span>
                  <div className="number-field">
                    <input
                      inputMode="numeric"
                      value={ruleWeights[pool.id] ?? ""}
                      onChange={(event) =>
                        setRuleWeights((current) => ({
                          ...current,
                          [pool.id]: event.target.value.replace(/\D/g, ""),
                        }))
                      }
                    />
                    <small>比</small>
                  </div>
                </label>
              ))}
            </div>
            <button className="button button-primary" disabled={pending}>
              <Check size={16} />
              ルールを保存
            </button>
          </form>
        </section>
        <section className="panel settings-section" hidden={activeSettingsTab !== "catalog"}>
          <div className="panel-heading">
            <div>
              <span className="eyebrow">商品カタログ</span>
              <h2>商品とバリエーション</h2>
            </div>
            <span className="count-badge">{catalog.length}</span>
          </div>
          <div className="settings-rows">
            {catalog.map((product) => (
              <div className="setting-row" key={product.id}>
                <div className="pool-avatar">品</div>
                <div>
                  <b>{product.name}</b>
                  <small>
                    {product.variants
                      .map((variant) =>
                        [variant.name, variant.brand, variant.packageSize]
                          .filter(Boolean)
                          .join(" · "),
                      )
                      .join(" / ") || "バリエーションなし"}
                  </small>
                  {product.variants.some((variant) => variant.aliases.length > 0) && (
                    <small>
                      {product.variants
                        .flatMap((variant) =>
                          variant.aliases.map((alias) => `${alias.merchantKey}: ${alias.alias}`),
                        )
                        .join(" / ")}
                    </small>
                  )}
                </div>
              </div>
            ))}
          </div>
          <form className="inline-create" data-unsaved-id="create-concept" onSubmit={createConcept}>
            <input
              value={conceptName}
              onChange={(event) => setConceptName(event.target.value)}
              placeholder="商品名"
              required
            />
            <select
              value={conceptCategoryId}
              onChange={(event) => setConceptCategoryId(event.target.value)}
            >
              <option value="">カテゴリなし</option>
              {household.categories
                .filter((category) => !category.archived)
                .map((category) => (
                  <option key={category.id} value={category.id}>
                    {category.name}
                  </option>
                ))}
            </select>
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              追加
            </button>
          </form>
          <form
            className="allocation-rule-form"
            data-unsaved-id="create-variant"
            onSubmit={createVariant}
          >
            <select
              value={variantConceptId}
              onChange={(event) => setVariantConceptId(event.target.value)}
              required
            >
              <option value="">商品を選択</option>
              {catalog
                .filter((product) => !product.archived)
                .map((product) => (
                  <option key={product.id} value={product.id}>
                    {product.name}
                  </option>
                ))}
            </select>
            <input
              value={variantName}
              onChange={(event) => setVariantName(event.target.value)}
              placeholder="バリエーション名"
              required
            />
            <input
              value={variantBrand}
              onChange={(event) => setVariantBrand(event.target.value)}
              placeholder="ブランド"
            />
            <input
              value={variantPackageSize}
              onChange={(event) => setVariantPackageSize(event.target.value)}
              placeholder="内容量・規格"
            />
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              バリエーションを追加
            </button>
          </form>
          <form
            className="allocation-rule-form"
            data-unsaved-id="create-alias"
            onSubmit={createAlias}
          >
            <select
              value={aliasVariantId}
              onChange={(event) => setAliasVariantId(event.target.value)}
              required
            >
              <option value="">バリエーションを選択</option>
              {catalog.flatMap((product) =>
                product.variants
                  .filter((variant) => !variant.archived)
                  .map((variant) => (
                    <option key={variant.id} value={variant.id}>
                      {product.name} · {variant.name}
                    </option>
                  )),
              )}
            </select>
            <input
              value={aliasMerchant}
              onChange={(event) => setAliasMerchant(event.target.value)}
              placeholder="店名"
              required
            />
            <input
              value={aliasName}
              onChange={(event) => setAliasName(event.target.value)}
              placeholder="店頭の表記"
              required
            />
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              別表記を追加
            </button>
          </form>
        </section>
        {isOwner && (
          <ManageHouseholdInvitations
            householdId={household.id}
            hidden={activeSettingsTab !== "members"}
            disabled={pending || invitationPending}
            onNotice={onNotice}
            onPendingChange={setInvitationPending}
            onInvitationFormSaved={() => markFormClean("create-invitation")}
          />
        )}
        <ManageTags
          householdId={household.id}
          hidden={activeSettingsTab !== "catalog"}
          onNotice={onNotice}
          onFormDirty={markFormDirty}
          onFormClean={markFormClean}
          onPendingChange={setTagPending}
        />
        <ManageBudgets
          householdId={household.id}
          categories={household.categories}
          hidden={activeSettingsTab !== "planning"}
          onNotice={onNotice}
          onFormDirty={markFormDirty}
          onFormClean={markFormClean}
          onPendingChange={setBudgetPending}
        />
        <ManageRecurringExpenses
          householdId={household.id}
          categories={household.categories}
          accounts={household.accounts}
          hidden={activeSettingsTab !== "planning"}
          onNotice={onNotice}
          onFormDirty={markFormDirty}
          onFormClean={markFormClean}
          onPendingChange={setRecurringPending}
        />
        <section className="panel settings-section" hidden={activeSettingsTab !== "accounts"}>
          <div className="panel-heading">
            <div>
              <span className="eyebrow">口座間の移動</span>
              <h2>資金移動</h2>
            </div>
            <ArrowLeftRight size={18} />
          </div>
          <form
            className="allocation-rule-form"
            data-unsaved-id="account-income"
            onSubmit={(event) => void saveIncome(event)}
          >
            <label>
              入金先
              <select
                value={incomeAccountId}
                disabled={pending || Boolean(pendingIncomeIntent)}
                onChange={(event) => setIncomeAccountId(event.target.value)}
                required
              >
                <option value="">口座を選択</option>
                {household.accounts
                  .filter((account) => account.kind !== "CARD")
                  .map((account) => (
                    <option key={account.id} value={account.id}>
                      {account.name}
                    </option>
                  ))}
              </select>
            </label>
            <input
              aria-label="入金額"
              type="number"
              min="1"
              disabled={pending || Boolean(pendingIncomeIntent)}
              value={incomeAmount}
              onChange={(event) => setIncomeAmount(event.target.value)}
              placeholder="入金額"
              required
            />
            <input
              aria-label="入金日時"
              type="datetime-local"
              disabled={pending || Boolean(pendingIncomeIntent)}
              value={incomeDate}
              onChange={(event) => setIncomeDate(event.target.value)}
              required
            />
            <input
              value={incomeDescription}
              disabled={pending || Boolean(pendingIncomeIntent)}
              onChange={(event) => setIncomeDescription(event.target.value)}
              placeholder="入金の摘要"
            />
            <button
              className="button button-soft"
              disabled={pending || (!pendingIncomeIntent && !incomeAccountId)}
            >
              <Plus size={15} />
              {pendingIncomeIntent ? "入金を再試行" : "入金を記録"}
            </button>
          </form>
          <form
            className="allocation-rule-form"
            data-unsaved-id="account-movement"
            onSubmit={(event) => void saveMovement(event)}
          >
            <select
              value={movementKind}
              disabled={pending || Boolean(pendingMovementIntent)}
              onChange={(event) =>
                setMovementKind(event.target.value as "TRANSFER" | "CARD_BILL_PAYMENT")
              }
            >
              <option value="TRANSFER">口座移動</option>
              <option value="CARD_BILL_PAYMENT">カード請求の支払い</option>
            </select>
            <select
              aria-label="移動元"
              value={movementFrom}
              disabled={pending || Boolean(pendingMovementIntent)}
              onChange={(event) => setMovementFrom(event.target.value)}
              required
            >
              <option value="">移動元</option>
              {household.accounts
                .filter((account) =>
                  movementKind === "TRANSFER" ? account.kind !== "CARD" : account.kind === "BANK",
                )
                .map((account) => (
                  <option key={account.id} value={account.id}>
                    {account.name}
                  </option>
                ))}
            </select>
            <select
              aria-label="移動先"
              value={movementTo}
              disabled={pending || Boolean(pendingMovementIntent)}
              onChange={(event) => setMovementTo(event.target.value)}
              required
            >
              <option value="">移動先</option>
              {household.accounts
                .filter((account) =>
                  movementKind === "TRANSFER" ? account.kind !== "CARD" : account.kind === "CARD",
                )
                .map((account) => (
                  <option key={account.id} value={account.id}>
                    {account.name}
                  </option>
                ))}
            </select>
            <input
              aria-label="移動額"
              type="number"
              min="1"
              disabled={pending || Boolean(pendingMovementIntent)}
              value={movementAmount}
              onChange={(event) => setMovementAmount(event.target.value)}
              placeholder="金額"
              required
            />
            <input
              aria-label="移動日時"
              type="datetime-local"
              disabled={pending || Boolean(pendingMovementIntent)}
              value={movementDate}
              onChange={(event) => setMovementDate(event.target.value)}
              required
            />
            <input
              value={movementDescription}
              disabled={pending || Boolean(pendingMovementIntent)}
              onChange={(event) => setMovementDescription(event.target.value)}
              placeholder="摘要"
            />
            <button
              className="button button-primary"
              disabled={
                pending ||
                (!pendingMovementIntent &&
                  (!movementFrom || !movementTo || movementFrom === movementTo))
              }
            >
              <Check size={15} />
              {pendingMovementIntent ? "同じ移動を再試行" : "記録"}
            </button>
          </form>
          <div className="settings-rows">
            {incomes.map((income) => (
              <div className="setting-row" key={income.id}>
                <div>
                  <b>外部入金</b>
                  <small>
                    {new Date(income.occurredAt).toLocaleDateString("ja-JP")} ·{" "}
                    {household.accounts.find((account) => account.id === income.accountId)?.name} ·{" "}
                    {income.description}
                  </small>
                </div>
                <strong>{yen(income.amountYen)}</strong>
              </div>
            ))}
            {incomePageError && <InlineError message={incomePageError} />}
            {incomeHasNextPage && (
              <button
                className="button button-soft"
                disabled={incomePageLoading}
                onClick={() => void loadMoreIncomes()}
              >
                {incomePageLoading ? "読み込み中" : "入金履歴をさらに表示"}
              </button>
            )}
            {movements.map((movement) => (
              <div className="setting-row" key={movement.id}>
                <div>
                  <b>{movement.kind === "CARD_BILL_PAYMENT" ? "カード請求の支払い" : "口座移動"}</b>
                  <small>
                    {new Date(movement.occurredAt).toLocaleDateString("ja-JP")} ·{" "}
                    {movement.fromAccount.name} → {movement.toAccount.name} · {movement.description}
                  </small>
                </div>
                <strong>{yen(movement.amountYen)}</strong>
              </div>
            ))}
            {movementPageError && <InlineError message={movementPageError} />}
            {movementHasNextPage && (
              <button
                className="button button-soft"
                disabled={movementPageLoading}
                onClick={() => void loadMoreMovements()}
              >
                {movementPageLoading ? "読み込み中" : "口座移動履歴をさらに表示"}
              </button>
            )}
          </div>
        </section>
        <section
          className="panel settings-section member-section"
          hidden={activeSettingsTab !== "members"}
        >
          <div className="panel-heading">
            <div>
              <span className="eyebrow">共同編集</span>
              <h2>メンバー</h2>
            </div>
          </div>
          {household.members.map((member) => (
            <div className="setting-row" key={member.id}>
              <div className="profile-avatar">{member.displayName.slice(0, 1)}</div>
              <div>
                <b>{member.displayName}</b>
                <small>{member.role === "OWNER" ? "オーナー" : "メンバー"}</small>
              </div>
            </div>
          ))}
        </section>
      </div>
    </>
  );
}
