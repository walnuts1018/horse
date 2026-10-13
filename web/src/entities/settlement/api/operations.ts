import { graphql } from "react-relay";

export const settlementQuery = graphql`
  query SettlementQuery($householdId: ID!, $period: YearMonth!) {
    settlement(householdId: $householdId, period: $period) {
      __typename
      id
      period
      revision
      lines {
        memberId
        memberName
        scheduledYen
        paidYen
        carryYen
        priorBalanceYen
        monthlyAdjustmentYen
        advanceCreditYen
        amountDueYen
      }
      ... on ClosedSettlement {
        createdAt
      }
    }
  }
`;

export const closePeriodMutation = graphql`
  mutation ClosePeriodMutation($input: ClosePeriodInput!) {
    closePeriod(input: $input) {
      __typename
      ... on ClosePeriodSuccess {
        result {
          __typename
          id
          period
          revision
          ... on ClosedSettlement {
            createdAt
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
