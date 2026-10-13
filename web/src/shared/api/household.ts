import { commitMutation, fetchQuery } from "react-relay";
import {
  Environment,
  Network,
  Observable,
  RecordSource,
  Store,
  type GraphQLTaggedNode,
  type GraphQLResponse,
  type OperationType,
} from "relay-runtime";
import { useCallback, useRef, useState } from "react";

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
export type Allocation = { poolId: string; poolName: string; weight: number; amountYen?: number };
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
  | { id: string; __typename: "PurchaseCorrection" }
  | { id: string; __typename: "CorrectionLedgerEntry" }
  | { id: string; __typename: "FundingLedgerEntry" }
  | { id: string; __typename: "PurchaseLedgerEntry" }
  | { id: string; __typename: "TransferLedgerEntry" }
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
  allocations: Allocation[];
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
export type PurchasePayment = PurchasePaymentInput & { account?: Account | null };
export type PurchaseOrigin = "AI" | "MANUAL" | "UNKNOWN";
export type SourceKind = "RECEIPT" | "ORDER" | "EMAIL" | "CARD" | "BANK";
export type UnmatchedObservationState = "UNMATCHED" | "CONFIRMED";
export type UnmatchedObservation = {
  documentId: string;
  sourceReference: string;
  purchaseId?: string | null;
  sourceKind: SourceKind;
  state: UnmatchedObservationState;
  merchant: string;
  occurredAt?: string | null;
  amountYen: number;
  orderId: string;
  transactionId: string;
};
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
  categories: Category[];
  defaultAllocation: Allocation[];
};
export type Settlement = {
  period: string;
  status: "OPEN" | "CLOSED";
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
export type ProductSuggestion = {
  concept: ProductConcept;
  variant: ProductVariant;
  matchedName: string;
  score: number;
};
export type HouseholdInvitation = {
  id: string;
  householdId: string;
  status: "PENDING" | "ACCEPTED" | "REVOKED" | "EXPIRED";
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

export async function executeGraphql<T, V extends OperationType["variables"]>(
  operation: GraphQLTaggedNode,
  variables: V,
): Promise<T> {
  const request = operation as { params?: { operationKind?: string } };
  const operationName = (operation as { params?: { name?: string } }).params?.name;
  const requestVariables = normalizePurchaseVariables(operationName, variables);
  if (request.params?.operationKind === "mutation") {
    return new Promise<T>((resolve, reject) =>
      commitMutation(getRelayEnvironment(), {
        mutation: operation,
        variables: requestVariables,
        onCompleted: (response, errors) => {
          if (errors?.length) {
            reject(
              new ApiError(
                errors[0].message,
                (errors[0] as Error & { extensions?: { code?: string } }).extensions?.code,
              ),
            );
            return;
          }
          try {
            resolve(normalizeGraphQLValue(unwrapMutationResults(response)) as T);
          } catch (reason) {
            reject(reason);
          }
        },
        onError: reject,
      }),
    );
  }
  const result = await fetchQuery(getRelayEnvironment(), operation, requestVariables, {
    fetchPolicy: "network-only",
  }).toPromise();
  return normalizeGraphQLValue(result) as T;
}

export function useGraphqlMutation<T = unknown>(operation: GraphQLTaggedNode) {
  const name = (operation as { params?: { name?: string } }).params?.name;
  const inFlightRef = useRef(0);
  const [inFlightCount, setInFlightCount] = useState(0);
  const mutate = useCallback(
    (variables: Record<string, unknown>) =>
      new Promise<T>((resolve, reject) => {
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
          commitMutation(getRelayEnvironment(), {
            mutation: operation,
            variables: normalizePurchaseVariables(name, variables) as never,
            onCompleted: (response, errors) => {
              finish();
              if (errors?.length) {
                const error = errors[0] as Error & { extensions?: { code?: string } };
                reject(new ApiError(error.message, error.extensions?.code));
                return;
              }
              try {
                resolve(
                  normalizeGraphQLValue(
                    unwrapMutationResults(response as Record<string, unknown>),
                  ) as T,
                );
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
    [name, operation],
  );
  return { mutate, isInFlight: inFlightCount > 0 };
}

function normalizePurchaseVariables(
  name: string | undefined,
  variables: OperationType["variables"],
) {
  if (!variables || typeof variables !== "object") return variables;
  const value = variables as Record<string, unknown>;
  if (!("input" in value) || !value.input || typeof value.input !== "object") return variables;
  const input = value.input as Record<string, unknown>;
  if (name === "CreatePurchaseMutation" && Array.isArray(input.items)) {
    return {
      ...value,
      input: {
        ...input,
        items: input.items.map(normalizePurchaseItemInput),
      },
    };
  }
  if (name === "UpdatePurchaseItemMutation") {
    return { ...value, input: normalizePurchaseItemInput(input) };
  }
  return variables;
}

function normalizePurchaseItemInput(input: Record<string, unknown>) {
  const {
    quantityNumerator,
    quantityDenominator,
    quantityUnit,
    printedUnitPriceYen,
    printedUnitPriceQuantityNumerator,
    printedUnitPriceQuantityDenominator,
    printedUnitPriceUnit,
    ...rest
  } = input;
  const hasQuantityInput =
    "quantityNumerator" in input || "quantityDenominator" in input || "quantityUnit" in input;
  const quantity =
    quantityNumerator && quantityDenominator && quantityUnit
      ? { numerator: quantityNumerator, denominator: quantityDenominator, unit: quantityUnit }
      : null;
  const hasPrintedUnitPriceInput =
    "printedUnitPriceYen" in input ||
    "printedUnitPriceQuantityNumerator" in input ||
    "printedUnitPriceQuantityDenominator" in input ||
    "printedUnitPriceUnit" in input;
  const printedUnitPrice =
    printedUnitPriceYen != null &&
    printedUnitPriceQuantityNumerator &&
    printedUnitPriceQuantityDenominator &&
    printedUnitPriceUnit
      ? {
          amountYen: printedUnitPriceYen,
          basis: {
            numerator: printedUnitPriceQuantityNumerator,
            denominator: printedUnitPriceQuantityDenominator,
            unit: printedUnitPriceUnit,
          },
        }
      : null;
  return {
    ...rest,
    ...(hasQuantityInput ? { quantity } : {}),
    ...(hasPrintedUnitPriceInput ? { printedUnitPrice } : {}),
  };
}

function normalizeGraphQLValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(normalizeGraphQLValue);
  if (!value || typeof value !== "object") return value;
  const item = value as Record<string, unknown>;
  if ("quantity" in item || "printedUnitPrice" in item || "effectiveUnitPrice" in item) {
    const quantity = item.quantity as {
      numerator?: string;
      denominator?: string;
      unit?: string;
    } | null;
    const printed = item.printedUnitPrice as {
      amountYen: number;
      basis: { numerator: string; denominator: string; unit: string };
    } | null;
    const effective = item.effectiveUnitPrice as {
      numeratorYen: string;
      denominator: string;
    } | null;
    const normalized = {
      ...item,
      quantityNumerator: quantity?.numerator ?? null,
      quantityDenominator: quantity?.denominator ?? null,
      quantityUnit: quantity?.unit ?? null,
      printedUnitPriceYen: printed?.amountYen ?? null,
      printedUnitPriceQuantityNumerator: printed?.basis.numerator ?? null,
      printedUnitPriceQuantityDenominator: printed?.basis.denominator ?? null,
      printedUnitPriceUnit: printed?.basis.unit ?? null,
      effectiveUnitPriceNumeratorYen: effective?.numeratorYen ?? null,
      effectiveUnitPriceDenominator: effective?.denominator ?? null,
    };
    return Object.fromEntries(
      Object.entries(normalized).map(([key, nested]) => [key, normalizeGraphQLValue(nested)]),
    );
  }
  if (typeof item.__typename === "string" && item.__typename.endsWith("LedgerEntry")) {
    const typename = item.__typename;
    const base = Object.fromEntries(
      Object.entries(item).map(([key, nested]) => [key, normalizeGraphQLValue(nested)]),
    );
    if (typename === "FundingLedgerEntry") {
      const member = item.member as { id?: string } | undefined;
      const pool = item.pool as { id?: string } | undefined;
      return { ...base, kind: "FUNDING", fromMemberId: member?.id, poolId: pool?.id };
    }
    if (typename === "TransferLedgerEntry") {
      const from = item.fromMember as { id?: string } | undefined;
      const to = item.toMember as { id?: string } | undefined;
      const loan = item.loan as { id?: string } | null;
      return {
        ...base,
        kind: item.transferKind,
        fromMemberId: from?.id,
        toMemberId: to?.id,
        loanId: loan?.id,
      };
    }
    if (typename === "PurchaseLedgerEntry" || typename === "CorrectionLedgerEntry") {
      const purchase = item.purchase as { id?: string } | undefined;
      return {
        ...base,
        kind: typename === "PurchaseLedgerEntry" ? "PURCHASE" : "CORRECTION",
        sourcePurchaseId: purchase?.id,
      };
    }
  }
  return Object.fromEntries(
    Object.entries(item).map(([key, nested]) => [key, normalizeGraphQLValue(nested)]),
  );
}

function unwrapMutationResults(response: Record<string, unknown>) {
  return Object.fromEntries(
    Object.entries(response).map(([key, value]) => {
      if (!value || typeof value !== "object" || !("__typename" in value)) return [key, value];
      const result = value as Record<string, unknown>;
      if (typeof result.__typename === "string" && result.__typename.endsWith("UserError")) {
        throw new ApiError(String(result.message), String(result.code));
      }
      if (typeof result.__typename === "string" && result.__typename.endsWith("Success")) {
        if ("result" in result) return [key, result.result];
        if ("items" in result) return [key, result.items];
        if ("success" in result) return [key, result.success];
      }
      return [key, value];
    }),
  );
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

export function makeIdempotencyKey() {
  return crypto.randomUUID();
}
