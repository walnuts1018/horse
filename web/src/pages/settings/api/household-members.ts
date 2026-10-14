import { commitMutation, graphql } from "react-relay";

import {
  getRelayEnvironment,
  type LeaveHouseholdMutation$data,
  type RemoveHouseholdMemberMutation$data,
} from "../../../shared/api";

type RemoveHouseholdMemberOperation = {
  response: RemoveHouseholdMemberMutation$data;
  variables: { input: { householdId: string; memberId: string } };
};

type LeaveHouseholdOperation = {
  response: LeaveHouseholdMutation$data;
  variables: { input: { householdId: string } };
};

export function removeHouseholdMember(input: {
  householdId: string;
  memberId: string;
}): Promise<void> {
  return new Promise((resolve, reject) =>
    commitMutation<RemoveHouseholdMemberOperation>(getRelayEnvironment(), {
      mutation: removeHouseholdMemberMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new Error(errors[0].message));
          return;
        }
        const result = response.removeHouseholdMember;
        switch (result.__typename) {
          case "RemoveHouseholdMemberSuccess":
            resolve();
            return;
          case "RemoveHouseholdMemberUserError":
            reject(new Error(result.message ?? "メンバーを除名できませんでした"));
            return;
          default:
            reject(new Error(`未対応のメンバー除名結果です: ${result.__typename}`));
        }
      },
      onError: reject,
    }),
  );
}

export function leaveHousehold(input: { householdId: string }): Promise<void> {
  return new Promise((resolve, reject) =>
    commitMutation<LeaveHouseholdOperation>(getRelayEnvironment(), {
      mutation: leaveHouseholdMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new Error(errors[0].message));
          return;
        }
        const result = response.leaveHousehold;
        switch (result.__typename) {
          case "LeaveHouseholdSuccess":
            resolve();
            return;
          case "LeaveHouseholdUserError":
            reject(new Error(result.message ?? "家計簿から脱退できませんでした"));
            return;
          default:
            reject(new Error(`未対応の脱退結果です: ${result.__typename}`));
        }
      },
      onError: reject,
    }),
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
