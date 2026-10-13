import { useCallback } from "react";
import {
  useGraphqlMutation,
  type CreateAndPostPurchaseMutation$data,
  type CreatePurchaseInput,
} from "../../../shared/api";
import { mapCreateAndPostPurchaseInput } from "./map-create-and-post-input";
import { createAndPostPurchaseMutation } from "./operations";

type PurchaseSubmission = {
  createAndPostPurchase: Extract<
    CreateAndPostPurchaseMutation$data["createAndPostPurchase"],
    { readonly __typename: "CreateAndPostPurchaseSuccess" }
  >["result"];
};

export function useCreateAndPostPurchase() {
  const submit = useGraphqlMutation<PurchaseSubmission>(createAndPostPurchaseMutation);
  return useCallback(
    async (input: CreatePurchaseInput) => {
      const result = await submit.mutate({ input: mapCreateAndPostPurchaseInput(input) });
      return { createPurchase: result.createAndPostPurchase };
    },
    [submit.mutate],
  );
}
