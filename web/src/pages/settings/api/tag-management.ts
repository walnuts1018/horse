import { archiveTagMutation, createTagMutation, updateTagMutation } from "../../../entities/tag";
import {
  commitGraphQLMutationResult,
  type ArchiveTagMutation,
  type CreateTagMutation,
  type UpdateTagMutation,
} from "../../../shared/api";

export function createTag(input: CreateTagMutation["variables"]["input"]) {
  return commitGraphQLMutationResult<
    CreateTagMutation,
    NonNullable<CreateTagMutation["response"]["createTag"]["result"]>
  >(
    createTagMutation,
    { input },
    (response) => response.createTag,
    "CreateTagSuccess",
    "タグを作成できませんでした",
  );
}

export function updateTag(input: UpdateTagMutation["variables"]["input"]) {
  return commitGraphQLMutationResult<
    UpdateTagMutation,
    NonNullable<UpdateTagMutation["response"]["updateTag"]["result"]>
  >(
    updateTagMutation,
    { input },
    (response) => response.updateTag,
    "UpdateTagSuccess",
    "タグを更新できませんでした",
  );
}

export function archiveTag(input: ArchiveTagMutation["variables"]["input"]) {
  return commitGraphQLMutationResult<
    ArchiveTagMutation,
    NonNullable<ArchiveTagMutation["response"]["archiveTag"]["result"]>
  >(
    archiveTagMutation,
    { input },
    (response) => response.archiveTag,
    "ArchiveTagSuccess",
    "タグの表示状態を変更できませんでした",
  );
}
