import { readGraphql, type Connection, type RecurringExpense } from "../../../shared/api";
import { recurringExpensesQuery } from "./operations";

type RecurringExpensesPage = {
  recurringExpenses: Connection<RecurringExpense>;
};

export async function readRecurringExpenses(
  householdId: string,
  includeArchived = false,
): Promise<RecurringExpense[]> {
  const expenses: RecurringExpense[] = [];
  let after: string | null = null;
  for (;;) {
    const result: RecurringExpensesPage = await readGraphql<RecurringExpensesPage>(
      recurringExpensesQuery,
      {
        householdId,
        includeArchived,
        first: 100,
        after,
      },
    );
    expenses.push(...result.recurringExpenses.edges.map((edge) => edge.node));
    const pageInfo: { endCursor?: string | null; hasNextPage: boolean } =
      result.recurringExpenses.pageInfo;
    if (!pageInfo.hasNextPage || !pageInfo.endCursor || pageInfo.endCursor === after)
      return expenses;
    after = pageInfo.endCursor;
  }
}
