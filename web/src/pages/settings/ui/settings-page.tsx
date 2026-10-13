import { useEffect, useState, type FormEvent } from "react";
import { Activity, ArrowLeftRight, Check, ChevronRight, Plus, Wallet } from "lucide-react";
import {
  readGraphql,
  type Connection,
  type SharedPoolSettlementPolicy,
  type AllocationRule,
  type HouseholdInvitation,
  type ProductConcept,
  type Household,
  type AccountMovement,
  type AccountBalance,
  type AccountBalanceAnalysis,
  type AccountIncome,
  type Budget,
  type RecurringExpense,
  type Tag,
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
  archiveBudgetMutation,
  budgetsQuery,
  createBudgetMutation,
  updateBudgetMutation,
} from "../../../entities/budget";
import {
  archiveRecurringExpenseMutation,
  createRecurringExpenseMutation,
  readRecurringExpenses,
  updateRecurringExpenseMutation,
} from "../../../entities/recurring-expense";
import {
  archiveTagMutation,
  createTagMutation,
  tagsQuery,
  updateTagMutation,
} from "../../../entities/tag";
import {
  createProductConceptMutation,
  createProductVariantMutation,
  readProductCatalog,
  setProductAliasMutation,
} from "../../../entities/product";
import {
  createHouseholdInvitationMutation,
  householdInvitationsQuery,
  revokeHouseholdInvitationMutation,
} from "../api/household/operations";
import {
  accountMovementsQuery,
  accountBalancesQuery,
  accountBalanceAnalysisQuery,
  accountIncomesQuery,
  recordAccountMovementMutation,
  recordExternalIncomeMutation,
} from "../../../entities/ledger";
import { InlineError } from "../../../shared/ui";

const currentMonth = () =>
  new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
  }).format(new Date());
const localDateTime = () => {
  const date = new Date(Date.now() - new Date().getTimezoneOffset() * 60_000);
  return date.toISOString().slice(0, 16);
};
const yen = (amount: number) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const monthIndex = (month: string) => {
  const [year, number] = month.split("-").map(Number);
  return year * 12 + number - 1;
};
const shiftMonth = (month: string, offset: number) => {
  const shifted = monthIndex(month) + offset;
  return `${Math.floor(shifted / 12)}-${String((shifted % 12) + 1).padStart(2, "0")}`;
};
const balanceChangeSource = (sourceType: string) =>
  ({
    Account: "初期残高",
    Purchase: "購入",
    PurchaseCorrection: "訂正",
    PurchaseLedgerEntry: "購入",
    CorrectionLedgerEntry: "訂正",
    FundingLedgerEntry: "資金拠出",
    TransferLedgerEntry: "送金",
    AccountMovement: "口座振替",
    AccountIncome: "外部入金",
  })[sourceType] ?? "仕訳";

export function SettingsPage({
  household,
  onRefresh,
  onNotice,
  isOwner,
}: {
  household: Household;
  onRefresh: () => Promise<void>;
  onNotice: (message: string) => void;
  isOwner: boolean;
}) {
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
  const [catalog, setCatalog] = useState<ProductConcept[]>([]);
  const [invitations, setInvitations] = useState<HouseholdInvitation[]>([]);
  const [invitationsCursor, setInvitationsCursor] = useState<string | null>(null);
  const [invitationsHasNextPage, setInvitationsHasNextPage] = useState(false);
  const [invitationsPageLoading, setInvitationsPageLoading] = useState(false);
  const [invitationLink, setInvitationLink] = useState("");
  const [issuedInvitationId, setIssuedInvitationId] = useState("");
  const [expiresInDays, setExpiresInDays] = useState("7");
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
  const [tags, setTags] = useState<Tag[]>([]);
  const [tagName, setTagName] = useState("");
  const [tagDrafts, setTagDrafts] = useState<Record<string, string>>({});
  const [budgetPeriod, setBudgetPeriod] = useState(currentMonth);
  const [budgets, setBudgets] = useState<Budget[]>([]);
  const [budgetName, setBudgetName] = useState("");
  const [budgetCategoryId, setBudgetCategoryId] = useState("");
  const [budgetAmount, setBudgetAmount] = useState("");
  const [editingBudget, setEditingBudget] = useState<Budget | null>(null);
  const [recurring, setRecurring] = useState<RecurringExpense[]>([]);
  const [recurringName, setRecurringName] = useState("");
  const [recurringMerchant, setRecurringMerchant] = useState("");
  const [recurringAmount, setRecurringAmount] = useState("");
  const [recurringCategoryId, setRecurringCategoryId] = useState("");
  const [recurringAccountId, setRecurringAccountId] = useState("");
  const [recurringCadence, setRecurringCadence] = useState<RecurringExpense["cadence"]>("MONTHLY");
  const [recurringStartsOn, setRecurringStartsOn] = useState(new Date().toISOString().slice(0, 10));
  const [recurringNextDueOn, setRecurringNextDueOn] = useState(
    new Date().toISOString().slice(0, 10),
  );
  const [recurringEndsOn, setRecurringEndsOn] = useState("");
  const [editingRecurring, setEditingRecurring] = useState<RecurringExpense | null>(null);
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
  const [balanceHistoryAccountId, setBalanceHistoryAccountId] = useState(
    household.accounts[0]?.id ?? "",
  );
  const [balanceHistoryFrom, setBalanceHistoryFrom] = useState(() =>
    shiftMonth(currentMonth(), -5),
  );
  const [balanceHistoryTo, setBalanceHistoryTo] = useState(currentMonth);
  const [balanceHistory, setBalanceHistory] = useState<AccountBalanceAnalysis | null>(null);
  const [balanceHistoryRevision, setBalanceHistoryRevision] = useState(0);
  const [balanceHistoryLimit, setBalanceHistoryLimit] = useState(100);
  const [balanceHistoryLoading, setBalanceHistoryLoading] = useState(false);
  const [balanceHistoryError, setBalanceHistoryError] = useState("");
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
      onNotice((reason as Error).message);
    } finally {
      setAllocationRulesPageLoading(false);
    }
  };
  const refreshProducts = async () => {
    setCatalog(await readProductCatalog(household.id));
  };
  const refreshInvitations = async () => {
    if (!isOwner) return;
    const result = await readGraphql<{ householdInvitations: Connection<HouseholdInvitation> }>(
      householdInvitationsQuery,
      { householdId: household.id, first: 50 },
    );
    setInvitations(result.householdInvitations.edges.map((edge) => edge.node));
    setInvitationsCursor(result.householdInvitations.pageInfo.endCursor ?? null);
    setInvitationsHasNextPage(result.householdInvitations.pageInfo.hasNextPage);
  };
  const loadMoreInvitations = async () => {
    if (!invitationsCursor || invitationsPageLoading) return;
    setInvitationsPageLoading(true);
    try {
      const result = await readGraphql<{ householdInvitations: Connection<HouseholdInvitation> }>(
        householdInvitationsQuery,
        { householdId: household.id, after: invitationsCursor, first: 50 },
      );
      setInvitations((current) => [
        ...current,
        ...result.householdInvitations.edges.map((edge) => edge.node),
      ]);
      setInvitationsCursor(result.householdInvitations.pageInfo.endCursor ?? null);
      setInvitationsHasNextPage(result.householdInvitations.pageInfo.hasNextPage);
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setInvitationsPageLoading(false);
    }
  };
  const refreshPlanning = async () => {
    const [tagResult, budgetResult, recurringResult, movementResult, incomeResult, balanceResult] =
      await Promise.all([
        readGraphql<{ tags: Tag[] }>(tagsQuery, {
          householdId: household.id,
          includeArchived: true,
        }),
        readGraphql<{ budgets: Budget[] }>(budgetsQuery, {
          householdId: household.id,
          period: budgetPeriod,
        }),
        readRecurringExpenses(household.id, true),
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
    setTags(tagResult.tags);
    setBudgets(budgetResult.budgets);
    setRecurring(recurringResult);
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
            ownerMemberId: pool.ownerMemberId ?? "",
            policy: pool.settlementPolicy ?? "BALANCE_TO_STANDARD",
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
    if (!household.accounts.some((account) => account.id === balanceHistoryAccountId)) {
      setBalanceHistoryAccountId(household.accounts[0]?.id ?? "");
    }
  }, [household.accounts, balanceHistoryAccountId]);
  useEffect(() => {
    if (!balanceHistoryAccountId) {
      setBalanceHistory(null);
      setBalanceHistoryError("");
      setBalanceHistoryLoading(false);
      return;
    }
    if (
      !/^\d{4}-\d{2}$/.test(balanceHistoryFrom) ||
      !/^\d{4}-\d{2}$/.test(balanceHistoryTo) ||
      monthIndex(balanceHistoryTo) < monthIndex(balanceHistoryFrom) ||
      monthIndex(balanceHistoryTo) - monthIndex(balanceHistoryFrom) > 120
    ) {
      setBalanceHistory(null);
      setBalanceHistoryLimit(100);
      setBalanceHistoryError("期間は前後関係を確認し、10年以内で指定してください");
      setBalanceHistoryLoading(false);
      return;
    }
    let active = true;
    setBalanceHistory(null);
    setBalanceHistoryLimit(100);
    setBalanceHistoryLoading(true);
    setBalanceHistoryError("");
    void readGraphql<{ accountBalanceAnalysis: AccountBalanceAnalysis }>(
      accountBalanceAnalysisQuery,
      {
        householdId: household.id,
        accountId: balanceHistoryAccountId,
        from: balanceHistoryFrom,
        to: balanceHistoryTo,
      },
    )
      .then((result) => active && setBalanceHistory(result.accountBalanceAnalysis))
      .catch((reason) => active && setBalanceHistoryError((reason as Error).message))
      .finally(() => active && setBalanceHistoryLoading(false));
    return () => {
      active = false;
    };
  }, [
    household.id,
    balanceHistoryAccountId,
    balanceHistoryFrom,
    balanceHistoryTo,
    balanceHistoryRevision,
  ]);
  useEffect(() => {
    setInvitationLink("");
    setIssuedInvitationId("");
    void Promise.all([
      refreshRules(),
      refreshProducts(),
      refreshInvitations(),
      refreshPlanning(),
    ]).catch((reason) => onNotice((reason as Error).message));
  }, [household.id, isOwner]);
  useEffect(() => {
    let active = true;
    void readGraphql<{ budgets: Budget[] }>(budgetsQuery, {
      householdId: household.id,
      period: budgetPeriod,
    })
      .then((result) => active && setBudgets(result.budgets))
      .catch((reason) => active && onNotice((reason as Error).message));
    return () => {
      active = false;
    };
  }, [household.id, budgetPeriod]);
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
  ) => {
    setPending(true);
    try {
      await readGraphql<Record<string, unknown>>(mutation, { input });
      await onRefresh();
      onNotice("設定を保存しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const saveTag = async (event: FormEvent) => {
    event.preventDefault();
    const name = tagName.trim();
    if (!name) return;
    setPending(true);
    try {
      await readGraphql(createTagMutation, { input: { householdId: household.id, name } });
      setTagName("");
      await refreshPlanning();
      onNotice("タグを追加しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const renameTag = async (tag: Tag) => {
    const name = (tagDrafts[tag.id] ?? tag.name).trim();
    if (!name || name === tag.name) return;
    setPending(true);
    try {
      await readGraphql(updateTagMutation, {
        input: { tagId: tag.id, baseRevision: tag.revision, name },
      });
      setTagDrafts((current) => ({ ...current, [tag.id]: name }));
      await refreshPlanning();
      onNotice("タグ名を保存しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const archiveTag = async (tag: Tag) => {
    setPending(true);
    try {
      await readGraphql(archiveTagMutation, {
        input: {
          tagId: tag.id,
          baseRevision: tag.revision,
          archived: !tag.archived,
        },
      });
      await refreshPlanning();
      onNotice(tag.archived ? "タグを再表示しました" : "タグを非表示にしました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const saveBudget = async (event: FormEvent) => {
    event.preventDefault();
    const amountYen = Number(budgetAmount);
    if (!amountYen || amountYen < 1) return;
    setPending(true);
    try {
      const input = {
        householdId: household.id,
        categoryId: budgetCategoryId || null,
        name:
          budgetName.trim() ||
          (budgetCategoryId
            ? household.categories.find((category) => category.id === budgetCategoryId)?.name
            : "全体予算") ||
          "全体予算",
        amountYen,
        startsOn: editingBudget?.startsOn ?? `${budgetPeriod}-01`,
        endsOn: editingBudget?.endsOn ?? null,
      };
      if (editingBudget) {
        await readGraphql(updateBudgetMutation, {
          input: { ...input, budgetId: editingBudget.id, baseRevision: editingBudget.revision },
        });
      } else {
        await readGraphql(createBudgetMutation, { input });
      }
      setEditingBudget(null);
      setBudgetName("");
      setBudgetAmount("");
      await refreshPlanning();
      onNotice("予算を保存しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const editBudget = (budget: Budget) => {
    setEditingBudget(budget);
    setBudgetCategoryId(budget.categoryId ?? "");
    setBudgetName(budget.name);
    setBudgetAmount(String(budget.amountYen));
  };
  const archiveBudget = async (budget: Budget) => {
    setPending(true);
    try {
      await readGraphql(archiveBudgetMutation, {
        input: {
          budgetId: budget.id,
          baseRevision: budget.revision,
          archived: !budget.archived,
        },
      });
      await refreshPlanning();
      onNotice(budget.archived ? "予算を再表示しました" : "予算を非表示にしました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const saveRecurring = async (event: FormEvent) => {
    event.preventDefault();
    const amountYen = Number(recurringAmount);
    if (!recurringName.trim() || !amountYen || amountYen < 1) return;
    setPending(true);
    try {
      const input = {
        householdId: household.id,
        name: recurringName.trim(),
        merchant: recurringMerchant.trim(),
        amountYen,
        categoryId: recurringCategoryId || null,
        paymentAccountId: recurringAccountId || null,
        cadence: recurringCadence,
        startsOn: recurringStartsOn,
        nextDueOn: recurringNextDueOn,
        endsOn: recurringEndsOn || null,
      };
      if (editingRecurring) {
        await readGraphql(updateRecurringExpenseMutation, {
          input: {
            ...input,
            expenseId: editingRecurring.id,
            baseRevision: editingRecurring.revision,
          },
        });
      } else {
        await readGraphql(createRecurringExpenseMutation, { input });
      }
      setEditingRecurring(null);
      setRecurringName("");
      setRecurringMerchant("");
      setRecurringAmount("");
      setRecurringEndsOn("");
      await refreshPlanning();
      onNotice("定期支出を保存しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const editRecurring = (expense: RecurringExpense) => {
    setEditingRecurring(expense);
    setRecurringName(expense.name);
    setRecurringMerchant(expense.merchant);
    setRecurringAmount(String(expense.amountYen));
    setRecurringCategoryId(expense.categoryId ?? "");
    setRecurringAccountId(expense.paymentAccountId ?? "");
    setRecurringCadence(expense.cadence);
    setRecurringStartsOn(expense.startsOn);
    setRecurringNextDueOn(expense.nextDueOn);
    setRecurringEndsOn(expense.endsOn ?? "");
  };
  const archiveRecurringExpense = async (expense: RecurringExpense) => {
    setPending(true);
    try {
      await readGraphql(archiveRecurringExpenseMutation, {
        input: {
          expenseId: expense.id,
          baseRevision: expense.revision,
          archived: !expense.archived,
        },
      });
      await refreshPlanning();
      onNotice(expense.archived ? "定期支出を再表示しました" : "定期支出を終了しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const saveMovement = async (event: FormEvent) => {
    event.preventDefault();
    if (!movementFrom || !movementTo || movementFrom === movementTo || !Number(movementAmount))
      return;
    setPending(true);
    try {
      await readGraphql(recordAccountMovementMutation, {
        input: {
          householdId: household.id,
          kind: movementKind,
          fromAccountId: movementFrom,
          toAccountId: movementTo,
          amountYen: Number(movementAmount),
          occurredAt: new Date(movementDate).toISOString(),
          description: movementDescription.trim(),
          idempotencyKey: crypto.randomUUID(),
        },
      });
      setMovementAmount("");
      setMovementDescription("");
      setMovementDate(localDateTime());
      await refreshPlanning();
      setBalanceHistoryRevision((revision) => revision + 1);
      onNotice(
        movementKind === "TRANSFER" ? "口座移動を記録しました" : "カード請求の支払いを記録しました",
      );
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const saveIncome = async (event: FormEvent) => {
    event.preventDefault();
    if (!incomeAccountId || !Number(incomeAmount)) return;
    setPending(true);
    try {
      await readGraphql(recordExternalIncomeMutation, {
        input: {
          householdId: household.id,
          accountId: incomeAccountId,
          amountYen: Number(incomeAmount),
          occurredAt: new Date(incomeDate).toISOString(),
          description: incomeDescription.trim(),
          idempotencyKey: crypto.randomUUID(),
        },
      });
      setIncomeAmount("");
      setIncomeDescription("");
      setIncomeDate(localDateTime());
      await refreshPlanning();
      setBalanceHistoryRevision((revision) => revision + 1);
      onNotice("入金を記録しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const addPool = (event: FormEvent) => {
    event.preventDefault();
    void saveMutation(createPoolMutation, {
      householdId: household.id,
      name: poolName.trim(),
      kind: poolKind,
    }).then(() => setPoolName(""));
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
        setAccountName("");
        setOpeningBalanceYen("");
        await onRefresh();
        await refreshPlanning();
        onNotice("口座を追加しました");
      })
      .catch((reason) => onNotice((reason as Error).message))
      .finally(() => setPending(false));
  };
  const addCategory = (event: FormEvent) => {
    event.preventDefault();
    void saveMutation(createCategoryMutation, {
      householdId: household.id,
      name: categoryName.trim(),
    }).then(() => setCategoryName(""));
  };
  const saveAllocation = (event: FormEvent) => {
    event.preventDefault();
    void saveMutation(setDefaultAllocationMutation, {
      householdId: household.id,
      allocations: household.pools
        .map((pool) => ({
          poolId: pool.id,
          weight: Math.max(0, Number(shareWeights[pool.id]) || 0),
        }))
        .filter((item) => item.weight > 0),
    });
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
    if (pool.kind === "PERSONAL" && !draft.ownerMemberId) {
      onNotice("所有者を選択してください");
      return;
    }
    if (pool.kind === "SHARED" && fundingShares.length === 0) {
      onNotice("拠出比率を入力してください");
      return;
    }
    setPending(true);
    try {
      await readGraphql(setPoolFundingRuleMutation, {
        input: {
          householdId: household.id,
          poolId,
          ownerMemberId: pool.kind === "PERSONAL" ? draft.ownerMemberId : null,
          fundingShares: pool.kind === "SHARED" ? fundingShares : [],
          settlementPolicy: pool.kind === "SHARED" ? draft.policy : null,
          expectedVersion: pool.fundingRuleVersion,
        },
      });
      await onRefresh();
      onNotice("精算設定を保存しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const archiveCategory = async (categoryId: string, archived: boolean) => {
    setPending(true);
    try {
      await readGraphql(setCategoryArchivedMutation, { input: { categoryId, archived } });
      await onRefresh();
      onNotice(archived ? "カテゴリを非表示にしました" : "カテゴリを再表示しました");
    } catch (reason) {
      onNotice((reason as Error).message);
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
      await refreshRules();
      onNotice("負担ルールを保存しました");
      setRuleMerchant("");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const issueInvitation = async (event: FormEvent) => {
    event.preventDefault();
    setPending(true);
    try {
      const expiresAt = new Date(Date.now() + Number(expiresInDays) * 86_400_000).toISOString();
      const result = await readGraphql<{
        createHouseholdInvitation: { token: string; invitation: HouseholdInvitation };
      }>(createHouseholdInvitationMutation, { input: { householdId: household.id, expiresAt } });
      setInvitationLink(
        `${window.location.origin}/#invite=${encodeURIComponent(result.createHouseholdInvitation.token)}`,
      );
      setIssuedInvitationId(result.createHouseholdInvitation.invitation.id);
      await refreshInvitations();
      onNotice("招待を発行しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const revokeInvitation = async (invitationId: string) => {
    setPending(true);
    try {
      await readGraphql(revokeHouseholdInvitationMutation, {
        input: {
          householdId: household.id,
          invitationId,
        },
      });
      if (invitationId === issuedInvitationId) {
        setInvitationLink("");
        setIssuedInvitationId("");
      }
      await refreshInvitations();
      onNotice("招待を失効しました");
    } catch (reason) {
      onNotice((reason as Error).message);
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
      setConceptName("");
      await refreshProducts();
      onNotice("商品を追加しました");
    } catch (reason) {
      onNotice((reason as Error).message);
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
      setVariantName("");
      setVariantBrand("");
      setVariantPackageSize("");
      await refreshProducts();
      onNotice("バリエーションを追加しました");
    } catch (reason) {
      onNotice((reason as Error).message);
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
      setAliasName("");
      await refreshProducts();
      onNotice("店名の別表記を登録しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const visibleBalanceChanges = balanceHistory?.changes.slice(-balanceHistoryLimit).reverse() ?? [];
  const maxBalanceChange =
    balanceHistory?.changes.reduce(
      (maximum, entry) => Math.max(maximum, Math.abs(entry.changeYen)),
      1,
    ) ?? 1;
  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">共有と分類</span>
          <h1>設定</h1>
          <p>{household.name}の口座、決済元、カテゴリ</p>
        </div>
      </div>
      <div className="settings-layout">
        <section className="panel settings-section">
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
                      {pool.kind === "SHARED" ? "共用" : "個人"} · 品目負担の標準比{" "}
                      {pool.defaultShareWeight}
                    </small>
                  </div>
                  {(pool.kind === "PERSONAL" && !pool.ownerMemberId) ||
                  (pool.kind === "SHARED" &&
                    (!pool.fundingShares.length || !pool.settlementPolicy)) ? (
                    <span className="period-chip">設定が必要</span>
                  ) : null}
                  <ChevronRight size={16} />
                </div>
                <form
                  className="pool-funding-form"
                  onSubmit={(event) => void saveFundingRule(event, pool.id)}
                >
                  {pool.kind === "PERSONAL" ? (
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
          <form className="inline-create" onSubmit={addPool}>
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
        <section className="panel settings-section account-balance-analysis">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">口座別の仕訳</span>
              <h2>残高の増減</h2>
            </div>
            {balanceHistory?.currency && <span>{balanceHistory.currency}</span>}
          </div>
          <div className="analysis-controls">
            <label className="select-filter">
              <span>口座</span>
              <select
                value={balanceHistoryAccountId}
                disabled={!household.accounts.length}
                onChange={(event) => setBalanceHistoryAccountId(event.target.value)}
              >
                {household.accounts.map((account) => (
                  <option key={account.id} value={account.id}>
                    {account.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="select-filter">
              <span>開始月</span>
              <input
                aria-label="残高分析の開始月"
                type="month"
                value={balanceHistoryFrom}
                onChange={(event) => setBalanceHistoryFrom(event.target.value)}
              />
            </label>
            <label className="select-filter">
              <span>終了月</span>
              <input
                aria-label="残高分析の終了月"
                type="month"
                value={balanceHistoryTo}
                onChange={(event) => setBalanceHistoryTo(event.target.value)}
              />
            </label>
          </div>
          {balanceHistoryError && <InlineError message={balanceHistoryError} />}
          {balanceHistoryLoading ? (
            <p>残高を集計しています</p>
          ) : balanceHistory ? (
            <>
              <div className="account-balance-summary">
                <div>
                  <small>期首残高</small>
                  <b>{yen(balanceHistory.openingBalanceYen)}</b>
                </div>
                <div>
                  <small>期末残高</small>
                  <b>{yen(balanceHistory.closingBalanceYen)}</b>
                </div>
              </div>
              {household.accounts.find((account) => account.id === balanceHistoryAccountId)
                ?.kind === "CARD" && (
                <small className="account-balance-sign">カード債務の増加はプラス</small>
              )}
              <div className="account-balance-changes">
                {visibleBalanceChanges.map((change, index) => (
                  <div
                    className="account-balance-change"
                    key={`${change.source.id}-${change.occurredAt}-${index}`}
                  >
                    <div className="account-balance-change-copy">
                      <b>{change.description || balanceChangeSource(change.source.__typename)}</b>
                      <small>
                        {new Date(change.occurredAt).toLocaleString("ja-JP", {
                          month: "numeric",
                          day: "numeric",
                          hour: "2-digit",
                          minute: "2-digit",
                        })}{" "}
                        · {balanceChangeSource(change.source.__typename)}
                      </small>
                    </div>
                    <div className="account-balance-change-bar">
                      <i
                        className={change.changeYen < 0 ? "account-balance-negative" : ""}
                        style={{
                          width: `${Math.max(2, Math.min(100, (Math.abs(change.changeYen) / maxBalanceChange) * 100))}%`,
                        }}
                      />
                    </div>
                    <b className={change.changeYen < 0 ? "account-balance-negative-text" : ""}>
                      {change.changeYen > 0 ? "+" : ""}
                      {yen(change.changeYen)}
                    </b>
                    <small>残高 {yen(change.balanceYen)}</small>
                  </div>
                ))}
                {!balanceHistory.changes.length && <p>この期間に仕訳はありません</p>}
                {balanceHistory.changes.length > visibleBalanceChanges.length && (
                  <button
                    className="text-button"
                    type="button"
                    onClick={() => setBalanceHistoryLimit((limit) => limit + 100)}
                  >
                    過去の仕訳を表示
                  </button>
                )}
              </div>
            </>
          ) : null}
        </section>
        <section className="panel settings-section">
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
          <form className="inline-create" onSubmit={addAccount}>
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
        <section className="panel settings-section">
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
          <form className="inline-create" onSubmit={addCategory}>
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
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">買い物の初期値</span>
              <h2>標準の負担割合</h2>
            </div>
            <span className="count-badge">
              <Activity size={14} />
            </span>
          </div>
          <form onSubmit={saveAllocation}>
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
        <section className="panel settings-section">
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
          <form className="allocation-rule-form" onSubmit={saveRule}>
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
        <section className="panel settings-section">
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
          <form className="inline-create" onSubmit={createConcept}>
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
          <form className="allocation-rule-form" onSubmit={createVariant}>
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
          <form className="allocation-rule-form" onSubmit={createAlias}>
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
          <section className="panel settings-section">
            <div className="panel-heading">
              <div>
                <span className="eyebrow">共同編集</span>
                <h2>招待</h2>
              </div>
              <span className="count-badge">
                {invitations.filter((invitation) => invitation.status === "PENDING").length}
              </span>
            </div>
            <form className="inline-create" onSubmit={issueInvitation}>
              <label>
                有効期間
                <select
                  value={expiresInDays}
                  onChange={(event) => setExpiresInDays(event.target.value)}
                >
                  <option value="1">1日</option>
                  <option value="7">7日</option>
                  <option value="30">30日</option>
                </select>
              </label>
              <button className="button button-primary" disabled={pending}>
                <Plus size={15} />
                招待リンクを発行
              </button>
            </form>
            {invitationLink && (
              <div className="invitation-issued">
                <input aria-label="招待リンク" readOnly value={invitationLink} />
                <button
                  type="button"
                  className="button button-soft"
                  onClick={() =>
                    void navigator.clipboard
                      .writeText(invitationLink)
                      .then(() => onNotice("招待リンクをコピーしました"))
                      .catch(() => onNotice("招待リンクをコピーできませんでした"))
                  }
                >
                  リンクをコピー
                </button>
              </div>
            )}
            <div className="settings-rows">
              {invitations.map((invitation) => (
                <div className="setting-row" key={invitation.id}>
                  <div className="pool-avatar">招</div>
                  <div>
                    <b>{new Date(invitation.expiresAt).toLocaleDateString("ja-JP")}まで</b>
                    <small>
                      {invitation.status === "ACCEPTED"
                        ? "参加済み"
                        : invitation.status === "REVOKED"
                          ? "失効"
                          : invitation.status === "EXPIRED"
                            ? "期限切れ"
                            : "未使用"}
                    </small>
                  </div>
                  {invitation.status === "PENDING" && (
                    <button
                      className="text-button"
                      disabled={pending}
                      onClick={() => void revokeInvitation(invitation.id)}
                    >
                      失効
                    </button>
                  )}
                </div>
              ))}
            </div>
            {invitationsHasNextPage && (
              <button
                className="text-button"
                type="button"
                disabled={invitationsPageLoading}
                onClick={() => void loadMoreInvitations()}
              >
                {invitationsPageLoading ? "読み込み中" : "さらに表示"}
              </button>
            )}
          </section>
        )}
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">品目管理</span>
              <h2>タグ</h2>
            </div>
            <span className="count-badge">{tags.filter((tag) => !tag.archived).length}</span>
          </div>
          <div className="settings-rows">
            {tags.map((tag) => (
              <div className="setting-row" key={tag.id}>
                <input
                  aria-label={`${tag.name}のタグ名`}
                  value={tagDrafts[tag.id] ?? tag.name}
                  disabled={tag.archived}
                  onChange={(event) =>
                    setTagDrafts((current) => ({ ...current, [tag.id]: event.target.value }))
                  }
                />
                <button
                  className="text-button"
                  disabled={
                    pending || tag.archived || (tagDrafts[tag.id] ?? tag.name).trim() === tag.name
                  }
                  onClick={() => void renameTag(tag)}
                >
                  保存
                </button>
                <button
                  className="text-button"
                  disabled={pending}
                  onClick={() => void archiveTag(tag)}
                >
                  {tag.archived ? "再表示" : "非表示"}
                </button>
              </div>
            ))}
          </div>
          <form className="inline-create" onSubmit={(event) => void saveTag(event)}>
            <input
              value={tagName}
              onChange={(event) => setTagName(event.target.value)}
              placeholder="タグ名"
              maxLength={80}
              required
            />
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              追加
            </button>
          </form>
        </section>
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">予算と実績</span>
              <h2>月次予算</h2>
            </div>
            <input
              aria-label="予算の対象月"
              type="month"
              value={budgetPeriod}
              onChange={(event) => setBudgetPeriod(event.target.value)}
            />
          </div>
          <div className="settings-rows">
            {budgets.map((budget) => {
              const ratio =
                budget.amountYen > 0
                  ? Math.min(100, (budget.actualYen / budget.amountYen) * 100)
                  : 0;
              return (
                <div className="setting-row" key={budget.id}>
                  <div>
                    <b>{budget.name}</b>
                    <small>
                      {yen(budget.actualYen)} / {yen(budget.amountYen)} ·{" "}
                      {budget.remainingYen >= 0
                        ? `残り${yen(budget.remainingYen)}`
                        : `${yen(-budget.remainingYen)}超過`}
                    </small>
                    <progress max="100" value={ratio} aria-label={`${budget.name}の予算使用率`} />
                  </div>
                  <button
                    className="text-button"
                    disabled={pending}
                    onClick={() => editBudget(budget)}
                  >
                    編集
                  </button>
                  <button
                    className="text-button"
                    disabled={pending}
                    onClick={() => void archiveBudget(budget)}
                  >
                    非表示
                  </button>
                </div>
              );
            })}
          </div>
          <form className="inline-create" onSubmit={(event) => void saveBudget(event)}>
            <input
              value={budgetName}
              onChange={(event) => setBudgetName(event.target.value)}
              placeholder="予算名（省略可）"
            />
            <select
              value={budgetCategoryId}
              onChange={(event) => setBudgetCategoryId(event.target.value)}
            >
              <option value="">全体</option>
              {household.categories
                .filter((category) => !category.archived)
                .map((category) => (
                  <option key={category.id} value={category.id}>
                    {category.name}
                  </option>
                ))}
            </select>
            <input
              aria-label="予算額"
              type="number"
              min="1"
              step="1"
              value={budgetAmount}
              onChange={(event) => setBudgetAmount(event.target.value)}
              placeholder="予算額"
              required
            />
            <button className="button button-primary" disabled={pending}>
              <Check size={15} />
              {editingBudget ? "変更を保存" : "予算を追加"}
            </button>
            {editingBudget && (
              <button
                type="button"
                className="text-button"
                onClick={() => {
                  setEditingBudget(null);
                  setBudgetName("");
                  setBudgetAmount("");
                }}
              >
                取消
              </button>
            )}
          </form>
        </section>
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">予定</span>
              <h2>定期支出</h2>
            </div>
            <span className="count-badge">
              {recurring.filter((expense) => !expense.archived).length}
            </span>
          </div>
          <div className="settings-rows">
            {recurring.map((expense) => (
              <div className="setting-row" key={expense.id}>
                <div>
                  <b>{expense.name}</b>
                  <small>
                    {expense.merchant} · {yen(expense.amountYen)} ·{" "}
                    {expense.cadence === "WEEKLY"
                      ? "毎週"
                      : expense.cadence === "YEARLY"
                        ? "毎年"
                        : "毎月"}{" "}
                    · 次回 {expense.nextDueOn}
                  </small>
                </div>
                <button
                  className="text-button"
                  disabled={pending || expense.archived}
                  onClick={() => editRecurring(expense)}
                >
                  編集
                </button>
                <button
                  className="text-button"
                  disabled={pending}
                  onClick={() => void archiveRecurringExpense(expense)}
                >
                  {expense.archived ? "再表示" : "終了"}
                </button>
              </div>
            ))}
          </div>
          <form className="allocation-rule-form" onSubmit={(event) => void saveRecurring(event)}>
            <input
              value={recurringName}
              onChange={(event) => setRecurringName(event.target.value)}
              placeholder="支出名"
              required
            />
            <input
              value={recurringMerchant}
              onChange={(event) => setRecurringMerchant(event.target.value)}
              placeholder="店舗"
            />
            <input
              aria-label="定期支出額"
              type="number"
              min="1"
              value={recurringAmount}
              onChange={(event) => setRecurringAmount(event.target.value)}
              placeholder="金額"
              required
            />
            <select
              aria-label="頻度"
              value={recurringCadence}
              onChange={(event) =>
                setRecurringCadence(event.target.value as RecurringExpense["cadence"])
              }
            >
              <option value="WEEKLY">毎週</option>
              <option value="MONTHLY">毎月</option>
              <option value="YEARLY">毎年</option>
            </select>
            <select
              aria-label="カテゴリ"
              value={recurringCategoryId}
              onChange={(event) => setRecurringCategoryId(event.target.value)}
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
            <select
              aria-label="決済元"
              value={recurringAccountId}
              onChange={(event) => setRecurringAccountId(event.target.value)}
            >
              <option value="">決済元なし</option>
              {household.accounts.map((account) => (
                <option key={account.id} value={account.id}>
                  {account.name}
                </option>
              ))}
            </select>
            <label>
              開始日
              <input
                type="date"
                value={recurringStartsOn}
                onChange={(event) => setRecurringStartsOn(event.target.value)}
                required
              />
            </label>
            <label>
              次回日
              <input
                type="date"
                value={recurringNextDueOn}
                onChange={(event) => setRecurringNextDueOn(event.target.value)}
                required
              />
            </label>
            <label>
              終了日
              <input
                type="date"
                value={recurringEndsOn}
                onChange={(event) => setRecurringEndsOn(event.target.value)}
              />
            </label>
            <button className="button button-primary" disabled={pending}>
              <Check size={15} />
              {editingRecurring ? "変更を保存" : "予定を追加"}
            </button>
            {editingRecurring && (
              <button
                type="button"
                className="text-button"
                onClick={() => setEditingRecurring(null)}
              >
                取消
              </button>
            )}
          </form>
        </section>
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">口座間の移動</span>
              <h2>資金移動</h2>
            </div>
            <ArrowLeftRight size={18} />
          </div>
          <form className="allocation-rule-form" onSubmit={(event) => void saveIncome(event)}>
            <label>
              入金先
              <select
                value={incomeAccountId}
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
              value={incomeAmount}
              onChange={(event) => setIncomeAmount(event.target.value)}
              placeholder="入金額"
              required
            />
            <input
              aria-label="入金日時"
              type="datetime-local"
              value={incomeDate}
              onChange={(event) => setIncomeDate(event.target.value)}
              required
            />
            <input
              value={incomeDescription}
              onChange={(event) => setIncomeDescription(event.target.value)}
              placeholder="入金の摘要"
            />
            <button className="button button-soft" disabled={pending || !incomeAccountId}>
              <Plus size={15} />
              入金を記録
            </button>
          </form>
          <form className="allocation-rule-form" onSubmit={(event) => void saveMovement(event)}>
            <select
              value={movementKind}
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
              value={movementAmount}
              onChange={(event) => setMovementAmount(event.target.value)}
              placeholder="金額"
              required
            />
            <input
              aria-label="移動日時"
              type="datetime-local"
              value={movementDate}
              onChange={(event) => setMovementDate(event.target.value)}
              required
            />
            <input
              value={movementDescription}
              onChange={(event) => setMovementDescription(event.target.value)}
              placeholder="摘要"
            />
            <button
              className="button button-primary"
              disabled={pending || !movementFrom || !movementTo || movementFrom === movementTo}
            >
              <Check size={15} />
              記録
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
        <section className="panel settings-section member-section">
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
