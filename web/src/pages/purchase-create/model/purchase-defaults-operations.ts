import { graphql } from "react-relay";

export const purchaseDefaultsQuery = graphql`
  query PurchaseDefaultsQuery($householdId: ID!) {
    purchaseDefaults(householdId: $householdId) {
      merchant
      paymentAccountId
      purchaseCount
      lastUsedAt
    }
  }
`;
