import { fetchQuery, graphql } from "react-relay";
import {
  getRelayEnvironment,
  type HouseholdInvitationsQuery,
  type HouseholdInvitationsQuery$data,
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
