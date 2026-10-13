import { graphql } from "react-relay";

export const accountBalanceAnalysisQuery = graphql`
  query AccountBalanceAnalysisQuery(
    $householdId: ID!
    $accountId: ID!
    $from: YearMonth!
    $to: YearMonth!
    $first: Int
    $after: String
  ) {
    accountBalanceAnalysis(
      householdId: $householdId
      accountId: $accountId
      from: $from
      to: $to
      first: $first
      after: $after
    ) {
      accountId
      currency
      from
      to
      openingBalanceYen
      closingBalanceYen
      changes {
        edges {
          cursor
          node {
            occurredAt
            source {
              __typename
              ... on AccountOpeningBalance {
                id
                account {
                  id
                  name
                }
              }
              ... on Purchase {
                id
              }
              ... on PurchaseWideCorrection {
                id
              }
              ... on PurchaseItemCorrection {
                id
              }
              ... on CorrectionLedgerEntry {
                id
              }
              ... on FundingLedgerEntry {
                id
              }
              ... on PurchaseLedgerEntry {
                id
              }
              ... on AdvanceTransferLedgerEntry {
                id
              }
              ... on LoanTransferLedgerEntry {
                id
              }
              ... on RepaymentTransferLedgerEntry {
                id
              }
              ... on GiftTransferLedgerEntry {
                id
              }
              ... on AccountMovement {
                id
              }
              ... on AccountIncome {
                id
              }
            }
            description
            changeYen
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
  }
`;
