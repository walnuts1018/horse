import { useCallback } from "react";

import {
  MutationUserError,
  useTypedGraphqlMutation,
  type CreateAndPostPurchaseMutation,
  type CreatePurchaseInput,
} from "../../../shared/api";
import { mapCreateAndPostPurchaseInput } from "./map-create-and-post-input";
import { createAndPostPurchaseMutation } from "./operations";

function readCreateAndPostResult(response: CreateAndPostPurchaseMutation["response"]) {
  const result = response.createAndPostPurchase;
  switch (result.__typename) {
    case "CreateAndPostPurchaseSuccess":
      return result.result;
    case "CreateAndPostPurchaseUserError":
      throw new MutationUserError(result.message, result.code);
    default:
      throw new Error("購入を登録できませんでした");
  }
}

export function useCreateAndPostPurchase() {
  const submit = useTypedGraphqlMutation<
    CreateAndPostPurchaseMutation,
    ReturnType<typeof readCreateAndPostResult>
  >(createAndPostPurchaseMutation, readCreateAndPostResult);
  return useCallback(
    async (input: CreatePurchaseInput) => {
      const result = await submit.mutate({ input: mapCreateAndPostPurchaseInput(input) });
      return { createPurchase: result };
    },
    [submit.mutate],
  );
}
