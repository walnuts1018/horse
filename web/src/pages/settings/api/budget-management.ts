import type {
  ArchiveBudgetMutation,
  CreateBudgetMutation,
  UpdateBudgetMutation,
} from "../../../shared/api";
import {
  archiveBudgetMutation,
  createBudgetMutation,
  updateBudgetMutation,
} from "../../../entities/budget";
import { commitSettingsMutation } from "./commit-mutation";

export function createBudget(input: CreateBudgetMutation["variables"]["input"]) {
  return commitSettingsMutation<
    CreateBudgetMutation,
    NonNullable<CreateBudgetMutation["response"]["createBudget"]["result"]>
  >(
    createBudgetMutation,
    { input },
    (response) => response.createBudget,
    "CreateBudgetSuccess",
    "予算を作成できませんでした",
  );
}

export function updateBudget(input: UpdateBudgetMutation["variables"]["input"]) {
  return commitSettingsMutation<
    UpdateBudgetMutation,
    NonNullable<UpdateBudgetMutation["response"]["updateBudget"]["result"]>
  >(
    updateBudgetMutation,
    { input },
    (response) => response.updateBudget,
    "UpdateBudgetSuccess",
    "予算を更新できませんでした",
  );
}

export function archiveBudget(input: ArchiveBudgetMutation["variables"]["input"]) {
  return commitSettingsMutation<
    ArchiveBudgetMutation,
    NonNullable<ArchiveBudgetMutation["response"]["archiveBudget"]["result"]>
  >(
    archiveBudgetMutation,
    { input },
    (response) => response.archiveBudget,
    "ArchiveBudgetSuccess",
    "予算の表示状態を変更できませんでした",
  );
}
