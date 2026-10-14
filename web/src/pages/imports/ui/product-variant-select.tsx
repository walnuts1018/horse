import { useState } from "react";

import { type ProductSuggestion } from "../../../entities/product";
import {
  ProductVariantSelect as ProductVariantSelectFeature,
  useProductSuggestions,
} from "../../../features/product-variant-select";

export function ProductVariantSelect({
  householdId,
  itemName,
  merchant,
  value,
  onChange,
}: {
  householdId: string;
  itemName: string;
  merchant: string;
  value: string;
  onChange: (variantId: string) => void;
}) {
  const [queryState, setQueryState] = useState({ itemName, query: itemName.trim() });
  const query = queryState.itemName === itemName ? queryState.query : itemName.trim();
  const [selectedSuggestion, setSelectedSuggestion] = useState<ProductSuggestion | null>(null);

  const search = query.trim();
  const { suggestions, loading } = useProductSuggestions({
    householdId,
    query,
    merchant,
    limit: 20,
    delay: search === itemName.trim() ? 0 : 180,
  });
  const currentSelection =
    suggestions.find((suggestion) => suggestion.variant.id === value) ??
    (selectedSuggestion?.variant.id === value ? selectedSuggestion : null);

  return (
    <ProductVariantSelectFeature
      label="商品"
      placeholder="商品名で検索"
      value={value}
      query={query}
      suggestions={suggestions}
      selectedSuggestion={currentSelection}
      loading={loading}
      error={false}
      onQueryChange={(nextQuery) => {
        setQueryState({ itemName, query: nextQuery });
      }}
      onSelect={(suggestion) => {
        setSelectedSuggestion(suggestion);
        onChange(suggestion.variant.id);
      }}
      onClear={() => {
        onChange("");
        setSelectedSuggestion(null);
      }}
    />
  );
}
