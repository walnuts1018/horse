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
    createSharedPurchaseDraft(householdId: $householdId, payload: $payload) {
      id
      householdId
      revision
      payload
      createdAt
      updatedAt
    }
  }
`;

export const updateSharedPurchaseDraftMutation = graphql`
  mutation UpdateSharedPurchaseDraftMutation($id: ID!, $expectedRevision: Int!, $payload: String!) {
    updateSharedPurchaseDraft(id: $id, expectedRevision: $expectedRevision, payload: $payload) {
      conflict
      draft {
        id
        householdId
        revision
        payload
        createdAt
        updatedAt
      }
    }
  }
`;
