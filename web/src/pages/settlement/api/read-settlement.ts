import { fetchQuery } from "react-relay";
import {
  getRelayEnvironment,
  type LoanBalance,
  type LoanBalancesQuery,
  type LoanBalancesQuery$data,
  type Settlement,
  type SettlementQuery,
  type SettlementQuery$data,
} from "../../../shared/api";
import { loanBalancesQuery } from "../../../entities/ledger";
import { settlementQuery } from "../../../entities/settlement";

type SettlementRecord = SettlementQuery$data["settlement"];
type LoanBalanceRecord = LoanBalancesQuery$data["loanBalances"]["edges"][number]["node"];

export type LoanBalancePage = {
  loans: LoanBalance[];
  endCursor: string | null;
  hasNextPage: boolean;
};

export async function readSettlement(householdId: string, period: string): Promise<Settlement> {
  const data = await fetchQuery<SettlementQuery>(
    getRelayEnvironment(),
    settlementQuery,
    { householdId, period },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("精算情報を取得できませんでした");
  return mapSettlement(data.settlement);
}

export async function readLoanBalances(
  householdId: string,
  { after, first = 50 }: { after?: string | null; first?: number } = {},
): Promise<LoanBalancePage> {
  const data: LoanBalancesQuery$data | undefined = await fetchQuery<LoanBalancesQuery>(
    getRelayEnvironment(),
    loanBalancesQuery,
    { householdId, after, first },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("貸付残高を取得できませんでした");
  return {
    loans: data.loanBalances.edges.map(({ node }) => mapLoanBalance(node)),
    endCursor: data.loanBalances.pageInfo.endCursor ?? null,
    hasNextPage: data.loanBalances.pageInfo.hasNextPage,
  };
}

function mapSettlement(record: SettlementRecord): Settlement {
  const fields = {
    id: record.id,
    period: record.period,
    revision: record.revision,
    lines: record.lines.map((line) => ({
      memberId: line.memberId,
      memberName: line.memberName,
      scheduledYen: Number(line.scheduledYen),
      paidYen: Number(line.paidYen),
      carryYen: Number(line.carryYen),
      priorBalanceYen: Number(line.priorBalanceYen),
      monthlyAdjustmentYen: Number(line.monthlyAdjustmentYen),
      advanceCreditYen: Number(line.advanceCreditYen),
      amountDueYen: Number(line.amountDueYen),
    })),
  };
  switch (record.__typename) {
    case "OpenSettlement":
      return { ...fields, __typename: "OpenSettlement" };
    case "ClosedSettlement":
      if (!record.createdAt) throw new Error("精算情報の確定日時を取得できませんでした");
      return { ...fields, __typename: "ClosedSettlement", createdAt: record.createdAt };
    default:
      throw new Error(`未対応の精算型です: ${record.__typename}`);
  }
}

function mapLoanBalance(record: LoanBalanceRecord): LoanBalance {
  return {
    id: record.id,
    lenderMemberId: record.lenderMemberId,
    borrowerMemberId: record.borrowerMemberId,
    principalYen: Number(record.principalYen),
    repaidYen: Number(record.repaidYen),
    balanceYen: Number(record.balanceYen),
  };
}
