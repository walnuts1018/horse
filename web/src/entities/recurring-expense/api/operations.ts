import { graphql } from "react-relay";

export const recurringExpensesQuery = graphql`
  query RecurringExpensesQuery(
    $householdId: ID!
    $includeArchived: Boolean
    $after: String
    $first: Int
  ) {
    recurringExpenses(
      householdId: $householdId
      includeArchived: $includeArchived
      after: $after
      first: $first
    ) {
      edges {
        cursor
        node {
          id
          householdId
          name
          merchant
          amountYen
          currency
          categoryId
          paymentAccountId
          cadence
          startsOn
          nextDueOn
          endsOn
          archived
          revision
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
    }
  }
`;

export const setRecurringExpenseMutation = graphql`
  mutation SetRecurringExpenseMutation($input: SetRecurringExpenseInput!) {
    setRecurringExpense(input: $input) {
      __typename
      ... on SetRecurringExpenseSuccess {
        result {
          id
          name
          merchant
          amountYen
          categoryId
          paymentAccountId
          cadence
          startsOn
          nextDueOn
          endsOn
          archived
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

export const archiveRecurringExpenseMutation = graphql`
  mutation ArchiveRecurringExpenseMutation($input: ArchiveRecurringExpenseInput!) {
    archiveRecurringExpense(input: $input) {
      __typename
      ... on ArchiveRecurringExpenseSuccess {
        result {
          id
          archived
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
