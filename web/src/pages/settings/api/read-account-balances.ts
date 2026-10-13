import { fetchQuery } from "react-relay";

import { accountBalancesQuery } from "../../../entities/ledger";
import {
  getRelayEnvironment,
  type AccountBalance,
  type AccountBalancesQuery,
  type AccountBalancesQuery$data,
} from "../../../shared/api";

export async function readAccountBalances(householdId: string): Promise<AccountBalance[]> {
  const data: AccountBalancesQuery$data | undefined = await fetchQuery<AccountBalancesQuery>(
    getRelayEnvironment(),
    accountBalancesQuery,
    { householdId },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("口座残高を取得できませんでした");
  return data.accountBalances.map((balance) => ({
    accountId: balance.accountId,
    balanceYen: Number(balance.balanceYen),
    currency: balance.currency,
  }));
}
