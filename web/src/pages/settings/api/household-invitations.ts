import { commitMutation, fetchQuery, graphql } from "react-relay";

import {
  getRelayEnvironment,
  type CreateHouseholdInvitationMutation$data,
  type HouseholdInvitationsQuery,
  type HouseholdInvitationsQuery$data,
  type RevokeHouseholdInvitationMutation$data,
} from "../../../shared/api";

export type HouseholdInvitation = {
  __typename:
    | "PendingHouseholdInvitation"
    | "AcceptedHouseholdInvitation"
    | "RevokedHouseholdInvitation"
    | "ExpiredHouseholdInvitation";
  id: string;
  expiresAt: string;
};

export type HouseholdInvitationPage = {
  invitations: HouseholdInvitation[];
  endCursor: string | null;
  hasNextPage: boolean;
};

export type CreatedHouseholdInvitation = {
  token: string;
  invitation: HouseholdInvitation;
};

type HouseholdInvitationRecord =
  HouseholdInvitationsQuery$data["householdInvitations"]["edges"][number]["node"];

export async function readHouseholdInvitations(
  householdId: string,
  after?: string,
): Promise<HouseholdInvitationPage> {
  const data = await fetchQuery<HouseholdInvitationsQuery>(
    getRelayEnvironment(),
    householdInvitationsQuery,
    { householdId, after, first: 50 },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("招待一覧を取得できませんでした");
  return {
    invitations: data.householdInvitations.edges.map(({ node }) => mapHouseholdInvitation(node)),
    endCursor: data.householdInvitations.pageInfo.endCursor ?? null,
    hasNextPage: data.householdInvitations.pageInfo.hasNextPage,
  };
}

export function createHouseholdInvitation(input: {
  householdId: string;
  expiresAt: string;
}): Promise<CreatedHouseholdInvitation> {
  return new Promise((resolve, reject) =>
    commitMutation(getRelayEnvironment(), {
      mutation: createHouseholdInvitationMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new Error(errors[0].message));
          return;
        }
        const result = (response as CreateHouseholdInvitationMutation$data)
          .createHouseholdInvitation;
        switch (result.__typename) {
          case "CreateHouseholdInvitationSuccess": {
            if (!result.result) {
              reject(new Error("招待を発行できませんでした"));
              return;
            }
            resolve({
              token: result.result.token,
              invitation: mapHouseholdInvitation(result.result.invitation),
            });
            return;
          }
          case "MutationError":
            reject(new Error(result.message ?? "招待を発行できませんでした"));
            return;
          default:
            reject(new Error(`未対応の招待発行結果です: ${result.__typename}`));
        }
      },
      onError: reject,
    }),
  );
}

export function revokeHouseholdInvitation(input: {
  householdId: string;
  invitationId: string;
}): Promise<void> {
  return new Promise((resolve, reject) =>
    commitMutation(getRelayEnvironment(), {
      mutation: revokeHouseholdInvitationMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new Error(errors[0].message));
          return;
        }
        const result = (response as RevokeHouseholdInvitationMutation$data)
          .revokeHouseholdInvitation;
        switch (result.__typename) {
          case "RevokeHouseholdInvitationSuccess":
            if (!result.result) {
              reject(new Error("招待を失効できませんでした"));
              return;
            }
            resolve();
            return;
          case "MutationError":
            reject(new Error(result.message ?? "招待を失効できませんでした"));
            return;
          default:
            reject(new Error(`未対応の招待失効結果です: ${result.__typename}`));
        }
      },
      onError: reject,
    }),
  );
}

function mapHouseholdInvitation(record: HouseholdInvitationRecord): HouseholdInvitation {
  switch (record.__typename) {
    case "PendingHouseholdInvitation":
    case "AcceptedHouseholdInvitation":
    case "RevokedHouseholdInvitation":
    case "ExpiredHouseholdInvitation":
      return {
        __typename: record.__typename,
        id: record.id,
        expiresAt: record.expiresAt,
      };
    default:
      throw new Error(`未対応の招待型です: ${record.__typename}`);
  }
}

export const householdInvitationsQuery = graphql`
  query HouseholdInvitationsQuery($householdId: ID!, $after: String, $first: Int) {
    householdInvitations(householdId: $householdId, after: $after, first: $first) {
      edges {
        cursor
        node {
          __typename
          id
          householdId
          expiresAt
          createdAt
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

export const createHouseholdInvitationMutation = graphql`
  mutation CreateHouseholdInvitationMutation($input: CreateHouseholdInvitationInput!) {
    createHouseholdInvitation(input: $input) {
      __typename
      ... on CreateHouseholdInvitationSuccess {
        result {
          token
          invitation {
            __typename
            id
            householdId
            expiresAt
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

export const revokeHouseholdInvitationMutation = graphql`
  mutation RevokeHouseholdInvitationMutation($input: RevokeHouseholdInvitationInput!) {
    revokeHouseholdInvitation(input: $input) {
      __typename
      ... on RevokeHouseholdInvitationSuccess {
        result {
          __typename
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
