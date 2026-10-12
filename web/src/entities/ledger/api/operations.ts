import { graphql } from "react-relay";

export const ledgerQuery = graphql`
  query LedgerQuery($householdId: ID!, $after: String, $first: Int) {
    ledger(householdId: $householdId, after: $after, first: $first) {
      edges {
        cursor
        node {
          ...LedgerEntryFields
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
  query LoanBalancesQuery($householdId: ID!) {
    loanBalances(householdId: $householdId) {
      loanId
      lenderMemberId
      borrowerMemberId
      principalYen
      repaidYen
      balanceYen
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

export const accountMovementsQuery = graphql`
  query AccountMovementsQuery($householdId: ID!, $first: Int) {
    accountMovements(householdId: $householdId, first: $first) {
      id
      householdId
      kind
      occurredAt
      fromAccountId
      toAccountId
      amountYen
      currency
      description
      createdAt
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

export const accountBalanceAnalysisQuery = graphql`
  query AccountBalanceAnalysisQuery(
    $householdId: ID!
    $accountId: ID!
    $from: String!
    $to: String!
  ) {
    accountBalanceAnalysis(householdId: $householdId, accountId: $accountId, from: $from, to: $to) {
      accountId
      currency
      from
      to
      openingBalanceYen
      closingBalanceYen
      changes {
        occurredAt
        sourceKind
        sourceId
        description
        changeYen
        balanceYen
      }
    }
  }
`;

export const accountIncomesQuery = graphql`
  query AccountIncomesQuery($householdId: ID!, $first: Int) {
    accountIncomes(householdId: $householdId, first: $first) {
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
      transferKind
      loan {
        loanId
      }
    }
    ... on CorrectionLedgerEntry {
      purchase {
        id
      }
    }
  }
`;
