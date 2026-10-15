import { graphql } from "react-relay";

export const sharedPurchaseDraftQuery = graphql`
  query SharedPurchaseDraftQuery($id: ID!) {
    sharedPurchaseDraft(id: $id) {
      id
      householdId
      revision
      payload
      createdAt
      updatedAt
    }
  }
`;

export const sharedPurchaseDraftChangesSubscription = graphql`
  subscription SharedPurchaseDraftChangesSubscription($id: ID!, $after: String) {
    sharedPurchaseDraftChanges(id: $id, after: $after) {
      cursor
    }
  }
`;

export const createSharedPurchaseDraftMutation = graphql`
  mutation CreateSharedPurchaseDraftMutation($householdId: ID!, $payload: String!) {
    createSharedPurchaseDraft(input: { householdId: $householdId, payload: $payload }) {
      __typename
      ... on CreateSharedPurchaseDraftSuccess {
        result {
          id
          householdId
          revision
          payload
          createdAt
          updatedAt
        }
      }
      ... on SharedPurchaseDraftUserError {
        code
        message
      }
    }
  }
`;

export const updateSharedPurchaseDraftMutation = graphql`
  mutation UpdateSharedPurchaseDraftMutation($id: ID!, $expectedRevision: Int!, $payload: String!) {
    updateSharedPurchaseDraft(input: { id: $id, expectedRevision: $expectedRevision, payload: $payload }) {
      __typename
      ... on UpdateSharedPurchaseDraftSuccess {
        result {
          id
          householdId
          revision
          payload
          createdAt
          updatedAt
        }
      }
      ... on UpdateSharedPurchaseDraftConflict {
        code
        message
        draft {
          id
          householdId
          revision
          payload
          createdAt
          updatedAt
        }
      }
      ... on SharedPurchaseDraftUserError {
        code
        message
      }
    }
  }
`;
