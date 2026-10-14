import { graphql } from "react-relay";

import {
  assertGraphQLMutationSuccess,
  commitGraphQLMutation,
  type LeaveHouseholdMutation$data,
  type RemoveHouseholdMemberMutation$data,
  type TransferHouseholdOwnerMutation$data,
} from "../../../shared/api";

type RemoveHouseholdMemberOperation = {
  response: RemoveHouseholdMemberMutation$data;
  variables: { input: { householdId: string; memberId: string } };
};

type LeaveHouseholdOperation = {
  response: LeaveHouseholdMutation$data;
  variables: { input: { householdId: string } };
};

type TransferHouseholdOwnerOperation = {
  response: TransferHouseholdOwnerMutation$data;
  variables: { input: { householdId: string; newOwnerId: string } };
};

export function removeHouseholdMember(input: {
  householdId: string;
  memberId: string;
}): Promise<void> {
  return commitGraphQLMutation<RemoveHouseholdMemberOperation, void>(
    removeHouseholdMemberMutation,
    { input },
    (response) => {
      assertGraphQLMutationSuccess(
        response.removeHouseholdMember,
        "RemoveHouseholdMemberSuccess",
        "メンバーを除名できませんでした",
        "未対応のメンバー除名結果です",
      );
    },
  );
}

export function leaveHousehold(input: { householdId: string }): Promise<void> {
  return commitGraphQLMutation<LeaveHouseholdOperation, void>(
    leaveHouseholdMutation,
    { input },
    (response) => {
      assertGraphQLMutationSuccess(
        response.leaveHousehold,
        "LeaveHouseholdSuccess",
        "家計簿から脱退できませんでした",
        "未対応の脱退結果です",
      );
    },
  );
}

export function transferHouseholdOwner(input: {
  householdId: string;
  newOwnerId: string;
}): Promise<void> {
  return commitGraphQLMutation<TransferHouseholdOwnerOperation, void>(
    transferHouseholdOwnerMutation,
    { input },
    (response) => {
      assertGraphQLMutationSuccess(
        response.transferHouseholdOwner,
        "TransferHouseholdOwnerSuccess",
        "オーナーを移譲できませんでした",
        "未対応のオーナー移譲結果です",
      );
    },
  );
}

export const removeHouseholdMemberMutation = graphql`
  mutation RemoveHouseholdMemberMutation($input: RemoveHouseholdMemberInput!) {
    removeHouseholdMember(input: $input) {
      __typename
      ... on RemoveHouseholdMemberSuccess {
        householdId
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const leaveHouseholdMutation = graphql`
  mutation LeaveHouseholdMutation($input: LeaveHouseholdInput!) {
    leaveHousehold(input: $input) {
      __typename
      ... on LeaveHouseholdSuccess {
        householdId
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const transferHouseholdOwnerMutation = graphql`
  mutation TransferHouseholdOwnerMutation($input: TransferHouseholdOwnerInput!) {
    transferHouseholdOwner(input: $input) {
      __typename
      ... on TransferHouseholdOwnerSuccess {
        householdId
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;
