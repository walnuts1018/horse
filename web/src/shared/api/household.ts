import { useCallback, useRef, useState } from "react";
import { commitMutation } from "react-relay";
import {
  Environment,
  Network,
  Observable,
  RecordSource,
  Store,
  type GraphQLTaggedNode,
  type GraphQLResponse,
  type MutationParameters,
} from "relay-runtime";

export type {
  BulkUpdatePurchaseItemsMutation,
  BulkUpdatePurchaseItemsMutation$data,
} from "./__generated__/BulkUpdatePurchaseItemsMutation.graphql";
export type {
  BulkUpdatePurchasesMutation,
  BulkUpdatePurchasesMutation$data,
} from "./__generated__/BulkUpdatePurchasesMutation.graphql";
export type {
  CorrectPurchaseMutation,
  CorrectPurchaseMutation$data,
} from "./__generated__/CorrectPurchaseMutation.graphql";
export type {
  CreateAndPostPurchaseMutation,
  CreateAndPostPurchaseMutation$data,
  CreateAndPostPurchaseMutation$variables,
} from "./__generated__/CreateAndPostPurchaseMutation.graphql";
export type {
  CreatePurchaseMutation,
  CreatePurchaseMutation$data,
} from "./__generated__/CreatePurchaseMutation.graphql";
export type {
  PostPurchaseMutation,
  PostPurchaseMutation$data,
} from "./__generated__/PostPurchaseMutation.graphql";
export type {
  UpdatePurchaseMutation,
  UpdatePurchaseMutation$data,
} from "./__generated__/UpdatePurchaseMutation.graphql";
export type {
  UpdatePurchaseItemMutation,
  UpdatePurchaseItemMutation$data,
  UpdatePurchaseItemMutation$variables,
} from "./__generated__/UpdatePurchaseItemMutation.graphql";

export type SharedPoolSettlementPolicy = "BALANCE_TO_STANDARD" | "REIMBURSE_PERSONAL";
export type PoolFundingShare = { memberId: string; memberName: string; weight: number };
export type PersonalPool = {
  __typename: "PersonalPool";
  id: string;
  name: string;
  defaultShareWeight: number;
  ownerMemberId: string;
  fundingShares: PoolFundingShare[];
  fundingRuleVersion: number;
};
export type SharedPool = {
  __typename: "SharedPool";
  id: string;
  name: string;
  defaultShareWeight: number;
  fundingShares: PoolFundingShare[];
  settlementPolicy: SharedPoolSettlementPolicy;
  fundingRuleVersion: number;
};
export type UnconfiguredPersonalPool = {
  __typename: "UnconfiguredPersonalPool";
  id: string;
  name: string;
  defaultShareWeight: number;
  fundingShares: PoolFundingShare[];
  fundingRuleVersion: number;
};
export type UnconfiguredSharedPool = {
  __typename: "UnconfiguredSharedPool";
  id: string;
  name: string;
  defaultShareWeight: number;
  fundingShares: PoolFundingShare[];
  fundingRuleVersion: number;
};
export type Pool = PersonalPool | SharedPool | UnconfiguredPersonalPool | UnconfiguredSharedPool;

export function isPersonalPool(pool: Pool): pool is PersonalPool | UnconfiguredPersonalPool {
  return pool.__typename === "PersonalPool" || pool.__typename === "UnconfiguredPersonalPool";
}

export function isSharedPool(pool: Pool): pool is SharedPool | UnconfiguredSharedPool {
  return pool.__typename === "SharedPool" || pool.__typename === "UnconfiguredSharedPool";
}
export type Account = { id: string; name: string; kind: string; poolId?: string | null };
export type Category = { id: string; name: string; parentId?: string | null; archived?: boolean };
export type Allocation = { poolId: string; poolName: string; weight: number };
export type PurchaseItemAllocation = Allocation & { amountYen: number };
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
  cadence: "WEEKLY" | "MONTHLY" | "YEARLY";
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
  fromAccount: Account;
  toAccountId: string;
  toAccount: Account;
  amountYen: number;
  currency: string;
  description: string;
  createdAt: string;
};
export type AccountBalance = { accountId: string; balanceYen: number; currency: string };
export type AccountBalanceChangeSource =
  | { id: string; __typename: "AccountOpeningBalance"; account: { id: string; name: string } }
  | { id: string; __typename: "Purchase" }
  | { id: string; __typename: "PurchaseWideCorrection" }
  | { id: string; __typename: "PurchaseItemCorrection" }
  | { id: string; __typename: "CorrectionLedgerEntry" }
  | { id: string; __typename: "FundingLedgerEntry" }
  | { id: string; __typename: "PurchaseLedgerEntry" }
  | { id: string; __typename: "AdvanceTransferLedgerEntry" }
  | { id: string; __typename: "LoanTransferLedgerEntry" }
  | { id: string; __typename: "RepaymentTransferLedgerEntry" }
  | { id: string; __typename: "GiftTransferLedgerEntry" }
  | { id: string; __typename: "AccountMovement" }
  | { id: string; __typename: "AccountIncome" };
export type AccountBalanceChange = {
  occurredAt: string;
  source: AccountBalanceChangeSource;
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
  changes: Connection<AccountBalanceChange>;
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
  quantityNumerator?: string | null;
  quantityDenominator?: string | null;
  quantityUnit?: string | null;
  printedUnitPriceYen?: number | null;
  printedUnitPriceQuantityNumerator?: string | null;
  printedUnitPriceQuantityDenominator?: string | null;
  printedUnitPriceUnit?: string | null;
  effectiveUnitPriceNumeratorYen?: string | null;
  effectiveUnitPriceDenominator?: string | null;
  categoryId?: string | null;
  productVariantId?: string | null;
  allocations: PurchaseItemAllocation[];
  tags: Tag[];
};
export type Purchase = {
  id: string;
  householdId: string;
  occurredAt: string;
  merchant: string;
  origin: "AI" | "MANUAL" | "UNKNOWN";
  hasEvidence: boolean;
  sourceKinds: string[];
  paymentAllocations: PurchasePayment[];
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
  origin?: "AI" | "MANUAL" | "UNKNOWN";
  sourceDocumentId?: string | null;
  sourceReference?: string | null;
  receiptCandidateId?: string | null;
  manualAmountOverrideReason?: string | null;
  paymentAllocations?: PurchasePaymentInput[];
  items: Array<{
    name: string;
    amountYen: number;
    quantityNumerator?: string | null;
    quantityDenominator?: string | null;
    quantityUnit?: string | null;
    printedUnitPriceYen?: number | null;
    printedUnitPriceQuantityNumerator?: string | null;
    printedUnitPriceQuantityDenominator?: string | null;
    printedUnitPriceUnit?: string | null;
    categoryId?: string | null;
    productVariantId?: string | null;
    allocations?: Array<{ poolId: string; weight: number }> | null;
  }>;
};
export type PurchasePaymentInput = { paymentAccountId: string; amountYen: number };
export type PurchasePayment = PurchasePaymentInput & {
  account?: Pick<Account, "id" | "name"> | null;
};
export type PurchaseOrigin = "AI" | "MANUAL" | "UNKNOWN";
export type SourceKind = "RECEIPT" | "ORDER" | "EMAIL" | "CARD" | "BANK";
export type UnmatchedObservationState = "UNMATCHED" | "CONFIRMED";
type ObservationFields = {
  documentId: string;
  sourceReference: string;
  sourceKind: SourceKind;
  merchant: string;
  occurredAt?: string | null;
  amountYen: number;
  orderId: string;
  transactionId: string;
};
export type UnmatchedObservation =
  | (ObservationFields & { __typename: "UnmatchedObservation" })
  | (ObservationFields & {
      __typename: "ConfirmedObservation";
      purchase: Pick<Purchase, "id" | "merchant" | "occurredAt">;
    });
type PurchaseCorrectionFields = {
  id: string;
  amountDeltaYen: number;
  reason: string;
  occurredAt: string;
  settlementMonth: string;
  paymentAllocations: PurchasePayment[];
};
export type PurchaseWideCorrection = PurchaseCorrectionFields & {
  __typename: "PurchaseWideCorrection";
};
export type PurchaseItemCorrection = PurchaseCorrectionFields & {
  __typename: "PurchaseItemCorrection";
  itemId: string;
};
export type PurchaseCorrection = PurchaseWideCorrection | PurchaseItemCorrection;
export type LoanBalance = {
  id: string;
  lenderMemberId: string;
  borrowerMemberId: string;
  principalYen: number;
  repaidYen: number;
  balanceYen: number;
};
export type Member = { id: string; displayName: string; role: "OWNER" | "MEMBER" };
export type Household = {
  id: string;
  name: string;
  revision: number;
  members: Member[];
  pools: Pool[];
  accounts: Account[];
  categories: Array<Category & { archived: boolean }>;
  defaultAllocation: Allocation[];
};
type SettlementLine = {
  memberId: string;
  memberName: string;
  scheduledYen: number;
  paidYen: number;
  carryYen: number;
  priorBalanceYen: number;
  monthlyAdjustmentYen: number;
  advanceCreditYen: number;
  amountDueYen: number;
};
type SettlementFields = {
  id: string;
  period: string;
  revision: number;
  lines: SettlementLine[];
};
export type OpenSettlement = SettlementFields & { __typename: "OpenSettlement" };
export type ClosedSettlement = SettlementFields & {
  __typename: "ClosedSettlement";
  createdAt: string;
};
export type Settlement = OpenSettlement | ClosedSettlement;
export type AllocationRule = {
  id: string;
  householdId: string;
  target: {
    __typename: "Category" | "ProductConcept" | "ProductVariant" | "MerchantAllocationRuleTarget";
    name: string;
  };
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
  brand?: string | null;
  packageSize?: string | null;
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
type HouseholdInvitationFields = {
  id: string;
  householdId: string;
  expiresAt: string;
  createdAt: string;
};
export type HouseholdInvitation = HouseholdInvitationFields &
  (
    | { __typename: "PendingHouseholdInvitation" }
    | { __typename: "AcceptedHouseholdInvitation" }
    | { __typename: "RevokedHouseholdInvitation" }
    | { __typename: "ExpiredHouseholdInvitation" }
  );
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

export class MutationUserError extends ApiError {}

let environment: Environment | undefined;

export function getRelayEnvironment() {
  if (environment) return environment;
  const network = Network.create(
    async (operation, variables) => {
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
    },
    (operation, variables) =>
      Observable.create<GraphQLResponse>((sink) => {
        const controller = new AbortController();
        let stopped = false;
        let after = typeof variables.after === "string" ? variables.after : undefined;

        const run = async () => {
          let retryDelay = 1000;
          while (!stopped) {
            try {
              const response = await fetch("/graphql", {
                method: "POST",
                credentials: "include",
                headers: {
                  accept: "text/event-stream",
                  "content-type": "application/json",
                },
                body: JSON.stringify({
                  query: operation.text,
                  variables: after === undefined ? variables : { ...variables, after },
                }),
                signal: controller.signal,
              });
              if (!response.ok)
                throw new ApiError(
                  `GraphQL API returned ${response.status}`,
                  undefined,
                  response.status,
                );
              if (!response.body) throw new ApiError("GraphQL subscription stream is unavailable");

              const reader = response.body.getReader();
              let buffer = "";
              let completed = false;
              while (!stopped) {
                const { done, value } = await reader.read();
                if (done) break;
                buffer += new TextDecoder().decode(value, { stream: true });
                while (true) {
                  const boundary = /\r?\n\r?\n/.exec(buffer);
                  if (!boundary || boundary.index === undefined) break;
                  const frame = buffer.slice(0, boundary.index).replace(/\r\n/g, "\n");
                  buffer = buffer.slice(boundary.index + boundary[0].length);
                  let eventName = "message";
                  const data: string[] = [];
                  for (const line of frame.split("\n")) {
                    if (line.startsWith("event:")) eventName = line.slice(6).trim();
                    if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
                  }
                  if (eventName === "complete") {
                    completed = true;
                    break;
                  }
                  if (eventName !== "next" || data.length === 0) continue;
                  const result = JSON.parse(data.join("\n")) as GraphQLResponse & {
                    errors?: Array<{ message: string; extensions?: { code?: string } }>;
                    data?: Record<string, { cursor?: unknown }>;
                  };
                  if (result.errors?.length)
                    throw new ApiError(result.errors[0].message, result.errors[0].extensions?.code);
                  const cursor = Object.values(result.data ?? {})[0]?.cursor;
                  if (typeof cursor === "string") after = cursor;
                  sink.next(result);
                  retryDelay = 1000;
                }
                if (completed) break;
              }
              await reader.cancel().catch(() => undefined);
              if (stopped) return;
            } catch (error) {
              if (stopped || controller.signal.aborted) return;
              if (error instanceof ApiError) {
                sink.error(error);
                return;
              }
            }

            await new Promise((resolve) => setTimeout(resolve, retryDelay));
            retryDelay = Math.min(retryDelay * 2, 10_000);
          }
        };

        void run();
        return () => {
          stopped = true;
          controller.abort();
        };
      }),
  );
  const relayEnvironment = new Environment({ network, store: new Store(new RecordSource()) });
  environment = relayEnvironment;
  return relayEnvironment;
}

export function useTypedGraphqlMutation<TOperation extends MutationParameters, TResult>(
  operation: GraphQLTaggedNode,
  readResult: (response: TOperation["response"]) => TResult,
) {
  const inFlightRef = useRef(0);
  const [inFlightCount, setInFlightCount] = useState(0);
  const mutate = useCallback(
    (variables: TOperation["variables"]) =>
      new Promise<TResult>((resolve, reject) => {
        inFlightRef.current += 1;
        setInFlightCount(inFlightRef.current);
        let finished = false;
        const finish = () => {
          if (finished) return;
          finished = true;
          inFlightRef.current -= 1;
          setInFlightCount(inFlightRef.current);
        };
        try {
          commitMutation<TOperation>(getRelayEnvironment(), {
            mutation: operation,
            variables,
            onCompleted: (response, errors) => {
              finish();
              if (errors?.length) {
                reject(new ApiError(errors[0].message));
                return;
              }
              try {
                resolve(readResult(response));
              } catch (reason) {
                reject(reason);
              }
            },
            onError: (reason) => {
              finish();
              reject(reason);
            },
          });
        } catch (reason) {
          finish();
          reject(reason);
        }
      }),
    [operation, readResult],
  );
  return { mutate, isInFlight: inFlightCount > 0 };
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

export function makeIdempotencyKey() {
  return crypto.randomUUID();
}
