import { readGraphql, type Budget, type Connection } from "../../../shared/api";
import { budgetsQuery } from "./operations";

type BudgetsPage = {
  budgets: Connection<Budget>;
};

export async function readBudgets(householdId: string, period: string): Promise<Budget[]> {
  const budgets: Budget[] = [];
  let after: string | null = null;
  for (;;) {
    const result: BudgetsPage = await readGraphql<BudgetsPage>(budgetsQuery, {
      householdId,
      period,
      first: 100,
      after,
    });
    budgets.push(...result.budgets.edges.map((edge) => edge.node));
    const endCursor: string | null | undefined = result.budgets.pageInfo.endCursor;
    const hasNextPage: boolean = result.budgets.pageInfo.hasNextPage;
    if (!hasNextPage || !endCursor || endCursor === after) return budgets;
    after = endCursor;
  }
}
