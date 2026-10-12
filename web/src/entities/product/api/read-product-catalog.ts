import { readGraphql, type ProductConcept } from "../../../shared/api";
import { productCatalogQuery } from "./operations";

type ProductCatalogPage = {
  productCatalog: {
    edges: Array<{ cursor: string; node: ProductConcept }>;
    pageInfo: { endCursor?: string | null; hasNextPage: boolean };
  };
};

export async function readProductCatalog(householdId: string): Promise<ProductConcept[]> {
  const products: ProductConcept[] = [];
  let after: string | null = null;
  for (;;) {
    const result: ProductCatalogPage = await readGraphql<ProductCatalogPage>(productCatalogQuery, {
      householdId,
      first: 100,
      after,
    });
    products.push(...result.productCatalog.edges.map((edge) => edge.node));
    const pageInfo = result.productCatalog.pageInfo;
    if (!pageInfo.hasNextPage || !pageInfo.endCursor || pageInfo.endCursor === after)
      return products;
    after = pageInfo.endCursor;
  }
}
