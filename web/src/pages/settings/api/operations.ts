import { graphql } from "react-relay";

export const recurringExpenseMatchCandidatesQuery = graphql`
  query RecurringExpenseMatchCandidatesQuery($filter: PurchaseFilter!, $first: Int) {
    purchases(filter: $filter, first: $first) {
      edges {
        node {
          id
          occurredAt
          merchant
          totalYen
          status
        }
      }
    }
  }
`;

export const allocationRulesQuery = graphql`
  query AllocationRulesQuery($householdId: ID!, $after: String, $first: Int) {
    allocationRules(householdId: $householdId, after: $after, first: $first) {
      edges {
        cursor
        node {
          id
          householdId
          target {
            __typename
            name
          }
          revision
          allocations {
            poolId
            poolName
            weight
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

export const setCategoryArchivedMutation = graphql`
  mutation SetCategoryArchivedMutation($input: SetCategoryArchivedInput!) {
    setCategoryArchived(input: $input) {
      __typename
      ... on SetCategoryArchivedSuccess {
        result {
          id
          archived
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const setAllocationRuleMutation = graphql`
  mutation SetAllocationRuleMutation($input: SetAllocationRuleInput!) {
    setAllocationRule(input: $input) {
      __typename
      ... on SetAllocationRuleSuccess {
        result {
          id
          target {
            __typename
            name
          }
          revision
          allocations {
            poolId
            weight
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

export const createPoolMutation = graphql`
  mutation CreatePoolMutation($input: CreatePoolInput!) {
    createPool(input: $input) {
      __typename
      ... on CreatePoolSuccess {
        result {
          id
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const createAccountMutation = graphql`
  mutation CreateAccountMutation($input: CreateAccountInput!) {
    createAccount(input: $input) {
      __typename
      ... on CreateAccountSuccess {
        result {
          id
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const createCategoryMutation = graphql`
  mutation CreateCategoryMutation($input: CreateCategoryInput!) {
    createCategory(input: $input) {
      __typename
      ... on CreateCategorySuccess {
        result {
          id
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const setDefaultAllocationMutation = graphql`
  mutation SetDefaultAllocationMutation($input: SetDefaultAllocationInput!) {
    setDefaultAllocation(input: $input) {
      __typename
      ... on SetDefaultAllocationSuccess {
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

export const setPoolFundingRuleMutation = graphql`
  mutation SetPoolFundingRuleMutation($input: SetPoolFundingRuleInput!) {
    setPoolFundingRule(input: $input) {
      __typename
      ... on SetPoolFundingRuleSuccess {
        result {
          id
          __typename
          ... on PersonalPool {
            ownerMemberId
          }
          fundingRuleVersion
          ... on SharedPool {
            settlementPolicy
          }
          fundingShares {
            memberId
            memberName
            weight
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

export const setFundingScheduleMutation = graphql`
  mutation SetFundingScheduleMutation($input: SetFundingScheduleInput!) {
    setFundingSchedule(input: $input) {
      __typename
      ... on SetFundingScheduleSuccess {
        result {
          id
          householdId
          memberId
          poolId
          amountYen
          cadence
          startsOn
          endsOn
          active
          createdAt
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;
