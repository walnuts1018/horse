import { graphql } from "react-relay";

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
        amountDueYen
      }
    }
  }
`;
