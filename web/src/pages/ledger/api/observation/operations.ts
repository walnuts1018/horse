import { graphql } from "react-relay";

export const unmatchedObservationsQuery = graphql`
  query UnmatchedObservationsQuery(
    $householdId: ID!
    $from: YearMonth
    $to: YearMonth
    $sourceKind: SourceKind
    $state: UnmatchedObservationState
    $after: String
    $first: Int
  ) {
    unmatchedObservations(
      householdId: $householdId
      from: $from
      to: $to
      sourceKind: $sourceKind
      state: $state
      after: $after
      first: $first
    ) {
      edges {
        cursor
        node {
          __typename
          documentId
          sourceReference
          sourceKind
          merchant
          occurredAt
          amountYen
          orderId
          transactionId
          ... on ConfirmedObservation {
            purchase {
              id
              merchant
              occurredAt
            }
          }
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
      totalCount
    }
  }
`;
