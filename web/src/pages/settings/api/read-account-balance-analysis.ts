import { fetchQuery } from "react-relay";

import {
  getRelayEnvironment,
  type AccountBalanceAnalysis,
  type AccountBalanceAnalysisQuery,
  type AccountBalanceAnalysisQuery$data,
} from "../../../shared/api";
import { accountBalanceAnalysisQuery } from "./account-balance-analysis";

type AccountBalanceAnalysisRecord = AccountBalanceAnalysisQuery$data["accountBalanceAnalysis"];
type BalanceChangeRecord = AccountBalanceAnalysisRecord["changes"]["edges"][number]["node"];
type BalanceChangeSourceRecord = BalanceChangeRecord["source"];

export async function readAccountBalanceAnalysis(variables: {
  householdId: string;
  accountId: string;
  from: string;
  to: string;
  first: number;
  after?: string;
}): Promise<AccountBalanceAnalysis> {
  const data: AccountBalanceAnalysisQuery$data | undefined =
    await fetchQuery<AccountBalanceAnalysisQuery>(
      getRelayEnvironment(),
      accountBalanceAnalysisQuery,
      variables,
      { fetchPolicy: "network-only" },
    ).toPromise();
  if (!data) throw new Error("口座残高の増減を取得できませんでした");
  return mapAccountBalanceAnalysis(data.accountBalanceAnalysis);
}

function mapAccountBalanceAnalysis(record: AccountBalanceAnalysisRecord): AccountBalanceAnalysis {
  return {
    accountId: record.accountId,
    currency: record.currency,
    from: record.from,
    to: record.to,
    openingBalanceYen: Number(record.openingBalanceYen),
    closingBalanceYen: Number(record.closingBalanceYen),
    changes: {
      edges: record.changes.edges.map(({ cursor, node }) => ({
        cursor,
        node: {
          occurredAt: node.occurredAt,
          source: mapBalanceChangeSource(node.source),
          description: node.description,
          changeYen: Number(node.changeYen),
          balanceYen: Number(node.balanceYen),
        },
      })),
      pageInfo: {
        endCursor: record.changes.pageInfo.endCursor ?? null,
        hasNextPage: record.changes.pageInfo.hasNextPage,
      },
      totalCount: record.changes.totalCount,
    },
  };
}

function mapBalanceChangeSource(
  source: BalanceChangeSourceRecord,
): AccountBalanceAnalysis["changes"]["edges"][number]["node"]["source"] {
  switch (source.__typename) {
    case "AccountOpeningBalance":
      return {
        __typename: source.__typename,
        id: source.id,
        account: { id: source.account.id, name: source.account.name },
      };
    case "Purchase":
    case "PurchaseWideCorrection":
    case "PurchaseItemCorrection":
    case "CorrectionLedgerEntry":
    case "FundingLedgerEntry":
    case "PurchaseLedgerEntry":
    case "AdvanceTransferLedgerEntry":
    case "LoanTransferLedgerEntry":
    case "RepaymentTransferLedgerEntry":
    case "GiftTransferLedgerEntry":
    case "AccountMovement":
    case "AccountIncome":
      return { __typename: source.__typename, id: source.id };
    case "%other":
    default:
      throw new Error(`未対応の残高変動元です: ${source.__typename}`);
  }
}
