import { useState } from "react";

import type { ProductConcept } from "../../../shared/api";

import { type ProductSuggestion } from "../../../entities/product";
import {
  ProductVariantSelect as ProductVariantSelectFeature,
  useProductSuggestions,
} from "../../../features/product-variant-select";

const catalogSuggestions = new WeakMap<ProductConcept[], Map<string, ProductSuggestion>>();

function catalogSuggestion(catalog: ProductConcept[], variantId: string) {
  let suggestionsById = catalogSuggestions.get(catalog);
  if (!suggestionsById) {
    suggestionsById = new Map();
    for (const concept of catalog) {
      for (const variant of concept.variants) {
        suggestionsById.set(variant.id, {
          concept: { id: concept.id, name: concept.name },
          variant: {
            id: variant.id,
            name: variant.name,
            brand: variant.brand,
            packageSize: variant.packageSize,
          },
          matchedName: variant.name,
          score: 0,
        });
      }
    }
    catalogSuggestions.set(catalog, suggestionsById);
  }
  return suggestionsById.get(variantId) ?? null;
}

export function LedgerProductVariantSelect({
  householdId,
  itemName,
  merchant,
  value,
  catalog,
  onChange,
}: {
  householdId: string;
  itemName: string;
  merchant: string;
  value: string;
  catalog: ProductConcept[];
  onChange: (variantId: string, variantName: string) => void;
}) {
  const [queryState, setQueryState] = useState({ itemName, query: itemName.trim() });
  const query = queryState.itemName === itemName ? queryState.query : itemName.trim();
  const [selectedSuggestion, setSelectedSuggestion] = useState<ProductSuggestion | null>(null);
  const [searchEnabled, setSearchEnabled] = useState(false);

  const search = query.trim();
  const { suggestions, loading, error, retry } = useProductSuggestions({
    householdId,
    query,
    merchant,
    limit: 20,
    enabled: searchEnabled,
    delay: search === itemName.trim() ? 0 : 180,
  });
  const currentSelection =
    suggestions.find((suggestion) => suggestion.variant.id === value) ??
    (selectedSuggestion?.variant.id === value ? selectedSuggestion : null) ??
    catalogSuggestion(catalog, value);

  return (
    <ProductVariantSelectFeature
      label="正規商品"
      placeholder="品名から商品を検索"
      value={value}
      query={query}
      suggestions={suggestions}
      selectedSuggestion={currentSelection}
      loading={loading}
      error={error}
      onQueryChange={(nextQuery) => {
        setSearchEnabled(true);
        setQueryState({ itemName, query: nextQuery });
      }}
      onSelect={(suggestion) => {
        setSelectedSuggestion(suggestion);
        onChange(suggestion.variant.id, suggestion.variant.name);
      }}
      onClear={() => {
        onChange("", "");
        setSelectedSuggestion(null);
      }}
      onFocus={() => {
        setSearchEnabled(true);
        if (error) retry();
      }}
    />
  );
}
