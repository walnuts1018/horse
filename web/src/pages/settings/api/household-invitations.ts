import { graphql } from "react-relay";

export const householdInvitationsQuery = graphql`
  query HouseholdInvitationsQuery($householdId: ID!, $after: String, $first: Int) {
    householdInvitations(householdId: $householdId, after: $after, first: $first) {
      edges {
        cursor
        node {
          __typename
          id
          householdId
          expiresAt
          createdAt
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
            __typename
            id
            householdId
            expiresAt
            createdAt
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
          __typename
          id
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;
