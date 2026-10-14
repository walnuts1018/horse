import {
  MutationUserError,
  useTypedGraphqlMutation,
  type BulkUpdatePurchaseItemsMutation,
  type BulkUpdatePurchaseItemsMutation$data,
  type BulkUpdatePurchasesMutation,
  type BulkUpdatePurchasesMutation$data,
  type CorrectPurchaseMutation,
  type CorrectPurchaseMutation$data,
  type CorrectPurchaseFromStatementMutation,
  type CorrectPurchaseFromStatementMutation$data,
  type CreatePurchaseMutation,
  type CreatePurchaseMutation$data,
  type PostPurchaseMutation,
  type PostPurchaseMutation$data,
  type UpdatePurchaseMutation,
  type UpdatePurchaseMutation$data,
} from "../../../shared/api";
import {
  bulkUpdatePurchaseItemsMutation,
  bulkUpdatePurchasesMutation,
  correctPurchaseMutation,
  correctPurchaseFromStatementMutation,
  createPurchaseMutation,
  postPurchaseMutation,
  updatePurchaseMutation,
} from "./operations";

type MutationPayload<TResult> = {
  readonly __typename: string;
  readonly code?: string;
  readonly message?: string;
  readonly result?: TResult | null;
};

function readMutationResult<TResult>(payload: MutationPayload<TResult>, failureMessage: string) {
  if (payload.__typename.endsWith("Success")) {
    if (payload.result == null) throw new Error(failureMessage);
    return payload.result;
  }
  if (payload.__typename.endsWith("Error")) {
    throw new MutationUserError(payload.message ?? failureMessage, payload.code);
  }
  throw new Error(failureMessage);
}

function readMutationCollection<TResult>(
  payload: {
    readonly __typename: string;
    readonly code?: string;
    readonly message?: string;
    readonly purchases?: ReadonlyArray<TResult>;
    readonly items?: ReadonlyArray<TResult>;
  },
  property: "purchases" | "items",
  failureMessage: string,
) {
  if (payload.__typename.endsWith("Success")) {
    const result = property === "purchases" ? payload.purchases : payload.items;
    if (result == null) throw new Error(failureMessage);
    return result;
  }
  throw new MutationUserError(payload.message ?? failureMessage, payload.code);
}

export function useCreatePurchaseMutation() {
  return useTypedGraphqlMutation<
    CreatePurchaseMutation,
    { createPurchase: NonNullable<CreatePurchaseMutation$data["createPurchase"]["result"]> }
  >(createPurchaseMutation, ({ createPurchase }) => ({
    createPurchase: readMutationResult(createPurchase, "買い物を作成できませんでした"),
  }));
}

export function usePostPurchaseMutation() {
  return useTypedGraphqlMutation<
    PostPurchaseMutation,
    { postPurchase: NonNullable<PostPurchaseMutation$data["postPurchase"]["result"]> }
  >(postPurchaseMutation, ({ postPurchase }) => ({
    postPurchase: readMutationResult(postPurchase, "買い物を確定できませんでした"),
  }));
}

export function useUpdatePurchaseMutation() {
  return useTypedGraphqlMutation<
    UpdatePurchaseMutation,
    { updatePurchase: NonNullable<UpdatePurchaseMutation$data["updatePurchase"]["result"]> }
  >(updatePurchaseMutation, ({ updatePurchase }) => ({
    updatePurchase: readMutationResult(updatePurchase, "買い物を更新できませんでした"),
  }));
}

export function useCorrectPurchaseMutation() {
  return useTypedGraphqlMutation<
    CorrectPurchaseMutation,
    { correctPurchase: NonNullable<CorrectPurchaseMutation$data["correctPurchase"]["result"]> }
  >(correctPurchaseMutation, ({ correctPurchase }) => ({
    correctPurchase: readMutationResult(correctPurchase, "返品を記録できませんでした"),
  }));
}

export function useCorrectPurchaseFromStatementMutation() {
  return useTypedGraphqlMutation<
    CorrectPurchaseFromStatementMutation,
    {
      correctPurchaseFromStatement: NonNullable<
        CorrectPurchaseFromStatementMutation$data["correctPurchaseFromStatement"]["result"]
      >;
    }
  >(correctPurchaseFromStatementMutation, ({ correctPurchaseFromStatement }) => ({
    correctPurchaseFromStatement: readMutationResult(
      correctPurchaseFromStatement,
      "返品を明細へ記録できませんでした",
    ),
  }));
}

export function useBulkUpdatePurchasesMutation() {
  return useTypedGraphqlMutation<
    BulkUpdatePurchasesMutation,
    {
      bulkUpdatePurchases: NonNullable<
        BulkUpdatePurchasesMutation$data["bulkUpdatePurchases"]["purchases"]
      >;
    }
  >(bulkUpdatePurchasesMutation, ({ bulkUpdatePurchases }) => ({
    bulkUpdatePurchases: readMutationCollection(
      bulkUpdatePurchases,
      "purchases",
      "買い物を一括更新できませんでした",
    ),
  }));
}

export function useBulkUpdatePurchaseItemsMutation() {
  return useTypedGraphqlMutation<
    BulkUpdatePurchaseItemsMutation,
    {
      bulkUpdatePurchaseItems: NonNullable<
        BulkUpdatePurchaseItemsMutation$data["bulkUpdatePurchaseItems"]["items"]
      >;
    }
  >(bulkUpdatePurchaseItemsMutation, ({ bulkUpdatePurchaseItems }) => ({
    bulkUpdatePurchaseItems: readMutationCollection(
      bulkUpdatePurchaseItems,
      "items",
      "品目を一括更新できませんでした",
    ),
  }));
}
