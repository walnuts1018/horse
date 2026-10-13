import { graphql } from "react-relay";

export const tagsQuery = graphql`
  query TagsQuery($householdId: ID!, $includeArchived: Boolean!, $first: Int!, $after: String) {
    tags(
      householdId: $householdId
      includeArchived: $includeArchived
      first: $first
      after: $after
    ) {
      edges {
        cursor
        node {
          id
          householdId
          name
          archived
          revision
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
    }
  }
`;

export const createTagMutation = graphql`
  mutation CreateTagMutation($input: CreateTagInput!) {
    createTag(input: $input) {
      __typename
      ... on CreateTagSuccess {
        result {
          id
          householdId
          name
          archived
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

export const updateTagMutation = graphql`
  mutation UpdateTagMutation($input: UpdateTagInput!) {
    updateTag(input: $input) {
      __typename
      ... on UpdateTagSuccess {
        result {
          id
          householdId
          name
          archived
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

export const archiveTagMutation = graphql`
  mutation ArchiveTagMutation($input: ArchiveTagInput!) {
    archiveTag(input: $input) {
      __typename
      ... on ArchiveTagSuccess {
        result {
          id
          archived
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
