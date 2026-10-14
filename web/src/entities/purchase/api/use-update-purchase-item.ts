import { useCallback } from "react";

import {
  MutationUserError,
  useTypedGraphqlMutation,
  type UpdatePurchaseItemMutation,
  type UpdatePurchaseItemMutation$data,
} from "../../../shared/api";
import { updatePurchaseItemMutation } from "./operations";

export function useUpdatePurchaseItem() {
  const update = useTypedGraphqlMutation<
    UpdatePurchaseItemMutation,
    {
      updatePurchaseItem: NonNullable<
        UpdatePurchaseItemMutation$data["updatePurchaseItem"]["result"]
      >;
    }
  >(updatePurchaseItemMutation, ({ updatePurchaseItem }) => {
    if (updatePurchaseItem.__typename.endsWith("Success")) {
      if (updatePurchaseItem.result == null) throw new Error("品目を更新できませんでした");
      return { updatePurchaseItem: updatePurchaseItem.result };
    }
    throw new MutationUserError(
      updatePurchaseItem.message ?? "品目を更新できませんでした",
      updatePurchaseItem.code,
    );
  });
  return useCallback(
    (variables: UpdatePurchaseItemMutation["variables"]) => update.mutate(variables),
    [update],
  );
}
