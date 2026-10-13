import { graphql } from "react-relay";

export const monthlyMemberSpendingQuery = graphql`
  query MonthlyMemberSpendingQuery($householdId: ID!, $from: YearMonth!, $to: YearMonth!) {
    monthlyMemberSpending(householdId: $householdId, from: $from, to: $to) {
      items {
        period
        memberId
        memberName
        amountYen
      }
      hasUnattributedPurchases
    }
  }
`;
