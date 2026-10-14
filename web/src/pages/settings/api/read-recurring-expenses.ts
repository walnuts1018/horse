import { fetchQuery } from "react-relay";

import {
  getRelayEnvironment,
  type RecurringExpense,
  type RecurringExpensesQuery,
  type RecurringExpensesQuery$data,
} from "../../../shared/api";
import { recurringExpensesQuery } from "./recurring-expense-operations";

type RecurringExpenseRecord =
  RecurringExpensesQuery$data["recurringExpenses"]["edges"][number]["node"];

export async function readRecurringExpenses(
  householdId: string,
  includeArchived = false,
): Promise<RecurringExpense[]> {
  const expenses: RecurringExpense[] = [];
  let after: string | null = null;
  for (;;) {
    const result: RecurringExpensesQuery$data | undefined =
      await fetchQuery<RecurringExpensesQuery>(
        getRelayEnvironment(),
        recurringExpensesQuery,
        { householdId, includeArchived, first: 100, after },
        { fetchPolicy: "network-only" },
      ).toPromise();
    if (!result) throw new Error("定期支出を取得できませんでした");
    expenses.push(...result.recurringExpenses.edges.map(({ node }) => mapRecurringExpense(node)));
    const pageInfo: RecurringExpensesQuery$data["recurringExpenses"]["pageInfo"] =
      result.recurringExpenses.pageInfo;
    const endCursor: string | null | undefined = pageInfo.endCursor;
    if (!pageInfo.hasNextPage || !endCursor || endCursor === after) return expenses;
    after = endCursor;
  }
}

function mapRecurringExpense(record: RecurringExpenseRecord): RecurringExpense {
  return {
    id: record.id,
    householdId: record.householdId,
    name: record.name,
    merchant: record.merchant,
    amountYen: Number(record.amountYen),
    currency: record.currency,
    categoryId: record.categoryId,
    paymentAccountId: record.paymentAccountId,
    cadence: mapCadence(record.cadence),
    startsOn: record.startsOn,
    nextDueOn: record.nextDueOn,
    endsOn: record.endsOn,
    archived: record.archived,
    revision: record.revision,
  };
}

function mapCadence(cadence: RecurringExpenseRecord["cadence"]): RecurringExpense["cadence"] {
  switch (cadence) {
    case "WEEKLY":
    case "MONTHLY":
    case "YEARLY":
      return cadence;
    case "%future added value":
    default:
      throw new Error(`未対応の定期支出周期です: ${cadence}`);
  }
}
