import type {
  ArchiveRecurringExpenseMutation,
  CreateRecurringExpenseMutation,
  UpdateRecurringExpenseMutation,
} from "../../../shared/api";

import { commitGraphQLMutationResult } from "../../../shared/api";
import {
  archiveRecurringExpenseMutation,
  createRecurringExpenseMutation,
  updateRecurringExpenseMutation,
} from "./recurring-expense-operations";

export function createRecurringExpense(
  input: CreateRecurringExpenseMutation["variables"]["input"],
) {
  return commitGraphQLMutationResult<
    CreateRecurringExpenseMutation,
    NonNullable<CreateRecurringExpenseMutation["response"]["createRecurringExpense"]["result"]>
  >(
    createRecurringExpenseMutation,
    { input },
    (response) => response.createRecurringExpense,
    "CreateRecurringExpenseSuccess",
    "定期支出を作成できませんでした",
  );
}

export function updateRecurringExpense(
  input: UpdateRecurringExpenseMutation["variables"]["input"],
) {
  return commitGraphQLMutationResult<
    UpdateRecurringExpenseMutation,
    NonNullable<UpdateRecurringExpenseMutation["response"]["updateRecurringExpense"]["result"]>
  >(
    updateRecurringExpenseMutation,
    { input },
    (response) => response.updateRecurringExpense,
    "UpdateRecurringExpenseSuccess",
    "定期支出を更新できませんでした",
  );
}

export function archiveRecurringExpense(
  input: ArchiveRecurringExpenseMutation["variables"]["input"],
) {
  return commitGraphQLMutationResult<
    ArchiveRecurringExpenseMutation,
    NonNullable<ArchiveRecurringExpenseMutation["response"]["archiveRecurringExpense"]["result"]>
  >(
    archiveRecurringExpenseMutation,
    { input },
    (response) => response.archiveRecurringExpense,
    "ArchiveRecurringExpenseSuccess",
    "定期支出の表示状態を変更できませんでした",
  );
}
