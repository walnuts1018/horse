import { graphql } from "react-relay";

export const householdInvitationsQuery = graphql`
  query HouseholdInvitationsQuery($householdId: ID!, $after: String, $first: Int) {
    householdInvitations(householdId: $householdId, after: $after, first: $first) {
      edges {
        cursor
        node {
          id
          householdId
          role
          status
          expiresAt
          createdAt
          revokedAt
          acceptedAt
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

export const createHouseholdInvitationMutation = graphql`
  mutation CreateHouseholdInvitationMutation($input: CreateHouseholdInvitationInput!) {
    createHouseholdInvitation(input: $input) {
      __typename
      ... on CreateHouseholdInvitationSuccess {
        result {
          token
          invitation {
            id
            householdId
            role
            status
            expiresAt
            createdAt
            revokedAt
            acceptedAt
          }
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const revokeHouseholdInvitationMutation = graphql`
  mutation RevokeHouseholdInvitationMutation($input: RevokeHouseholdInvitationInput!) {
    revokeHouseholdInvitation(input: $input) {
      __typename
      ... on RevokeHouseholdInvitationSuccess {
        result {
          id
          status
          revokedAt
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;
