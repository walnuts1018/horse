import { fetchQuery } from "react-relay";

import {
  getRelayEnvironment,
  type ProductSuggestionsQuery,
  type ProductSuggestionsQuery$data,
} from "../../../shared/api";
import { productSuggestionsQuery } from "./operations";

export type ProductSuggestion = {
  concept: {
    id: string;
    name: string;
    categoryId?: string | null;
  };
  variant: {
    id: string;
    name: string;
    brand?: string | null;
    packageSize?: string | null;
  };
  matchedName: string;
  score: number;
};

type ProductSuggestionRecord = ProductSuggestionsQuery$data["productSuggestions"][number];

export async function readProductSuggestions(
  householdId: string,
  query: string,
  merchant: string | null,
  first: number,
): Promise<ProductSuggestion[]> {
  const result: ProductSuggestionsQuery$data | undefined =
    await fetchQuery<ProductSuggestionsQuery>(
      getRelayEnvironment(),
      productSuggestionsQuery,
      { householdId, query, merchant, first },
      { fetchPolicy: "store-or-network" },
    ).toPromise();
  if (!result) throw new Error("商品候補を取得できませんでした");
  return result.productSuggestions.map(mapProductSuggestion);
}

function mapProductSuggestion(record: ProductSuggestionRecord): ProductSuggestion {
  return {
    concept: {
      id: record.concept.id,
      name: record.concept.name,
      categoryId: record.concept.categoryId,
    },
    variant: {
      id: record.variant.id,
      name: record.variant.name,
      brand: record.variant.brand,
      packageSize: record.variant.packageSize,
    },
    matchedName: record.matchedName,
    score: record.score,
  };
}
