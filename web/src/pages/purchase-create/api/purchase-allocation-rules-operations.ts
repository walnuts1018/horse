import { graphql } from "react-relay";

export const purchaseAllocationRulesQuery = graphql`
  query PurchaseAllocationRulesQuery($householdId: ID!, $after: String, $first: Int) {
    allocationRules(householdId: $householdId, after: $after, first: $first) {
      edges {
        node {
          id
          target {
            __typename
            name
            ... on Node {
              id
            }
          }
          allocations {
            poolId
            weight
          }
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
    }
  }
`;
