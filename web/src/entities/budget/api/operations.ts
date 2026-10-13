import { graphql } from "react-relay";

export const budgetsQuery = graphql`
  query BudgetsQuery($householdId: ID!, $period: String!) {
    budgets(householdId: $householdId, period: $period) {
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
`;

export const setBudgetMutation = graphql`
  mutation SetBudgetMutation($input: SetBudgetInput!) {
    setBudget(input: $input) {
      __typename
      ... on SetBudgetSuccess {
        result {
          id
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
