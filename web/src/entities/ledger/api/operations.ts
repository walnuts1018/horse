import { graphql } from "react-relay";

export const householdLedgerQuery = graphql`
  query HouseholdLedgerQuery($householdId: ID!, $first: Int) {
    ...HouseholdLedgerFragment @arguments(householdId: $householdId, first: $first)
  }
`;

export const householdLedgerFragment = graphql`
  fragment HouseholdLedgerFragment on Query
  @argumentDefinitions(
    householdId: { type: "ID!" }
    first: { type: "Int", defaultValue: 100 }
    after: { type: "String" }
  )
  @refetchable(queryName: "HouseholdLedgerPaginationQuery") {
    ledger(householdId: $householdId, first: $first, after: $after)
      @connection(key: "HouseholdLedgerFragment_ledger", filters: ["householdId"]) {
      edges {
        cursor
        node {
          ...LedgerEntryFields @relay(mask: false)
          ... on FundingLedgerEntry {
            member {
              id
            }
            pool {
              id
            }
          }
          ... on TransferLedgerEntry {
            fromMember {
              id
            }
            toMember {
              id
            }
          }
          ... on PurchaseLedgerEntry {
            purchase {
              id
            }
          }
          ... on CorrectionLedgerEntry {
            purchase {
              id
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

export const loanBalancesQuery = graphql`
  query LoanBalancesQuery($householdId: ID!, $after: String, $first: Int) {
    loanBalances(householdId: $householdId, after: $after, first: $first) {
      edges {
        cursor
        node {
          id
          lenderMemberId
          borrowerMemberId
          principalYen
          repaidYen
          balanceYen
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

export const recordFundingMutation = graphql`
  mutation RecordFundingMutation($input: RecordFundingInput!) {
    recordFunding(input: $input) {
      __typename
      ... on RecordFundingSuccess {
        result {
          ...LedgerEntryFields
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const recordTransferMutation = graphql`
  mutation RecordTransferMutation($input: RecordTransferInput!) {
    recordTransfer(input: $input) {
      __typename
      ... on RecordTransferSuccess {
        result {
          ...LedgerEntryFields
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const recordRepaymentMutation = graphql`
  mutation RecordRepaymentMutation($input: RecordRepaymentInput!) {
    recordRepayment(input: $input) {
      __typename
      ... on RecordRepaymentSuccess {
        result {
          ...LedgerEntryFields
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const accountMovementsQuery = graphql`
  query AccountMovementsQuery($householdId: ID!, $first: Int, $after: String) {
    accountMovements(householdId: $householdId, first: $first, after: $after) {
      edges {
        cursor
        node {
          id
          householdId
          kind
          occurredAt
          fromAccountId
          fromAccount {
            id
            name
          }
          toAccountId
          toAccount {
            id
            name
          }
          amountYen
          currency
          description
          createdAt
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
    }
  }
`;

export const accountBalancesQuery = graphql`
  query AccountBalancesQuery($householdId: ID!) {
    accountBalances(householdId: $householdId) {
      accountId
      balanceYen
      currency
    }
  }
`;

export const accountIncomesQuery = graphql`
  query AccountIncomesQuery($householdId: ID!, $first: Int, $after: String) {
    accountIncomes(householdId: $householdId, first: $first, after: $after) {
      edges {
        cursor
        node {
          id
          householdId
          accountId
          amountYen
          currency
          occurredAt
          description
          createdAt
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
    }
  }
`;

export const recordAccountMovementMutation = graphql`
  mutation RecordAccountMovementMutation($input: RecordAccountMovementInput!) {
    recordAccountMovement(input: $input) {
      __typename
      ... on RecordAccountMovementSuccess {
        result {
          id
          kind
          occurredAt
          fromAccountId
          toAccountId
          amountYen
          description
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const recordExternalIncomeMutation = graphql`
  mutation RecordExternalIncomeMutation($input: RecordExternalIncomeInput!) {
    recordExternalIncome(input: $input) {
      __typename
      ... on RecordExternalIncomeSuccess {
        result {
          id
          householdId
          accountId
          currency
          occurredAt
          amountYen
          description
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const ledgerEntryFields = graphql`
  fragment LedgerEntryFields on LedgerEntry {
    __typename
    id
    occurredAt
    description
    amountYen
    currency
    createdAt
    ... on PurchaseLedgerEntry {
      purchase {
        id
      }
    }
    ... on FundingLedgerEntry {
      member {
        id
        displayName
      }
      pool {
        id
        name
      }
    }
    ... on TransferLedgerEntry {
      fromMember {
        id
        displayName
      }
      toMember {
        id
        displayName
      }
    }
    ... on LoanTransferLedgerEntry {
      loan {
        id
      }
    }
    ... on RepaymentTransferLedgerEntry {
      loan {
        id
      }
    }
    ... on CorrectionLedgerEntry {
      purchase {
        id
      }
    }
  }
`;
