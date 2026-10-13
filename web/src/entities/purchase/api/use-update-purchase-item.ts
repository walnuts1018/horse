import { useCallback } from "react";
import { useGraphqlMutation, type UpdatePurchaseItemMutation$variables } from "../../../shared/api";
import { updatePurchaseItemMutation } from "./operations";

export function useUpdatePurchaseItem() {
  const update = useGraphqlMutation(updatePurchaseItemMutation);
  return useCallback(
    (variables: UpdatePurchaseItemMutation$variables) => update.mutate(variables),
    [update.mutate],
  );
}
