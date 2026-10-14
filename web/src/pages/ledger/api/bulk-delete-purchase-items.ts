import { graphql } from "react-relay";

import type { BulkDeletePurchaseItemsMutation } from "../../../shared/api/__generated__/BulkDeletePurchaseItemsMutation.graphql";

import { MutationUserError, useTypedGraphqlMutation } from "../../../shared/api";

const bulkDeletePurchaseItemsMutation = graphql`
  mutation BulkDeletePurchaseItemsMutation($input: BulkDeletePurchaseItemsInput!) {
    bulkDeletePurchaseItems(input: $input) {
      __typename
      ... on BulkDeletePurchaseItemsSuccess {
        deletedItemIds
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

export function useBulkDeletePurchaseItemsMutation() {
  return useTypedGraphqlMutation<
    BulkDeletePurchaseItemsMutation,
    { deletedItemIds: ReadonlyArray<string> }
  >(bulkDeletePurchaseItemsMutation, ({ bulkDeletePurchaseItems }) => {
    if (bulkDeletePurchaseItems.__typename === "BulkDeletePurchaseItemsSuccess") {
      if (!bulkDeletePurchaseItems.deletedItemIds) {
        throw new Error("削除された品目を確認できませんでした");
      }
      return { deletedItemIds: bulkDeletePurchaseItems.deletedItemIds };
    }
    throw new MutationUserError(
      bulkDeletePurchaseItems.message ?? "品目を削除できませんでした",
      bulkDeletePurchaseItems.code,
    );
  });
}
