import { fetchQuery } from "react-relay";

import { loanBalancesQuery } from "../../../entities/ledger";
import {
  getRelayEnvironment,
  type LoanBalance,
  type LoanBalancesQuery,
  type LoanBalancesQuery$data,
  type ClosedSettlement,
  type OpenSettlement,
  type Settlement,
  type SettlementQuery,
  type SettlementQuery$data,
} from "../../../shared/api";
import { settlementQuery } from "./settlement-operations";

type SettlementRecord = SettlementQuery$data["settlement"];
type LoanBalanceRecord = LoanBalancesQuery$data["loanBalances"]["edges"][number]["node"];

export type SettlementEvidence = {
  kind: string;
  label: string;
  amountYen: number;
  sourceId: string;
  purchaseId: string | null;
  occurredAt: string | null;
};

type SettlementLineWithEvidence = Settlement["lines"][number] & { evidence: SettlementEvidence[] };
export type SettlementWithEvidence =
  | (Omit<OpenSettlement, "lines"> & { lines: SettlementLineWithEvidence[] })
  | (Omit<ClosedSettlement, "lines"> & { lines: SettlementLineWithEvidence[] });

export type LoanBalancePage = {
  loans: LoanBalance[];
  endCursor: string | null;
  hasNextPage: boolean;
};

export async function readSettlement(
  householdId: string,
  period: string,
): Promise<SettlementWithEvidence> {
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

function mapSettlement(record: SettlementRecord): SettlementWithEvidence {
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
      evidence: line.evidence.map((item) => ({
        kind: item.kind,
        label: item.label,
        amountYen: Number(item.amountYen),
        sourceId: item.sourceId,
        purchaseId: item.purchaseId ?? null,
        occurredAt: item.occurredAt ?? null,
      })),
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
