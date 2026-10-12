import { graphql } from "react-relay";

export const householdQuery = graphql`
  query HouseholdQuery($id: ID!) {
    household(id: $id) {
      id
      name
      revision
      members {
        id
        displayName
        role
      }
      pools {
        id
        name
        kind
        defaultShareWeight
        ownerMemberId
        fundingShares {
          memberId
          memberName
          weight
        }
        settlementPolicy
        fundingRuleVersion
      }
      accounts {
        id
        name
        kind
        poolId
      }
      categories {
        id
        name
        parentId
        archived
      }
      defaultAllocation {
        poolId
        weight
      }
    }
  }
`;

export const householdsQuery = graphql`
  query HouseholdsQuery {
    households {
      id
      name
    }
  }
`;

export const createHouseholdMutation = graphql`
  mutation CreateHouseholdMutation($input: CreateHouseholdInput!) {
    createHousehold(input: $input) {
      __typename
      ... on CreateHouseholdSuccess {
        result {
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

export const acceptHouseholdInvitationMutation = graphql`
  mutation AcceptHouseholdInvitationMutation($token: String!) {
    acceptHouseholdInvitation(token: $token) {
      __typename
      ... on AcceptHouseholdInvitationSuccess {
        result {
          id
          name
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;
