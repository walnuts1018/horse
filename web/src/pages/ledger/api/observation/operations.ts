import { graphql } from "react-relay";

export const unmatchedObservationsQuery = graphql`
  query UnmatchedObservationsQuery(
    $householdId: ID!
    $from: YearMonth
    $to: YearMonth
    $sourceKind: SourceKind
    $state: UnmatchedObservationState
    $first: Int = 50
  ) {
    ...UnmatchedObservationsFragment
      @arguments(
        householdId: $householdId
        from: $from
        to: $to
        sourceKind: $sourceKind
        state: $state
        first: $first
      )
  }
`;

export const unmatchedObservationsFragment = graphql`
  fragment UnmatchedObservationsFragment on Query
  @argumentDefinitions(
    householdId: { type: "ID!" }
    from: { type: "YearMonth" }
    to: { type: "YearMonth" }
    sourceKind: { type: "SourceKind" }
    state: { type: "UnmatchedObservationState" }
    first: { type: "Int", defaultValue: 50 }
    after: { type: "String" }
  )
  @refetchable(queryName: "UnmatchedObservationsPaginationQuery") {
    unmatchedObservations(
      householdId: $householdId
      from: $from
      to: $to
      sourceKind: $sourceKind
      state: $state
      after: $after
      first: $first
    )
      @connection(
        key: "UnmatchedObservationsFragment_unmatchedObservations"
        filters: ["householdId", "from", "to", "sourceKind", "state"]
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
