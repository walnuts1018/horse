import { useCallback } from "react";
import { useGraphqlMutation, type CreatePurchaseInput } from "../../../shared/api";
import { createPurchaseMutation, postPurchaseMutation } from "./operations";

type PurchaseSubmission = {
  createPurchase: { id: string; revision: number; status: string; items: Array<{ id: string }> };
};

type PostedPurchase = { postPurchase: { id: string; revision: number; status: string } };

export function useCreateAndPostPurchase() {
  const create = useGraphqlMutation<PurchaseSubmission>(createPurchaseMutation);
  const post = useGraphqlMutation<PostedPurchase>(postPurchaseMutation);
  return useCallback(
    async (input: CreatePurchaseInput) => {
      const created = await create.mutate({ input });
      if (created.createPurchase.status !== "DRAFT") return created;
      try {
        const posted = await post.mutate({
          purchaseId: created.createPurchase.id,
          expectedRevision: created.createPurchase.revision,
          idempotencyKey: input.idempotencyKey,
        });
        return {
          createPurchase: {
            ...created.createPurchase,
            revision: posted.postPurchase.revision,
            status: posted.postPurchase.status,
          },
        };
      } catch (reason) {
        const latest = await create.mutate({ input });
        if (latest.createPurchase.status !== "DRAFT") return latest;
        throw reason;
      }
    },
    [create.mutate, post.mutate],
  );
}
