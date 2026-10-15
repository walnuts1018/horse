import {
  commitGraphQLMutation,
  type CreateSharedPurchaseDraftMutation,
  type UpdateSharedPurchaseDraftMutation,
} from "../../../shared/api";
import {
  createSharedPurchaseDraftMutation,
  updateSharedPurchaseDraftMutation,
} from "../model/shared-purchase-draft-operations";

export type SharedPurchaseDraftRecord = {
  id: string;
  householdId: string;
  revision: number;
  payload: string;
  createdAt: string;
  updatedAt: string;
};

export function createSharedPurchaseDraft(householdId: string, payload: string) {
  return commitGraphQLMutation<CreateSharedPurchaseDraftMutation, SharedPurchaseDraftRecord>(
    createSharedPurchaseDraftMutation,
    { householdId, payload },
    (response) => {
      const result = response.createSharedPurchaseDraft;
      if (result.__typename === "CreateSharedPurchaseDraftSuccess") return result.result;
      if (result.__typename === "SharedPurchaseDraftUserError") throw new Error(result.message);
      throw new Error("共有下書きを作成できません");
    },
  );
}

export function updateSharedPurchaseDraft(input: {
  id: string;
  expectedRevision: number;
  payload: string;
}) {
  return commitGraphQLMutation<
    UpdateSharedPurchaseDraftMutation,
    { conflict: boolean; draft: SharedPurchaseDraftRecord }
  >(updateSharedPurchaseDraftMutation, input, (response) => {
    const result = response.updateSharedPurchaseDraft;
    if (result.__typename === "UpdateSharedPurchaseDraftSuccess") {
      return { conflict: false, draft: result.result };
    }
    if (result.__typename === "UpdateSharedPurchaseDraftConflict") {
      return { conflict: true, draft: result.draft };
    }
    if (result.__typename === "SharedPurchaseDraftUserError") throw new Error(result.message);
    throw new Error("共有下書きを保存できません");
  });
}
