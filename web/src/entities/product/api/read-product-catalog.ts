import { fetchQuery } from "react-relay";
import {
  getRelayEnvironment,
  type ProductAlias,
  type ProductConcept,
  type ProductCatalogQuery,
  type ProductCatalogQuery$data,
  type ProductVariant,
} from "../../../shared/api";
import { productCatalogQuery } from "./operations";

type ProductConceptRecord = ProductCatalogQuery$data["productCatalog"]["edges"][number]["node"];
type ProductVariantRecord = ProductConceptRecord["variants"][number];
type ProductAliasRecord = ProductVariantRecord["aliases"][number];

export async function readProductCatalog(householdId: string): Promise<ProductConcept[]> {
  const products: ProductConcept[] = [];
  let after: string | null = null;
  for (;;) {
    const result: ProductCatalogQuery$data | undefined = await fetchQuery<ProductCatalogQuery>(
      getRelayEnvironment(),
      productCatalogQuery,
      { householdId, first: 100, after },
      { fetchPolicy: "network-only" },
    ).toPromise();
    if (!result) throw new Error("商品一覧を取得できませんでした");
    products.push(...result.productCatalog.edges.map(({ node }) => mapProductConcept(node)));
    const pageInfo: ProductCatalogQuery$data["productCatalog"]["pageInfo"] =
      result.productCatalog.pageInfo;
    const endCursor: string | null | undefined = pageInfo.endCursor;
    if (!pageInfo.hasNextPage || !endCursor || endCursor === after) return products;
    after = endCursor;
  }
}

function mapProductConcept(record: ProductConceptRecord): ProductConcept {
  return {
    id: record.id,
    householdId: record.householdId,
    name: record.name,
    normalizedName: record.normalizedName,
    categoryId: record.categoryId,
    archived: record.archived,
    revision: record.revision,
    variants: record.variants.map(mapProductVariant),
  };
}

function mapProductVariant(record: ProductVariantRecord): ProductVariant {
  return {
    id: record.id,
    householdId: record.householdId,
    conceptId: record.conceptId,
    name: record.name,
    normalizedName: record.normalizedName,
    brand: record.brand,
    packageSize: record.packageSize,
    barcode: record.barcode,
    archived: record.archived,
    revision: record.revision,
    aliases: record.aliases.map(mapProductAlias),
  };
}

function mapProductAlias(record: ProductAliasRecord): ProductAlias {
  return {
    id: record.id,
    alias: record.alias,
    merchantKey: record.merchantKey,
    revision: record.revision,
  };
}
