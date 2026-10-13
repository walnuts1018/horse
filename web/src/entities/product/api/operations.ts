import { graphql } from "react-relay";

export const productCatalogQuery = graphql`
  query ProductCatalogQuery($householdId: ID!, $first: Int!, $after: String) {
    productCatalog(householdId: $householdId, first: $first, after: $after) {
      edges {
        cursor
        node {
          id
          householdId
          name
          normalizedName
          categoryId
          archived
          revision
          variants {
            id
            householdId
            conceptId
            name
            normalizedName
            brand
            packageSize
            barcode
            archived
            revision
            aliases {
              id
              alias
              merchantKey
              revision
            }
          }
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
    }
  }
`;

export const productSuggestionsQuery = graphql`
  query ProductSuggestionsQuery(
    $householdId: ID!
    $query: String!
    $merchant: String
    $first: Int!
  ) {
    productSuggestions(
      householdId: $householdId
      query: $query
      merchant: $merchant
      first: $first
    ) {
      concept {
        id
        name
        categoryId
      }
      variant {
        id
        name
        brand
        packageSize
      }
      matchedName
      score
    }
  }
`;

export const createProductConceptMutation = graphql`
  mutation CreateProductConceptMutation($input: CreateProductConceptInput!) {
    createProductConcept(input: $input) {
      __typename
      ... on CreateProductConceptSuccess {
        result {
          id
          name
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const createProductVariantMutation = graphql`
  mutation CreateProductVariantMutation($input: CreateProductVariantInput!) {
    createProductVariant(input: $input) {
      __typename
      ... on CreateProductVariantSuccess {
        result {
          id
          name
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export const setProductAliasMutation = graphql`
  mutation SetProductAliasMutation($input: SetProductAliasInput!) {
    setProductAlias(input: $input) {
      __typename
      ... on SetProductAliasSuccess {
        result {
          id
          alias
          merchantKey
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
