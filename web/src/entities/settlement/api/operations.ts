import { graphql } from "react-relay";

export const settlementQuery = graphql`
  query SettlementQuery($householdId: ID!, $period: String!) {
    settlement(householdId: $householdId, period: $period) {
      period
      status
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
    }
  }
`;

export const closePeriodMutation = graphql`
  mutation ClosePeriodMutation($input: ClosePeriodInput!) {
    closePeriod(input: $input) {
      __typename
      ... on ClosePeriodSuccess {
        result {
          period
          status
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
