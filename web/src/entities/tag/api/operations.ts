import { graphql } from "react-relay";

export const tagsQuery = graphql`
  query TagsQuery($householdId: ID!, $includeArchived: Boolean) {
    tags(householdId: $householdId, includeArchived: $includeArchived) {
      id
      householdId
      name
      archived
      revision
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
  mutation ArchiveTagMutation($tagId: ID!, $baseRevision: Int!, $archived: Boolean!) {
    archiveTag(tagId: $tagId, baseRevision: $baseRevision, archived: $archived) {
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
