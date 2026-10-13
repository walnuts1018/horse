import type { ArchiveTagMutation, CreateTagMutation, UpdateTagMutation } from "../../../shared/api";

import { archiveTagMutation, createTagMutation, updateTagMutation } from "../../../entities/tag";
import { commitSettingsMutation } from "./commit-mutation";

export function createTag(input: CreateTagMutation["variables"]["input"]) {
  return commitSettingsMutation<
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
  return commitSettingsMutation<
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
  return commitSettingsMutation<
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
