import { fetchQuery, graphql } from "react-relay";

import {
  commitGraphQLMutation,
  getRelayEnvironment,
  readGraphQLMutationResult,
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

/**
 * Relayのミューテーション実行に必要な型を生成済みデータ型から組み立てる。
 * shared/apiからエクスポートされていない操作型の代わりに利用する。応答を型付きで受け取れるようになる。
 */
type CreateHouseholdInvitationOperation = {
  response: CreateHouseholdInvitationMutation$data;
  variables: { input: { householdId: string; expiresAt: string } };
};

type RevokeHouseholdInvitationOperation = {
  response: RevokeHouseholdInvitationMutation$data;
  variables: { input: { householdId: string; invitationId: string } };
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
  return commitGraphQLMutation<CreateHouseholdInvitationOperation, CreatedHouseholdInvitation>(
    createHouseholdInvitationMutation,
    { input },
    (response) => {
      const result = readGraphQLMutationResult(
        response.createHouseholdInvitation,
        "CreateHouseholdInvitationSuccess",
        "招待を発行できませんでした",
        "未対応の招待発行結果です",
      );
      return {
        token: result.token,
        invitation: mapHouseholdInvitation(result.invitation),
      };
    },
  );
}

export function revokeHouseholdInvitation(input: {
  householdId: string;
  invitationId: string;
}): Promise<void> {
  return commitGraphQLMutation<RevokeHouseholdInvitationOperation, void>(
    revokeHouseholdInvitationMutation,
    { input },
    (response) => {
      readGraphQLMutationResult(
        response.revokeHouseholdInvitation,
        "RevokeHouseholdInvitationSuccess",
        "招待を失効できませんでした",
        "未対応の招待失効結果です",
      );
    },
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
