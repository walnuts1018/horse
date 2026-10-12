import { graphql } from "react-relay";

export const householdInvitationsQuery = graphql`
  query HouseholdInvitationsQuery($householdId: ID!) {
    householdInvitations(householdId: $householdId) {
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
  mutation RevokeHouseholdInvitationMutation($householdId: ID!, $invitationId: ID!) {
    revokeHouseholdInvitation(householdId: $householdId, invitationId: $invitationId) {
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
