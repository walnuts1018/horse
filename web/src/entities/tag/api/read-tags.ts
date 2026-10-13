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
    const result = await readGraphql<TagsPage>(tagsQuery, {
      householdId,
      includeArchived,
      first: 100,
      after,
    });
    tags.push(...result.tags.edges.map((edge) => edge.node));
    const pageInfo = result.tags.pageInfo;
    if (!pageInfo.hasNextPage || !pageInfo.endCursor || pageInfo.endCursor === after) return tags;
    after = pageInfo.endCursor;
  }
}
