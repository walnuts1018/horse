import { commitMutation } from "react-relay";
import {
  getRelayEnvironment,
  type CreateCategoryMutation,
  type CreateCategoryMutation$variables,
  type SetCategoryArchivedMutation,
  type SetCategoryArchivedMutation$variables,
  type SetDefaultAllocationMutation,
  type SetDefaultAllocationMutation$variables,
} from "../../../shared/api";
import {
  createCategoryMutation,
  setCategoryArchivedMutation,
  setDefaultAllocationMutation,
} from "./operations";

export function createCategory(input: CreateCategoryMutation$variables["input"]): Promise<void> {
  return new Promise((resolve, reject) =>
    commitMutation<CreateCategoryMutation>(getRelayEnvironment(), {
      mutation: createCategoryMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new Error(errors[0].message));
          return;
        }
        const result = response.createCategory;
        switch (result.__typename) {
          case "CreateCategorySuccess":
            if (!result.result) {
              reject(new Error("カテゴリを作成できませんでした"));
              return;
            }
            resolve();
            return;
          case "MutationError":
            reject(new Error(result.message ?? "カテゴリを作成できませんでした"));
            return;
          default:
            reject(new Error(`未対応のカテゴリ作成結果です: ${result.__typename}`));
        }
      },
      onError: reject,
    }),
  );
}

export function setCategoryArchived(
  input: SetCategoryArchivedMutation$variables["input"],
): Promise<void> {
  return new Promise((resolve, reject) =>
    commitMutation<SetCategoryArchivedMutation>(getRelayEnvironment(), {
      mutation: setCategoryArchivedMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new Error(errors[0].message));
          return;
        }
        const result = response.setCategoryArchived;
        switch (result.__typename) {
          case "SetCategoryArchivedSuccess":
            if (!result.result) {
              reject(new Error("カテゴリを更新できませんでした"));
              return;
            }
            resolve();
            return;
          case "MutationError":
            reject(new Error(result.message ?? "カテゴリを更新できませんでした"));
            return;
          default:
            reject(new Error(`未対応のカテゴリ更新結果です: ${result.__typename}`));
        }
      },
      onError: reject,
    }),
  );
}

export function setDefaultAllocation(
  input: SetDefaultAllocationMutation$variables["input"],
): Promise<void> {
  return new Promise((resolve, reject) =>
    commitMutation<SetDefaultAllocationMutation>(getRelayEnvironment(), {
      mutation: setDefaultAllocationMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new Error(errors[0].message));
          return;
        }
        const result = response.setDefaultAllocation;
        switch (result.__typename) {
          case "SetDefaultAllocationSuccess":
            if (!result.result) {
              reject(new Error("標準の負担割合を保存できませんでした"));
              return;
            }
            resolve();
            return;
          case "MutationError":
            reject(new Error(result.message ?? "標準の負担割合を保存できませんでした"));
            return;
          default:
            reject(new Error(`未対応の標準負担割合の保存結果です: ${result.__typename}`));
        }
      },
      onError: reject,
    }),
  );
}
