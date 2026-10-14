import { useCallback } from "react";

import type {
  SavePurchaseItemEditsMutation,
  SavePurchaseItemEditsMutation$data,
} from "../../../shared/api/__generated__/SavePurchaseItemEditsMutation.graphql";

import { MutationUserError, useTypedGraphqlMutation } from "../../../shared/api";
import { savePurchaseItemEditsMutation } from "./operations";

type SavePurchaseItemEditsSuccess = Extract<
  SavePurchaseItemEditsMutation$data["savePurchaseItemEdits"],
  { readonly __typename: "SavePurchaseItemEditsSuccess" }
>;

export function useSavePurchaseItemEdits() {
  const save = useTypedGraphqlMutation<
    SavePurchaseItemEditsMutation,
    {
      savePurchaseItemEdits: SavePurchaseItemEditsSuccess["items"];
    }
  >(savePurchaseItemEditsMutation, ({ savePurchaseItemEdits }) => {
    if (savePurchaseItemEdits.__typename === "SavePurchaseItemEditsSuccess") {
      return { savePurchaseItemEdits: savePurchaseItemEdits.items };
    }
    if (savePurchaseItemEdits.__typename === "SavePurchaseItemEditsUserError")
      throw new MutationUserError(savePurchaseItemEdits.message, savePurchaseItemEdits.code);
    throw new Error("品目を保存できませんでした");
  });
  return useCallback(
    (variables: SavePurchaseItemEditsMutation["variables"]) => save.mutate(variables),
    [save],
  );
}
