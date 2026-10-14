import { fetchQuery } from "react-relay";

import {
  getRelayEnvironment,
  type Budget,
  type BudgetsQuery,
  type BudgetsQuery$data,
} from "../../../shared/api";
import { budgetsQuery } from "./budget-operations";

type BudgetRecord = BudgetsQuery$data["budgets"]["edges"][number]["node"];

export async function readBudgets(householdId: string, period: string): Promise<Budget[]> {
  const budgets: Budget[] = [];
  let after: string | null = null;
  for (;;) {
    const result: BudgetsQuery$data | undefined = await fetchQuery<BudgetsQuery>(
      getRelayEnvironment(),
      budgetsQuery,
      { householdId, period, first: 100, after },
      { fetchPolicy: "network-only" },
    ).toPromise();
    if (!result) throw new Error("予算を取得できませんでした");
    budgets.push(...result.budgets.edges.map(({ node }) => mapBudget(node)));
    const endCursor: string | null | undefined = result.budgets.pageInfo.endCursor;
    const hasNextPage = result.budgets.pageInfo.hasNextPage;
    if (!hasNextPage || !endCursor || endCursor === after) return budgets;
    after = endCursor;
  }
}

function mapBudget(record: BudgetRecord): Budget {
  return {
    id: record.id,
    householdId: record.householdId,
    categoryId: record.categoryId,
    name: record.name,
    amountYen: Number(record.amountYen),
    actualYen: Number(record.actualYen),
    remainingYen: Number(record.remainingYen),
    startsOn: record.startsOn,
    endsOn: record.endsOn,
    archived: record.archived,
    revision: record.revision,
  };
}
