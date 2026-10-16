import { graphql } from "react-relay";

export const budgetsQuery = graphql`
  query BudgetsQuery($householdId: ID!, $period: YearMonth!, $first: Int!, $after: String) {
    budgets(householdId: $householdId, period: $period, first: $first, after: $after) {
      edges {
        cursor
        node {
          id
          householdId
          categoryId
          name
          amountYen
          actualYen
          remainingYen
          startsOn
          endsOn
          archived
          revision
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

export const createBudgetMutation = graphql`
  mutation CreateBudgetMutation($input: CreateBudgetInput!) {
    createBudget(input: $input) {
      __typename
      ... on CreateBudgetSuccess {
        result {
          budgetId
          categoryId
          name
          amountYen
          startsOn
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

export const updateBudgetMutation = graphql`
  mutation UpdateBudgetMutation($input: UpdateBudgetInput!) {
    updateBudget(input: $input) {
      __typename
      ... on UpdateBudgetSuccess {
        result {
          budgetId
          categoryId
          name
          amountYen
          startsOn
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

export const archiveBudgetMutation = graphql`
  mutation ArchiveBudgetMutation($input: ArchiveBudgetInput!) {
    archiveBudget(input: $input) {
      __typename
      ... on ArchiveBudgetSuccess {
        result {
          budgetId
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
