import { graphql } from "react-relay";

export const statementRefundCorrectionsQuery = graphql`
  query StatementRefundCorrectionsQuery(
    $householdId: ID!
    $sourceDocumentId: ID!
    $sourceReference: String!
    $first: Int
    $after: String
  ) {
    statementRefundCorrections(
      householdId: $householdId
      sourceDocumentId: $sourceDocumentId
      sourceReference: $sourceReference
      first: $first
      after: $after
    ) {
      edges {
        cursor
        node {
          id
          amountYen
          occurredAt
          merchant
          sourceFilename
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
    }
  }
`;

export const linkStatementRefundCorrectionMutation = graphql`
  mutation LinkStatementRefundCorrectionMutation($input: LinkStatementRefundCorrectionInput!) {
    linkStatementRefundCorrection(input: $input) {
      __typename
      ... on LinkStatementRefundCorrectionSuccess {
        result {
          id
        }
      }
      ... on LinkStatementRefundCorrectionUserError {
        message
      }
    }
  }
`;
