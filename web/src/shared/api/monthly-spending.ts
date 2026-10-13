import { graphql } from "react-relay";

export const monthlyCategorySpendingQuery = graphql`
  query MonthlyCategorySpendingQuery($householdId: ID!, $from: YearMonth!, $to: YearMonth!) {
    monthlyCategorySpending(householdId: $householdId, from: $from, to: $to) {
      period
      categoryId
      categoryName
      amountYen
    }
  }
`;
