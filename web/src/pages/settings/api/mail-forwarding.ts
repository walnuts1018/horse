import { fetchQuery, graphql } from "react-relay";

import {
  getRelayEnvironment,
  assertGraphQLMutationSuccess,
  commitGraphQLMutation,
  readGraphQLMutationResult,
  type HouseholdMailForwardingStatusQuery,
  type HouseholdMailForwardingStatusQuery$data,
  type IssueHouseholdMailForwardingMutation$data,
  type RevokeHouseholdMailForwardingMutation$data,
  type RotateHouseholdMailForwardingMutation$data,
} from "../../../shared/api";

export type MailForwardingAddressStatus = NonNullable<
  HouseholdMailForwardingStatusQuery$data["householdMailForwarding"]
>;

export type IssuedMailForwardingAddress = {
  address: string;
  createdAt: string;
};

type IssueOperation = {
  response: IssueHouseholdMailForwardingMutation$data;
  variables: { input: { householdId: string } };
};

type RotateOperation = {
  response: RotateHouseholdMailForwardingMutation$data;
  variables: { input: { householdId: string } };
};

type RevokeOperation = {
  response: RevokeHouseholdMailForwardingMutation$data;
  variables: { input: { householdId: string } };
};

export async function readMailForwardingStatus(
  householdId: string,
): Promise<MailForwardingAddressStatus | null> {
  const data = await fetchQuery<HouseholdMailForwardingStatusQuery>(
    getRelayEnvironment(),
    householdMailForwardingStatusQuery,
    { householdId },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("転送先を取得できませんでした");
  const status = data.householdMailForwarding;
  return status?.active ? status : null;
}

export function issueMailForwardingAddress(
  householdId: string,
): Promise<IssuedMailForwardingAddress> {
  return commitGraphQLMutation<IssueOperation, IssuedMailForwardingAddress>(
    issueHouseholdMailForwardingMutation,
    { input: { householdId } },
    (response) => {
      const result = readGraphQLMutationResult(
        response.issueHouseholdMailForwarding,
        "IssueHouseholdMailForwardingSuccess",
        "転送先を発行できませんでした",
        "未対応の転送先発行結果です",
      );
      return { address: result.address, createdAt: result.status.createdAt };
    },
  );
}

export function rotateMailForwardingAddress(
  householdId: string,
): Promise<IssuedMailForwardingAddress> {
  return commitGraphQLMutation<RotateOperation, IssuedMailForwardingAddress>(
    rotateHouseholdMailForwardingMutation,
    { input: { householdId } },
    (response) => {
      const result = readGraphQLMutationResult(
        response.rotateHouseholdMailForwarding,
        "RotateHouseholdMailForwardingSuccess",
        "転送先を再発行できませんでした",
        "未対応の転送先再発行結果です",
      );
      return { address: result.address, createdAt: result.status.createdAt };
    },
  );
}

export function revokeMailForwardingAddress(householdId: string): Promise<void> {
  return commitGraphQLMutation<RevokeOperation, void>(
    revokeHouseholdMailForwardingMutation,
    { input: { householdId } },
    (response) => {
      assertGraphQLMutationSuccess(
        response.revokeHouseholdMailForwarding,
        "RevokeHouseholdMailForwardingSuccess",
        "転送先を無効化できませんでした",
        "未対応の転送先無効化結果です",
      );
    },
  );
}

export const householdMailForwardingStatusQuery = graphql`
  query HouseholdMailForwardingStatusQuery($householdId: ID!) {
    householdMailForwarding(householdId: $householdId) {
      createdAt
      active
    }
  }
`;

export const issueHouseholdMailForwardingMutation = graphql`
  mutation IssueHouseholdMailForwardingMutation($input: HouseholdMailForwardingInput!) {
    issueHouseholdMailForwarding(input: $input) {
      __typename
      ... on IssueHouseholdMailForwardingSuccess {
        result {
          address
          status {
            createdAt
          }
        }
      }
      ... on HouseholdMailForwardingUserError {
        code
        message
      }
    }
  }
`;

export const rotateHouseholdMailForwardingMutation = graphql`
  mutation RotateHouseholdMailForwardingMutation($input: HouseholdMailForwardingInput!) {
    rotateHouseholdMailForwarding(input: $input) {
      __typename
      ... on RotateHouseholdMailForwardingSuccess {
        result {
          address
          status {
            createdAt
          }
        }
      }
      ... on HouseholdMailForwardingUserError {
        code
        message
      }
    }
  }
`;

export const revokeHouseholdMailForwardingMutation = graphql`
  mutation RevokeHouseholdMailForwardingMutation($input: HouseholdMailForwardingInput!) {
    revokeHouseholdMailForwarding(input: $input) {
      __typename
      ... on RevokeHouseholdMailForwardingSuccess {
        result {
          createdAt
          active
        }
      }
      ... on HouseholdMailForwardingUserError {
        code
        message
      }
    }
  }
`;
