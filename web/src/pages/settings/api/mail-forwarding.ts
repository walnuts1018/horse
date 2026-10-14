import { commitMutation, fetchQuery, graphql } from "react-relay";

import {
  getRelayEnvironment,
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
  return new Promise((resolve, reject) =>
    commitMutation<IssueOperation>(getRelayEnvironment(), {
      mutation: issueHouseholdMailForwardingMutation,
      variables: { input: { householdId } },
      onCompleted: (response, errors) => {
        if (errors?.length) return reject(new Error(errors[0].message));
        const result = response.issueHouseholdMailForwarding;
        if (result.__typename === "IssueHouseholdMailForwardingSuccess") {
          resolve({
            address: result.result.address,
            createdAt: result.result.status.createdAt,
          });
          return;
        }
        reject(
          new Error(
            result.__typename === "HouseholdMailForwardingUserError"
              ? result.message
              : `未対応の転送先発行結果です: ${result.__typename}`,
          ),
        );
      },
      onError: reject,
    }),
  );
}

export function rotateMailForwardingAddress(
  householdId: string,
): Promise<IssuedMailForwardingAddress> {
  return new Promise((resolve, reject) =>
    commitMutation<RotateOperation>(getRelayEnvironment(), {
      mutation: rotateHouseholdMailForwardingMutation,
      variables: { input: { householdId } },
      onCompleted: (response, errors) => {
        if (errors?.length) return reject(new Error(errors[0].message));
        const result = response.rotateHouseholdMailForwarding;
        if (result.__typename === "RotateHouseholdMailForwardingSuccess") {
          resolve({
            address: result.result.address,
            createdAt: result.result.status.createdAt,
          });
          return;
        }
        reject(
          new Error(
            result.__typename === "HouseholdMailForwardingUserError"
              ? result.message
              : `未対応の転送先再発行結果です: ${result.__typename}`,
          ),
        );
      },
      onError: reject,
    }),
  );
}

export function revokeMailForwardingAddress(householdId: string): Promise<void> {
  return new Promise((resolve, reject) =>
    commitMutation<RevokeOperation>(getRelayEnvironment(), {
      mutation: revokeHouseholdMailForwardingMutation,
      variables: { input: { householdId } },
      onCompleted: (response, errors) => {
        if (errors?.length) return reject(new Error(errors[0].message));
        const result = response.revokeHouseholdMailForwarding;
        if (result.__typename === "RevokeHouseholdMailForwardingSuccess") {
          resolve();
          return;
        }
        reject(
          new Error(
            result.__typename === "HouseholdMailForwardingUserError"
              ? result.message
              : `未対応の転送先無効化結果です: ${result.__typename}`,
          ),
        );
      },
      onError: reject,
    }),
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
