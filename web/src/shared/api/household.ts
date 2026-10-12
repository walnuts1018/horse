import { commitMutation, fetchQuery, graphql } from "react-relay";
import {
  Environment,
  Network,
  RecordSource,
  Store,
  type GraphQLTaggedNode,
  type GraphQLResponse,
  type OperationType,
} from "relay-runtime";

export type SharedPoolSettlementPolicy = "BALANCE_TO_STANDARD" | "REIMBURSE_PERSONAL";
export type PoolFundingShare = { memberId: string; memberName: string; weight: number };
export type Pool = {
  id: string;
  name: string;
  kind: string;
  defaultShareWeight: number;
  ownerMemberId?: string | null;
  fundingShares: PoolFundingShare[];
  settlementPolicy?: SharedPoolSettlementPolicy | null;
  fundingRuleVersion: number;
};
export type Account = { id: string; name: string; kind: string; poolId?: string | null };
export type Category = { id: string; name: string; archived?: boolean };
export type Allocation = { poolId: string; poolName?: string; weight: number; amountYen?: number };
export type Tag = {
  id: string;
  householdId: string;
  name: string;
  archived: boolean;
  revision: number;
};
export type Budget = {
  id: string;
  householdId: string;
  categoryId?: string | null;
  name: string;
  amountYen: number;
  actualYen: number;
  remainingYen: number;
  startsOn: string;
  endsOn?: string | null;
  archived: boolean;
  revision: number;
};
export type RecurringExpense = {
  id: string;
  householdId: string;
  name: string;
  merchant: string;
  amountYen: number;
  currency: string;
  categoryId?: string | null;
  paymentAccountId?: string | null;
  cadence: string;
  startsOn: string;
  nextDueOn: string;
  endsOn?: string | null;
  archived: boolean;
  revision: number;
};
export type AccountMovement = {
  id: string;
  householdId: string;
  kind: "TRANSFER" | "CARD_BILL_PAYMENT";
  occurredAt: string;
  fromAccountId: string;
  toAccountId: string;
  amountYen: number;
  currency: string;
  description: string;
  createdAt: string;
};
export type AccountBalance = { accountId: string; balanceYen: number; currency: string };
export type AccountBalanceChange = {
  occurredAt: string;
  sourceKind: string;
  sourceId: string;
  description: string;
  changeYen: number;
  balanceYen: number;
};
export type AccountBalanceAnalysis = {
  accountId: string;
  currency: string;
  from: string;
  to: string;
  openingBalanceYen: number;
  closingBalanceYen: number;
  changes: AccountBalanceChange[];
};
export type AccountIncome = {
  id: string;
  householdId: string;
  accountId: string;
  amountYen: number;
  currency: string;
  occurredAt: string;
  description: string;
  createdAt: string;
};
export type PurchaseItem = {
  id: string;
  name: string;
  amountYen: number;
  revision: number;
  categoryId?: string | null;
  productVariantId?: string | null;
  allocations: Allocation[];
  tags: Tag[];
};
export type Purchase = {
  id: string;
  householdId: string;
  occurredAt: string;
  merchant: string;
  paymentAccountId?: string | null;
  paymentAccount?: Account | null;
  paymentAllocations?: PurchasePayment[];
  corrections?: PurchaseCorrection[];
  items: PurchaseItem[];
  totalYen: number;
  revision: number;
  status: string;
  createdAt: string;
  updatedAt: string;
};
export type CreatePurchaseInput = {
  idempotencyKey: string;
  householdId: string;
  occurredAt: string;
  merchant: string;
  paymentAccountId: string | null;
  paymentAllocations?: PurchasePaymentInput[];
  items: Array<{
    name: string;
    amountYen: number;
    categoryId: string | null;
    productVariantId?: string | null;
    allocations: Array<{ poolId: string; weight: number }> | null;
  }>;
};
export type PurchasePaymentInput = { paymentAccountId: string; amountYen: number };
export type PurchasePayment = PurchasePaymentInput & { account?: Account | null };
export type PurchaseCorrection = {
  id: string;
  itemId?: string | null;
  amountDeltaYen: number;
  reason: string;
  occurredAt: string;
  settlementMonth: string;
  paymentAllocations: PurchasePayment[];
};
export type LoanBalance = {
  loanId: string;
  lenderMemberId: string;
  borrowerMemberId: string;
  principalYen: number;
  repaidYen: number;
  balanceYen: number;
};
export type Member = { id: string; displayName: string; role: string };
export type Household = {
  id: string;
  name: string;
  revision: number;
  members: Member[];
  pools: Pool[];
  accounts: Account[];
  categories: Category[];
  defaultAllocation: Allocation[];
};
export type Settlement = {
  period: string;
  status: string;
  revision: number;
  lines: Array<{
    memberId: string;
    memberName: string;
    scheduledYen: number;
    paidYen: number;
    carryYen: number;
    priorBalanceYen: number;
    monthlyAdjustmentYen: number;
    advanceCreditYen: number;
    amountDueYen: number;
  }>;
};
export type AllocationRule = {
  id: string;
  householdId: string;
  scope: "PRODUCT_VARIANT" | "PRODUCT" | "CATEGORY" | "MERCHANT";
  categoryId?: string | null;
  merchant?: string | null;
  productConceptId?: string | null;
  productVariantId?: string | null;
  revision: number;
  allocations: Allocation[];
};
export type ProductAlias = { id: string; alias: string; merchantKey: string; revision: number };
export type ProductVariant = {
  id: string;
  householdId: string;
  conceptId: string;
  name: string;
  normalizedName: string;
  brand: string;
  packageSize: string;
  barcode?: string | null;
  archived: boolean;
  revision: number;
  aliases: ProductAlias[];
};
export type ProductConcept = {
  id: string;
  householdId: string;
  name: string;
  normalizedName: string;
  categoryId?: string | null;
  archived: boolean;
  revision: number;
  variants: ProductVariant[];
};
export type ProductSuggestion = {
  concept: ProductConcept;
  variant: ProductVariant;
  matchedName: string;
  score: number;
};
export type HouseholdInvitation = {
  id: string;
  householdId: string;
  role: string;
  expiresAt: string;
  createdAt: string;
  revokedAt?: string | null;
  acceptedAt?: string | null;
};
export type LedgerEntry = {
  id: string;
  kind: string;
  occurredAt: string;
  description: string;
  amountYen: number;
  currency: string;
  fromMemberId?: string | null;
  toMemberId?: string | null;
  poolId?: string | null;
  sourcePurchaseId?: string | null;
};
export type Connection<T> = {
  edges: Array<{ cursor: string; node: T }>;
  pageInfo: { endCursor?: string | null; hasNextPage: boolean };
  totalCount: number;
};

const apiErrorMessages: Record<string, string> = {
  BAD_USER_INPUT: "入力内容を確認してください",
  NOT_FOUND: "対象の記録が見つかりません。画面を更新してください",
  UNAUTHENTICATED: "ログインし直してから再送してください",
  FORBIDDEN: "操作する権限がありません。管理者に確認してください",
  CONFLICT: "記録が更新されました。最新の帳簿を確認してください",
  SERVER_ERROR: "一時的なサーバーエラーです。時間をおいて再送してください",
  INTERNAL: "一時的なサーバーエラーです。時間をおいて再送してください",
  INTERNAL_SERVER_ERROR: "一時的なサーバーエラーです。時間をおいて再送してください",
  SERVICE_UNAVAILABLE: "一時的なサーバーエラーです。時間をおいて再送してください",
  BAD_GATEWAY: "一時的なサーバーエラーです。時間をおいて再送してください",
  GATEWAY_TIMEOUT: "一時的なサーバーエラーです。時間をおいて再送してください",
};

export class ApiError extends Error {
  readonly code?: string;
  readonly status?: number;

  constructor(message: string, code?: string, status?: number) {
    const statusCode =
      code ??
      (status === 401
        ? "UNAUTHENTICATED"
        : status === 403
          ? "FORBIDDEN"
          : status === 404
            ? "NOT_FOUND"
            : status === 409
              ? "CONFLICT"
              : status !== undefined && status >= 500
                ? "SERVER_ERROR"
                : undefined);
    super((statusCode && apiErrorMessages[statusCode]) || message);
    this.code = statusCode;
    this.status = status;
  }
}

let environment: Environment | undefined;

export function getRelayEnvironment() {
  if (environment) return environment;
  const network = Network.create(async (operation, variables) => {
    const response = await fetch("/graphql", {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ query: operation.text, variables }),
    });
    if (!response.ok)
      throw new ApiError(`GraphQL API returned ${response.status}`, undefined, response.status);
    const result = (await response.json()) as {
      data?: unknown;
      errors?: Array<{ message: string; extensions?: { code?: string } }>;
    };
    if (result.errors?.length)
      throw new ApiError(result.errors[0].message, result.errors[0].extensions?.code);
    return result as GraphQLResponse;
  });
  const relayEnvironment = new Environment({ network, store: new Store(new RecordSource()) });
  environment = relayEnvironment;
  return relayEnvironment;
}

export async function executeGraphql<T, V extends OperationType["variables"]>(
  operation: GraphQLTaggedNode,
  variables: V,
): Promise<T> {
  const request = operation as { params?: { operationKind?: string } };
  if (request.params?.operationKind === "mutation") {
    return new Promise<T>((resolve, reject) =>
      commitMutation(getRelayEnvironment(), {
        mutation: operation,
        variables,
        onCompleted: (response, errors) =>
          errors?.length
            ? reject(
                new ApiError(
                  errors[0].message,
                  (errors[0] as Error & { extensions?: { code?: string } }).extensions?.code,
                ),
              )
            : resolve(response as T),
        onError: reject,
      }),
    );
  }
  const result = await fetchQuery(getRelayEnvironment(), operation, variables, {
    fetchPolicy: "network-only",
  }).toPromise();
  return result as T;
}

export async function readGraphql<T>(
  operation: GraphQLTaggedNode,
  variables: Record<string, unknown> = {},
): Promise<T> {
  return executeGraphql<T, Record<string, unknown>>(operation, variables);
}

export async function getViewer(): Promise<{ id: string; displayName: string; email?: string }> {
  const response = await fetch("/auth/me", { credentials: "include" });
  if (!response.ok)
    throw new ApiError(`Session API returned ${response.status}`, undefined, response.status);
  const data = (await response.json()) as {
    authenticated?: boolean;
    viewer?: { id: string; displayName?: string; email?: string };
  };
  if (data.authenticated === false)
    throw new ApiError("ログインが必要です", "UNAUTHENTICATED", 401);
  const viewer = data.viewer;
  if (!viewer?.id) throw new ApiError("ログイン情報を取得できませんでした");
  return { id: viewer.id, displayName: viewer.displayName ?? "メンバー", email: viewer.email };
}

export const householdQuery = graphql`
  query HouseholdQuery($id: ID!) {
    household(id: $id) {
      id
      name
      revision
      members {
        id
        displayName
        role
      }
      pools {
        id
        name
        kind
        defaultShareWeight
        ownerMemberId
        fundingShares {
          memberId
          memberName
          weight
        }
        settlementPolicy
        fundingRuleVersion
      }
      accounts {
        id
        name
        kind
        poolId
      }
      categories {
        id
        name
        parentId
        archived
      }
      defaultAllocation {
        poolId
        weight
      }
    }
  }
`;

export const householdsQuery = graphql`
  query HouseholdsQuery {
    households {
      id
      name
    }
  }
`;

export const purchasesQuery = graphql`
  query PurchasesQuery($householdId: ID!, $from: String, $to: String, $after: String, $first: Int) {
    purchases(householdId: $householdId, from: $from, to: $to, after: $after, first: $first) {
      edges {
        cursor
        node {
          id
          householdId
          occurredAt
          merchant
          paymentAccountId
          paymentAccount {
            id
            name
            kind
            poolId
          }
          paymentAllocations {
            paymentAccountId
            amountYen
            account {
              id
              name
            }
          }
          items {
            id
            name
            amountYen
            revision
            categoryId
            productVariantId
            allocations {
              poolId
              poolName
              weight
              amountYen
            }
            tags {
              id
              householdId
              name
              archived
              revision
            }
          }
          totalYen
          corrections {
            id
            itemId
            amountDeltaYen
            reason
            occurredAt
            settlementMonth
            paymentAllocations {
              paymentAccountId
              amountYen
              account {
                id
                name
              }
            }
          }
          revision
          status
          createdAt
          updatedAt
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
      totalCount
    }
  }
`;

export const settlementQuery = graphql`
  query SettlementQuery($householdId: ID!, $period: String!) {
    settlement(householdId: $householdId, period: $period) {
      period
      status
      revision
      lines {
        memberId
        memberName
        scheduledYen
        paidYen
        carryYen
        priorBalanceYen
        monthlyAdjustmentYen
        advanceCreditYen
        amountDueYen
      }
    }
  }
`;
export const monthlyCategorySpendingQuery = graphql`
  query MonthlyCategorySpendingQuery($householdId: ID!, $from: String!, $to: String!) {
    monthlyCategorySpending(householdId: $householdId, from: $from, to: $to) {
      period
      categoryId
      categoryName
      amountYen
    }
  }
`;
export const allocationRulesQuery = graphql`
  query AllocationRulesQuery($householdId: ID!) {
    allocationRules(householdId: $householdId) {
      id
      householdId
      scope
      categoryId
      merchant
      productConceptId
      productVariantId
      revision
      allocations {
        poolId
        poolName
        weight
        amountYen
      }
    }
  }
`;

export const ledgerQuery = graphql`
  query LedgerQuery($householdId: ID!, $after: String, $first: Int) {
    ledger(householdId: $householdId, after: $after, first: $first) {
      edges {
        cursor
        node {
          id
          kind
          occurredAt
          description
          amountYen
          currency
          fromMemberId
          toMemberId
          poolId
          sourcePurchaseId
          loanId
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
      totalCount
    }
  }
`;

export const loanBalancesQuery = graphql`
  query LoanBalancesQuery($householdId: ID!) {
    loanBalances(householdId: $householdId) {
      loanId
      lenderMemberId
      borrowerMemberId
      principalYen
      repaidYen
      balanceYen
    }
  }
`;

export const createHouseholdMutation = graphql`
  mutation CreateHouseholdMutation($input: CreateHouseholdInput!) {
    createHousehold(input: $input) {
      id
    }
  }
`;

export const createPurchaseMutation = graphql`
  mutation CreatePurchaseMutation($input: CreatePurchaseInput!) {
    createPurchase(input: $input) {
      id
      householdId
      occurredAt
      merchant
      paymentAccountId
      totalYen
      revision
      status
      items {
        id
        name
        amountYen
        categoryId
        productVariantId
        tags {
          id
          name
          archived
          revision
          householdId
        }
        allocations {
          poolId
          weight
          amountYen
        }
      }
    }
  }
`;

export const updatePurchaseMutation = graphql`
  mutation UpdatePurchaseMutation($input: UpdatePurchaseInput!) {
    updatePurchase(input: $input) {
      id
      revision
      occurredAt
      merchant
      paymentAccountId
    }
  }
`;

export const updatePurchaseItemMutation = graphql`
  mutation UpdatePurchaseItemMutation($input: UpdatePurchaseItemInput!) {
    updatePurchaseItem(input: $input) {
      id
      revision
      name
      amountYen
      categoryId
    }
  }
`;

export const setCategoryArchivedMutation = graphql`
  mutation SetCategoryArchivedMutation($categoryId: ID!, $archived: Boolean!) {
    setCategoryArchived(categoryId: $categoryId, archived: $archived) {
      id
      archived
    }
  }
`;
export const setAllocationRuleMutation = graphql`
  mutation SetAllocationRuleMutation($input: SetAllocationRuleInput!) {
    setAllocationRule(input: $input) {
      id
      scope
      categoryId
      merchant
      productConceptId
      productVariantId
      revision
      allocations {
        poolId
        weight
      }
    }
  }
`;

export const householdInvitationsQuery = graphql`
  query HouseholdInvitationsQuery($householdId: ID!) {
    householdInvitations(householdId: $householdId) {
      id
      householdId
      role
      expiresAt
      createdAt
      revokedAt
      acceptedAt
    }
  }
`;

export const createHouseholdInvitationMutation = graphql`
  mutation CreateHouseholdInvitationMutation($input: CreateHouseholdInvitationInput!) {
    createHouseholdInvitation(input: $input) {
      token
      invitation {
        id
        householdId
        role
        expiresAt
        createdAt
        revokedAt
        acceptedAt
      }
    }
  }
`;

export const revokeHouseholdInvitationMutation = graphql`
  mutation RevokeHouseholdInvitationMutation($householdId: ID!, $invitationId: ID!) {
    revokeHouseholdInvitation(householdId: $householdId, invitationId: $invitationId) {
      id
      revokedAt
    }
  }
`;

export const acceptHouseholdInvitationMutation = graphql`
  mutation AcceptHouseholdInvitationMutation($token: String!) {
    acceptHouseholdInvitation(token: $token) {
      id
      name
    }
  }
`;

export const productCatalogQuery = graphql`
  query ProductCatalogQuery($householdId: ID!) {
    productCatalog(householdId: $householdId) {
      id
      householdId
      name
      normalizedName
      categoryId
      archived
      revision
      variants {
        id
        householdId
        conceptId
        name
        normalizedName
        brand
        packageSize
        barcode
        archived
        revision
        aliases {
          id
          alias
          merchantKey
          revision
        }
      }
    }
  }
`;

export const productSuggestionsQuery = graphql`
  query ProductSuggestionsQuery(
    $householdId: ID!
    $query: String!
    $merchant: String
    $first: Int
  ) {
    productSuggestions(
      householdId: $householdId
      query: $query
      merchant: $merchant
      first: $first
    ) {
      concept {
        id
        name
        categoryId
      }
      variant {
        id
        name
        brand
        packageSize
      }
      matchedName
      score
    }
  }
`;

export const createProductConceptMutation = graphql`
  mutation CreateProductConceptMutation($input: CreateProductConceptInput!) {
    createProductConcept(input: $input) {
      id
      name
    }
  }
`;

export const createProductVariantMutation = graphql`
  mutation CreateProductVariantMutation($input: CreateProductVariantInput!) {
    createProductVariant(input: $input) {
      id
      name
    }
  }
`;

export const setProductAliasMutation = graphql`
  mutation SetProductAliasMutation($input: SetProductAliasInput!) {
    setProductAlias(input: $input) {
      id
      alias
      merchantKey
      revision
    }
  }
`;

export const createPoolMutation = graphql`
  mutation CreatePoolMutation($input: CreatePoolInput!) {
    createPool(input: $input) {
      id
    }
  }
`;
export const createAccountMutation = graphql`
  mutation CreateAccountMutation($input: CreateAccountInput!) {
    createAccount(input: $input) {
      id
    }
  }
`;
export const createCategoryMutation = graphql`
  mutation CreateCategoryMutation($input: CreateCategoryInput!) {
    createCategory(input: $input) {
      id
    }
  }
`;
export const setDefaultAllocationMutation = graphql`
  mutation SetDefaultAllocationMutation($input: SetDefaultAllocationInput!) {
    setDefaultAllocation(input: $input) {
      id
      revision
    }
  }
`;
export const setPoolFundingRuleMutation = graphql`
  mutation SetPoolFundingRuleMutation($input: SetPoolFundingRuleInput!) {
    setPoolFundingRule(input: $input) {
      id
      kind
      ownerMemberId
      fundingRuleVersion
      settlementPolicy
      fundingShares {
        memberId
        memberName
        weight
      }
    }
  }
`;
export const recordFundingMutation = graphql`
  mutation RecordFundingMutation($input: RecordFundingInput!) {
    recordFunding(input: $input) {
      id
    }
  }
`;
export const recordTransferMutation = graphql`
  mutation RecordTransferMutation($input: RecordTransferInput!) {
    recordTransfer(input: $input) {
      id
      loanId
    }
  }
`;
export const correctPurchaseMutation = graphql`
  mutation CorrectPurchaseMutation($input: CorrectPurchaseInput!) {
    correctPurchase(input: $input) {
      id
      revision
    }
  }
`;
export const closePeriodMutation = graphql`
  mutation ClosePeriodMutation($input: ClosePeriodInput!) {
    closePeriod(input: $input) {
      period
      status
      revision
    }
  }
`;

export const tagsQuery = graphql`
  query TagsQuery($householdId: ID!, $includeArchived: Boolean) {
    tags(householdId: $householdId, includeArchived: $includeArchived) {
      id
      householdId
      name
      archived
      revision
    }
  }
`;

export const createTagMutation = graphql`
  mutation CreateTagMutation($input: CreateTagInput!) {
    createTag(input: $input) {
      id
      householdId
      name
      archived
      revision
    }
  }
`;

export const updateTagMutation = graphql`
  mutation UpdateTagMutation($input: UpdateTagInput!) {
    updateTag(input: $input) {
      id
      householdId
      name
      archived
      revision
    }
  }
`;

export const archiveTagMutation = graphql`
  mutation ArchiveTagMutation($tagId: ID!, $baseRevision: Int!, $archived: Boolean!) {
    archiveTag(tagId: $tagId, baseRevision: $baseRevision, archived: $archived) {
      id
      archived
      revision
    }
  }
`;

export const bulkUpdatePurchaseItemsMutation = graphql`
  mutation BulkUpdatePurchaseItemsMutation($input: BulkUpdatePurchaseItemsInput!) {
    bulkUpdatePurchaseItems(input: $input) {
      id
      revision
      categoryId
      productVariantId
      allocations {
        poolId
        weight
      }
      tags {
        id
        name
        revision
        archived
      }
    }
  }
`;

export const budgetsQuery = graphql`
  query BudgetsQuery($householdId: ID!, $period: String!) {
    budgets(householdId: $householdId, period: $period) {
      id
      householdId
      categoryId
      name
      amountYen
      actualYen
      remainingYen
      startsOn
      endsOn
      archived
      revision
    }
  }
`;

export const setBudgetMutation = graphql`
  mutation SetBudgetMutation($input: SetBudgetInput!) {
    setBudget(input: $input) {
      id
      categoryId
      name
      amountYen
      actualYen
      remainingYen
      startsOn
      endsOn
      archived
      revision
    }
  }
`;

export const archiveBudgetMutation = graphql`
  mutation ArchiveBudgetMutation($budgetId: ID!, $baseRevision: Int!, $archived: Boolean!) {
    archiveBudget(budgetId: $budgetId, baseRevision: $baseRevision, archived: $archived) {
      id
      archived
      revision
    }
  }
`;

export const recurringExpensesQuery = graphql`
  query RecurringExpensesQuery($householdId: ID!, $includeArchived: Boolean) {
    recurringExpenses(householdId: $householdId, includeArchived: $includeArchived) {
      id
      householdId
      name
      merchant
      amountYen
      currency
      categoryId
      paymentAccountId
      cadence
      startsOn
      nextDueOn
      endsOn
      archived
      revision
    }
  }
`;

export const setRecurringExpenseMutation = graphql`
  mutation SetRecurringExpenseMutation($input: SetRecurringExpenseInput!) {
    setRecurringExpense(input: $input) {
      id
      name
      merchant
      amountYen
      categoryId
      paymentAccountId
      cadence
      startsOn
      nextDueOn
      endsOn
      archived
      revision
    }
  }
`;

export const archiveRecurringExpenseMutation = graphql`
  mutation ArchiveRecurringExpenseMutation(
    $expenseId: ID!
    $baseRevision: Int!
    $archived: Boolean!
  ) {
    archiveRecurringExpense(
      expenseId: $expenseId
      baseRevision: $baseRevision
      archived: $archived
    ) {
      id
      archived
      revision
    }
  }
`;

export const accountMovementsQuery = graphql`
  query AccountMovementsQuery($householdId: ID!, $first: Int) {
    accountMovements(householdId: $householdId, first: $first) {
      id
      householdId
      kind
      occurredAt
      fromAccountId
      toAccountId
      amountYen
      currency
      description
      createdAt
    }
  }
`;

export const accountBalancesQuery = graphql`
  query AccountBalancesQuery($householdId: ID!) {
    accountBalances(householdId: $householdId) {
      accountId
      balanceYen
      currency
    }
  }
`;

export const accountBalanceAnalysisQuery = graphql`
  query AccountBalanceAnalysisQuery(
    $householdId: ID!
    $accountId: ID!
    $from: String!
    $to: String!
  ) {
    accountBalanceAnalysis(householdId: $householdId, accountId: $accountId, from: $from, to: $to) {
      accountId
      currency
      from
      to
      openingBalanceYen
      closingBalanceYen
      changes {
        occurredAt
        sourceKind
        sourceId
        description
        changeYen
        balanceYen
      }
    }
  }
`;

export const accountIncomesQuery = graphql`
  query AccountIncomesQuery($householdId: ID!, $first: Int) {
    accountIncomes(householdId: $householdId, first: $first) {
      id
      householdId
      accountId
      amountYen
      currency
      occurredAt
      description
      createdAt
    }
  }
`;

export const recordAccountMovementMutation = graphql`
  mutation RecordAccountMovementMutation($input: RecordAccountMovementInput!) {
    recordAccountMovement(input: $input) {
      id
      kind
      occurredAt
      fromAccountId
      toAccountId
      amountYen
      description
    }
  }
`;

export const recordExternalIncomeMutation = graphql`
  mutation RecordExternalIncomeMutation($input: RecordExternalIncomeInput!) {
    recordExternalIncome(input: $input) {
      id
      householdId
      accountId
      currency
      occurredAt
      amountYen
      description
    }
  }
`;

export function makeIdempotencyKey() {
  return crypto.randomUUID();
}
