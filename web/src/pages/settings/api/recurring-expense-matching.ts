import { fetchQuery } from "react-relay";

import { matchRecurringExpenseMutation } from "../../../entities/recurring-expense";
import {
  getRelayEnvironment,
  commitGraphQLMutationResult,
  type MatchRecurringExpenseMutation,
  type RecurringExpenseMatchCandidatesQuery,
  type RecurringExpenseMatchCandidatesQuery$data,
} from "../../../shared/api";
import { recurringExpenseMatchCandidatesQuery } from "./operations";

export type RecurringExpenseMatchCandidate =
  RecurringExpenseMatchCandidatesQuery$data["purchases"]["edges"][number]["node"];

export async function readRecurringExpenseMatchCandidates(
  householdId: string,
  merchant: string,
): Promise<RecurringExpenseMatchCandidate[]> {
  const result: RecurringExpenseMatchCandidatesQuery$data | undefined =
    await fetchQuery<RecurringExpenseMatchCandidatesQuery>(
      getRelayEnvironment(),
      recurringExpenseMatchCandidatesQuery,
      {
        filter: { householdId, ...(merchant ? { search: merchant } : {}) },
        first: 100,
      },
      { fetchPolicy: "network-only" },
    ).toPromise();
  if (!result) throw new Error("購入履歴を取得できませんでした");
  return result.purchases.edges.map(({ node }) => node);
}

export function matchRecurringExpense(input: MatchRecurringExpenseMutation["variables"]["input"]) {
  return commitGraphQLMutationResult<
    MatchRecurringExpenseMutation,
    NonNullable<MatchRecurringExpenseMutation["response"]["matchRecurringExpense"]["result"]>
  >(
    matchRecurringExpenseMutation,
    { input },
    (response) => response.matchRecurringExpense,
    "MatchRecurringExpenseSuccess",
    "定期支出の実績を照合できませんでした",
  );
}
