import { graphql } from "react-relay";

export const recurringExpensesQuery = graphql`
  query RecurringExpensesQuery(
    $householdId: ID!
    $includeArchived: Boolean!
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

export const createRecurringExpenseMutation = graphql`
  mutation CreateRecurringExpenseMutation($input: CreateRecurringExpenseInput!) {
    createRecurringExpense(input: $input) {
      __typename
      ... on CreateRecurringExpenseSuccess {
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

export const updateRecurringExpenseMutation = graphql`
  mutation UpdateRecurringExpenseMutation($input: UpdateRecurringExpenseInput!) {
    updateRecurringExpense(input: $input) {
      __typename
      ... on UpdateRecurringExpenseSuccess {
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

export const matchRecurringExpenseMutation = graphql`
  mutation MatchRecurringExpenseMutation($input: MatchRecurringExpenseInput!) {
    matchRecurringExpense(input: $input) {
      __typename
      ... on MatchRecurringExpenseSuccess {
        result {
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
      ... on MutationError {
        code
        message
      }
    }
  }
`;
