import { graphql } from "react-relay";

export function summarizeSettlementDues(lines: Array<{ amountDueYen: number }>) {
  return lines.reduce(
    (totals, line) => {
      if (line.amountDueYen > 0) totals.fundingDueYen += line.amountDueYen;
      if (line.amountDueYen < 0) totals.creditYen -= line.amountDueYen;
      return totals;
    },
    { fundingDueYen: 0, creditYen: 0 },
  );
}

export const homeSummaryQuery = graphql`
  query HomeSummaryQuery($householdId: ID!) {
    homeSummary(householdId: $householdId) {
      dailySpending {
        date
        amountYen
      }
      monthlyItemCount
    }
  }
`;

export const homeDashboardQuery = graphql`
  query HomeDashboardQuery(
    $householdId: ID!
    $from: YearMonth!
    $to: YearMonth!
    $period: YearMonth!
  ) {
    homeSummary(householdId: $householdId) {
      dailySpending {
        date
        amountYen
      }
      monthlyItemCount
    }
    monthlyCategorySpending(householdId: $householdId, from: $from, to: $to) {
      period
      amountYen
    }
    settlement(householdId: $householdId, period: $period) {
      __typename
      lines {
        memberId
        memberName
        amountDueYen
      }
    }
  }
`;
