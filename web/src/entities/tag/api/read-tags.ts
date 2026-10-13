import { fetchQuery } from "react-relay";

import {
  getRelayEnvironment,
  type Tag,
  type TagsQuery,
  type TagsQuery$data,
} from "../../../shared/api";
import { tagsQuery } from "./operations";

type TagRecord = TagsQuery$data["tags"]["edges"][number]["node"];

export async function readTags(householdId: string, includeArchived = false): Promise<Tag[]> {
  const tags: Tag[] = [];
  let after: string | null = null;
  for (;;) {
    const result: TagsQuery$data | undefined = await fetchQuery<TagsQuery>(
      getRelayEnvironment(),
      tagsQuery,
      { householdId, includeArchived, first: 100, after },
      { fetchPolicy: "network-only" },
    ).toPromise();
    if (!result) throw new Error("タグを取得できませんでした");
    tags.push(...result.tags.edges.map(({ node }) => mapTag(node)));
    const endCursor: string | null | undefined = result.tags.pageInfo.endCursor;
    if (!result.tags.pageInfo.hasNextPage || !endCursor || endCursor === after) return tags;
    after = endCursor;
  }
}

function mapTag(record: TagRecord): Tag {
  return {
    id: record.id,
    householdId: record.householdId,
    name: record.name,
    archived: record.archived,
    revision: record.revision,
  };
}
