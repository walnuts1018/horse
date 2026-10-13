import { graphql } from "react-relay";

export const purchasesQuery = graphql`
  query PurchasesQuery(
    $householdId: ID!
    $from: String
    $to: String
    $origin: PurchaseOrigin
    $hasEvidence: Boolean
    $first: Int
  ) {
    ...PurchaseListFragment
      @arguments(
        householdId: $householdId
        from: $from
        to: $to
        origin: $origin
        hasEvidence: $hasEvidence
        first: $first
      )
  }
`;

export const purchaseListFragment = graphql`
  fragment PurchaseListFragment on Query
  @argumentDefinitions(
    householdId: { type: "ID!" }
    from: { type: "String" }
    to: { type: "String" }
    origin: { type: "PurchaseOrigin" }
    hasEvidence: { type: "Boolean" }
    first: { type: "Int", defaultValue: 100 }
    after: { type: "String" }
  )
  @refetchable(queryName: "PurchasePaginationQuery") {
    purchases(
      householdId: $householdId
      from: $from
      to: $to
      origin: $origin
      hasEvidence: $hasEvidence
      after: $after
      first: $first
    )
      @connection(
        key: "PurchaseListFragment_purchases"
        filters: ["householdId", "from", "to", "origin", "hasEvidence"]
      ) {
      edges {
        cursor
        node {
          id
          householdId
          occurredAt
          merchant
          origin
          hasEvidence
          sourceKinds
          paymentAllocations {
            paymentAccountId
            amountYen
            account {
              id
              name
            }
          }
          items {
            ...PurchaseItemFields @relay(mask: false)
          }
          totalYen
          corrections {
            id
            itemId
            amountDeltaYen
            reason
            occurredAt
            settlementMonth
            paymentAllocations {
              paymentAccountId
              amountYen
              account {
                id
                name
              }
            }
          }
          revision
          status
          createdAt
          updatedAt
        }
      }
      pageInfo {
        startCursor
        endCursor
        hasNextPage
        hasPreviousPage
      }
      totalCount
      syncCursor
    }
  }
`;

export const purchaseChangesSubscription = graphql`
  subscription PurchaseChangesSubscription($householdId: ID!, $after: String) {
    purchaseChanges(householdId: $householdId, after: $after) {
      cursor
    }
  }
`;

export const createPurchaseMutation = graphql`
  mutation CreatePurchaseMutation($input: CreatePurchaseInput!) {
    createPurchase(input: $input) {
      __typename
      ... on CreatePurchaseSuccess {
        result {
          id
          householdId
          occurredAt
          merchant
          totalYen
          revision
          status
          items {
            ...PurchaseItemFields @relay(mask: false)
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

export const postPurchaseMutation = graphql`
  mutation PostPurchaseMutation($input: PostPurchaseInput!) {
    postPurchase(input: $input) {
      __typename
      ... on PostPurchaseSuccess {
        result {
          id
          revision
          status
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const updatePurchaseMutation = graphql`
  mutation UpdatePurchaseMutation($input: UpdatePurchaseInput!) {
    updatePurchase(input: $input) {
      __typename
      ... on UpdatePurchaseSuccess {
        result {
          id
          revision
          occurredAt
          merchant
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const bulkUpdatePurchasesMutation = graphql`
  mutation BulkUpdatePurchasesMutation($input: BulkUpdatePurchasesInput!) {
    bulkUpdatePurchases(input: $input) {
      __typename
      ... on BulkUpdatePurchasesSuccess {
        purchases {
          id
          revision
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const updatePurchaseItemMutation = graphql`
  mutation UpdatePurchaseItemMutation($input: UpdatePurchaseItemInput!) {
    updatePurchaseItem(input: $input) {
      __typename
      ... on UpdatePurchaseItemSuccess {
        result {
          ...PurchaseItemFields @relay(mask: false)
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const correctPurchaseMutation = graphql`
  mutation CorrectPurchaseMutation($input: CorrectPurchaseInput!) {
    correctPurchase(input: $input) {
      __typename
      ... on CorrectPurchaseSuccess {
        result {
          id
          revision
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const bulkUpdatePurchaseItemsMutation = graphql`
  mutation BulkUpdatePurchaseItemsMutation($input: BulkUpdatePurchaseItemsInput!) {
    bulkUpdatePurchaseItems(input: $input) {
      __typename
      ... on BulkUpdatePurchaseItemsSuccess {
        items {
          ...PurchaseItemFields @relay(mask: false)
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const purchaseItemFields = graphql`
  fragment PurchaseItemFields on PurchaseItem {
    id
    name
    amountYen
    currency
    quantity {
      numerator
      denominator
      unit
    }
    printedUnitPrice {
      amountYen
      basis {
        numerator
        denominator
        unit
      }
    }
    effectiveUnitPrice {
      numeratorYen
      denominator
    }
    revision
    categoryId
    productVariantId
    allocations {
      poolId
      poolName
      weight
      amountYen
    }
    tags {
      id
      householdId
      name
      archived
      revision
    }
  }
`;
