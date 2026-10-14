import {
  commitGraphQLMutation,
  readGraphQLMutationResult,
  type CreateCategoryMutation,
  type CreateCategoryMutation$variables,
  type SetCategoryArchivedMutation,
  type SetCategoryArchivedMutation$variables,
  type UpdateCategoryNameMutation,
  type UpdateCategoryNameMutation$variables,
  type SetDefaultAllocationMutation,
  type SetDefaultAllocationMutation$variables,
} from "../../../shared/api";
import {
  createCategoryMutation,
  setCategoryArchivedMutation,
  updateCategoryNameMutation,
  setDefaultAllocationMutation,
} from "./operations";

export function createCategory(input: CreateCategoryMutation$variables["input"]): Promise<void> {
  return commitGraphQLMutation<CreateCategoryMutation, void>(
    createCategoryMutation,
    { input },
    (response) => {
      readGraphQLMutationResult(
        response.createCategory,
        "CreateCategorySuccess",
        "カテゴリを作成できませんでした",
        "未対応のカテゴリ作成結果です",
      );
    },
  );
}

export function setCategoryArchived(
  input: SetCategoryArchivedMutation$variables["input"],
): Promise<void> {
  return commitGraphQLMutation<SetCategoryArchivedMutation, void>(
    setCategoryArchivedMutation,
    { input },
    (response) => {
      readGraphQLMutationResult(
        response.setCategoryArchived,
        "SetCategoryArchivedSuccess",
        "カテゴリを更新できませんでした",
        "未対応のカテゴリ更新結果です",
      );
    },
  );
}

export function updateCategoryName(
  input: UpdateCategoryNameMutation$variables["input"],
): Promise<void> {
  return commitGraphQLMutation<UpdateCategoryNameMutation, void>(
    updateCategoryNameMutation,
    { input },
    (response) => {
      readGraphQLMutationResult(
        response.updateCategoryName,
        "UpdateCategoryNameSuccess",
        "カテゴリ名を更新できませんでした",
        "未対応のカテゴリ名更新結果です",
      );
    },
  );
}

export function setDefaultAllocation(
  input: SetDefaultAllocationMutation$variables["input"],
): Promise<void> {
  return commitGraphQLMutation<SetDefaultAllocationMutation, void>(
    setDefaultAllocationMutation,
    { input },
    (response) => {
      readGraphQLMutationResult(
        response.setDefaultAllocation,
        "SetDefaultAllocationSuccess",
        "標準の負担割合を保存できませんでした",
        "未対応の標準負担割合の保存結果です",
      );
    },
  );
}
