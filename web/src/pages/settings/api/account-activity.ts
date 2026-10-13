import { fetchQuery } from "react-relay";
import {
  getRelayEnvironment,
  readGraphql,
  type AccountIncomesQuery,
  type AccountIncomesQuery$data,
  type AccountMovementsQuery,
  type AccountMovementsQuery$data,
} from "../../../shared/api";
import {
  accountIncomesQuery,
  accountMovementsQuery,
  recordAccountMovementMutation,
  recordExternalIncomeMutation,
} from "../../../entities/ledger";

type AccountMovementRecord =
  AccountMovementsQuery$data["accountMovements"]["edges"][number]["node"];
type AccountIncomeRecord = AccountIncomesQuery$data["accountIncomes"]["edges"][number]["node"];

export type AccountActivityMovement = {
  id: AccountMovementRecord["id"];
  householdId: AccountMovementRecord["householdId"];
  kind: AccountMovementRecord["kind"];
  occurredAt: AccountMovementRecord["occurredAt"];
  fromAccountId: AccountMovementRecord["fromAccountId"];
  fromAccount: Pick<AccountMovementRecord["fromAccount"], "id" | "name">;
  toAccountId: AccountMovementRecord["toAccountId"];
  toAccount: Pick<AccountMovementRecord["toAccount"], "id" | "name">;
  amountYen: AccountMovementRecord["amountYen"];
  currency: AccountMovementRecord["currency"];
  description: AccountMovementRecord["description"];
  createdAt: AccountMovementRecord["createdAt"];
};

export type AccountActivityIncome = AccountIncomeRecord;

type ActivityPage<T> = {
  edges: Array<{ cursor: string; node: T }>;
  pageInfo: { endCursor: string | null; hasNextPage: boolean };
};

type AccountMovementPage = AccountMovementsQuery$data["accountMovements"];
type AccountIncomePage = AccountIncomesQuery$data["accountIncomes"];

export async function readAccountActivity(householdId: string) {
  const [movementData, incomeData] = await Promise.all([
    fetchQuery<AccountMovementsQuery>(
      getRelayEnvironment(),
      accountMovementsQuery,
      { householdId, first: 50 },
      { fetchPolicy: "network-only" },
    ).toPromise(),
    fetchQuery<AccountIncomesQuery>(
      getRelayEnvironment(),
      accountIncomesQuery,
      { householdId, first: 50 },
      { fetchPolicy: "network-only" },
    ).toPromise(),
  ]);
  if (!movementData || !incomeData) throw new Error("口座の入出金履歴を取得できませんでした");
  return {
    movements: mapMovementPage(movementData.accountMovements),
    incomes: mapIncomePage(incomeData.accountIncomes),
  };
}

export async function readMoreAccountMovements(householdId: string, after: string) {
  const data = await fetchQuery<AccountMovementsQuery>(
    getRelayEnvironment(),
    accountMovementsQuery,
    { householdId, first: 50, after },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("口座移動履歴を取得できませんでした");
  return mapMovementPage(data.accountMovements);
}

export async function readMoreAccountIncomes(householdId: string, after: string) {
  const data = await fetchQuery<AccountIncomesQuery>(
    getRelayEnvironment(),
    accountIncomesQuery,
    { householdId, first: 50, after },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("外部入金履歴を取得できませんでした");
  return mapIncomePage(data.accountIncomes);
}

function mapMovementPage(page: AccountMovementPage): ActivityPage<AccountActivityMovement> {
  return {
    edges: page.edges.map(({ cursor, node }) => ({ cursor, node: mapMovement(node) })),
    pageInfo: {
      endCursor: page.pageInfo.endCursor ?? null,
      hasNextPage: page.pageInfo.hasNextPage,
    },
  };
}

function mapMovement(record: AccountMovementRecord): AccountActivityMovement {
  return {
    id: record.id,
    householdId: record.householdId,
    kind: record.kind,
    occurredAt: record.occurredAt,
    fromAccountId: record.fromAccountId,
    fromAccount: { id: record.fromAccount.id, name: record.fromAccount.name },
    toAccountId: record.toAccountId,
    toAccount: { id: record.toAccount.id, name: record.toAccount.name },
    amountYen: record.amountYen,
    currency: record.currency,
    description: record.description,
    createdAt: record.createdAt,
  };
}

function mapIncomePage(page: AccountIncomePage): ActivityPage<AccountActivityIncome> {
  return {
    edges: page.edges.map(({ cursor, node }) => ({ cursor, node: mapIncome(node) })),
    pageInfo: {
      endCursor: page.pageInfo.endCursor ?? null,
      hasNextPage: page.pageInfo.hasNextPage,
    },
  };
}

function mapIncome(record: AccountIncomeRecord): AccountActivityIncome {
  return {
    id: record.id,
    householdId: record.householdId,
    accountId: record.accountId,
    amountYen: record.amountYen,
    currency: record.currency,
    occurredAt: record.occurredAt,
    description: record.description,
    createdAt: record.createdAt,
  };
}

export function recordAccountActivityIncome(input: Record<string, unknown>) {
  return readGraphql(recordExternalIncomeMutation, { input });
}

export function recordAccountActivityMovement(input: Record<string, unknown>) {
  return readGraphql(recordAccountMovementMutation, { input });
}
