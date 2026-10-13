import type { GraphQLTaggedNode, MutationParameters } from "relay-runtime";

import { commitMutation, graphql } from "react-relay";

import {
  getRelayEnvironment,
  type SettlementClosePeriodMutation,
  type SettlementRecordFundingMutation,
  type SettlementRecordRepaymentMutation,
  type SettlementRecordTransferMutation,
} from "../../../shared/api";

type MutationResult = {
  __typename: string;
  message?: string | null;
  result?: unknown | null;
};

function commitSettlementMutation<TOperation extends MutationParameters>(
  mutation: GraphQLTaggedNode,
  variables: TOperation["variables"],
  readResult: (response: TOperation["response"]) => MutationResult,
  successType: string,
  failureMessage: string,
): Promise<void> {
  return new Promise((resolve, reject) =>
    commitMutation<TOperation>(getRelayEnvironment(), {
      mutation,
      variables,
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new Error(errors[0].message));
          return;
        }
        const result = readResult(response);
        switch (result.__typename) {
          case successType:
            if (result.result == null) {
              reject(new Error(failureMessage));
              return;
            }
            resolve();
            return;
          case "MutationError":
            reject(new Error(result.message ?? failureMessage));
            return;
          default:
            reject(new Error(`未対応の精算結果です: ${result.__typename}`));
        }
      },
      onError: reject,
    }),
  );
}

export function recordSettlementFunding(
  input: SettlementRecordFundingMutation["variables"]["input"],
): Promise<void> {
  return commitSettlementMutation<SettlementRecordFundingMutation>(
    recordSettlementFundingMutation,
    { input },
    (response) => response.recordFunding,
    "RecordFundingSuccess",
    "入金を記録できませんでした",
  );
}

export function closeSettlementPeriod(
  input: SettlementClosePeriodMutation["variables"]["input"],
): Promise<void> {
  return commitSettlementMutation<SettlementClosePeriodMutation>(
    closeSettlementPeriodMutation,
    { input },
    (response) => response.closePeriod,
    "ClosePeriodSuccess",
    "精算期間を締められませんでした",
  );
}

export function recordSettlementTransfer(
  input: SettlementRecordTransferMutation["variables"]["input"],
): Promise<void> {
  return commitSettlementMutation<SettlementRecordTransferMutation>(
    recordSettlementTransferMutation,
    { input },
    (response) => response.recordTransfer,
    "RecordTransferSuccess",
    "送金を記録できませんでした",
  );
}

export function recordSettlementRepayment(
  input: SettlementRecordRepaymentMutation["variables"]["input"],
): Promise<void> {
  return commitSettlementMutation<SettlementRecordRepaymentMutation>(
    recordSettlementRepaymentMutation,
    { input },
    (response) => response.recordRepayment,
    "RecordRepaymentSuccess",
    "返済を記録できませんでした",
  );
}

const recordSettlementFundingMutation = graphql`
  mutation SettlementRecordFundingMutation($input: RecordFundingInput!) {
    recordFunding(input: $input) {
      __typename
      ... on RecordFundingSuccess {
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

const closeSettlementPeriodMutation = graphql`
  mutation SettlementClosePeriodMutation($input: ClosePeriodInput!) {
    closePeriod(input: $input) {
      __typename
      ... on ClosePeriodSuccess {
        result {
          __typename
          id
          period
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

const recordSettlementTransferMutation = graphql`
  mutation SettlementRecordTransferMutation($input: RecordTransferInput!) {
    recordTransfer(input: $input) {
      __typename
      ... on RecordTransferSuccess {
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

const recordSettlementRepaymentMutation = graphql`
  mutation SettlementRecordRepaymentMutation($input: RecordRepaymentInput!) {
    recordRepayment(input: $input) {
      __typename
      ... on RecordRepaymentSuccess {
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
