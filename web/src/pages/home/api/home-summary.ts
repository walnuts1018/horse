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
