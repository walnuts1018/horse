import { readGraphql, type Tag } from "../../../shared/api";
import { tagsQuery } from "./operations";

type TagsPage = {
  tags: {
    edges: Array<{ cursor: string; node: Tag }>;
    pageInfo: { endCursor?: string | null; hasNextPage: boolean };
  };
};

export async function readTags(householdId: string, includeArchived = false): Promise<Tag[]> {
  const tags: Tag[] = [];
  let after: string | null = null;
  for (;;) {
    const result: TagsPage = await readGraphql<TagsPage>(tagsQuery, {
      householdId,
      includeArchived,
      first: 100,
      after,
    });
    tags.push(...result.tags.edges.map((edge: TagsPage["tags"]["edges"][number]) => edge.node));
    const pageInfo: TagsPage["tags"]["pageInfo"] = result.tags.pageInfo;
    const endCursor: string | null | undefined = pageInfo.endCursor;
    if (!pageInfo.hasNextPage || !endCursor || endCursor === after) return tags;
    after = endCursor;
  }
}
