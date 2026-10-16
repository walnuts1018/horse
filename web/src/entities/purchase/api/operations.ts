import { graphql } from "react-relay";

export const purchasesQuery = graphql`
  query PurchasesQuery($filter: PurchaseFilter!, $first: Int) {
    ...PurchaseListFragment @arguments(filter: $filter, first: $first)
  }
`;

export const purchaseHistoryQuery = graphql`
  query PurchaseHistoryQuery($filter: PurchaseFilter!, $first: Int, $after: String) {
    purchases(filter: $filter, after: $after, first: $first) {
      edges {
        cursor
        node {
          id
          householdId
          occurredAt
          datePrecision
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
          correctionAmountYen
          correctionCount
          remainingRefundableAmountYen
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

export const purchaseCorrectionsPaginationQuery = graphql`
  query PurchaseCorrectionsPaginationQuery($purchaseId: ID!, $first: Int, $after: String) {
    purchaseCorrections(purchaseId: $purchaseId, first: $first, after: $after) {
      edges {
        cursor
        node {
          __typename
          id
          ... on PurchaseItemCorrection {
            itemId
          }
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
      }
      pageInfo {
        endCursor
        hasNextPage
      }
    }
  }
`;

export const purchaseListFragment = graphql`
  fragment PurchaseListFragment on Query
  @argumentDefinitions(
    filter: { type: "PurchaseFilter!" }
    first: { type: "Int", defaultValue: 100 }
    after: { type: "String" }
  )
  @refetchable(queryName: "PurchasePaginationQuery") {
    purchases(filter: $filter, after: $after, first: $first)
      @connection(key: "PurchaseListFragment_purchases", filters: ["filter"]) {
      edges {
        cursor
        node {
          id
          householdId
          occurredAt
          datePrecision
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
          correctionAmountYen
          correctionCount
          remainingRefundableAmountYen
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
      entityId
      entityKind
      kind
      changedFields
      purchase {
        id
        householdId
        occurredAt
        datePrecision
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
        correctionAmountYen
        correctionCount
        remainingRefundableAmountYen
        revision
        status
        createdAt
        updatedAt
      }
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
          datePrecision
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

export const createAndPostPurchaseMutation = graphql`
  mutation CreateAndPostPurchaseMutation($input: CreatePurchaseInput!) {
    createAndPostPurchase(input: $input) {
      __typename
      ... on CreateAndPostPurchaseSuccess {
        result {
          id
          revision
          status
          items {
            ...PurchaseItemFields @relay(mask: false)
          }
        }
      }
      ... on CreateAndPostPurchaseUserError {
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
          datePrecision
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

export const savePurchaseItemEditsMutation = graphql`
  mutation SavePurchaseItemEditsMutation($input: SavePurchaseItemEditsInput!) {
    savePurchaseItemEdits(input: $input) {
      __typename
      ... on SavePurchaseItemEditsSuccess {
        items {
          ...PurchaseItemFields @relay(mask: false)
        }
      }
      ... on SavePurchaseItemEditsUserError {
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

export const correctPurchaseFromStatementMutation = graphql`
  mutation CorrectPurchaseFromStatementMutation($input: CorrectPurchaseFromStatementInput!) {
    correctPurchaseFromStatement(input: $input) {
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
    remainingRefundableAmountYen
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
      denominatorYen
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
