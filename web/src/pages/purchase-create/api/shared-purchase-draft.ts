import type { CreateSharedPurchaseDraftMutation } from "../../../shared/api/__generated__/CreateSharedPurchaseDraftMutation.graphql";
import type { UpdateSharedPurchaseDraftMutation } from "../../../shared/api/__generated__/UpdateSharedPurchaseDraftMutation.graphql";

import { commitGraphQLMutation } from "../../../shared/api";
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
      return response.createSharedPurchaseDraft;
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
    return response.updateSharedPurchaseDraft;
  });
}
