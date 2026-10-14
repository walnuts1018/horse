import { useEffect, useState } from "react";

import { readProductSuggestions, type ProductSuggestion } from "../../../entities/product";
import { ProductVariantSelect as ProductVariantSelectFeature } from "../../../features/product-variant-select";

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
  const [suggestionsState, setSuggestionsState] = useState<{
    query: string;
    suggestions: ProductSuggestion[];
  }>({ query: "", suggestions: [] });
  const [loadingQuery, setLoadingQuery] = useState("");
  const [selectedSuggestion, setSelectedSuggestion] = useState<ProductSuggestion | null>(null);

  useEffect(() => {
    const search = query.trim();
    if (search.length < 2) return;

    let active = true;
    const timer = window.setTimeout(
      () => {
        void readProductSuggestions(householdId, search, merchant.trim() || null, 20)
          .then((suggestions) => {
            if (active) setSuggestionsState({ query: search, suggestions });
          })
          .catch(() => {
            if (active) setSuggestionsState({ query: search, suggestions: [] });
          })
          .finally(() => {
            if (active) setLoadingQuery((current) => (current === search ? "" : current));
          });
      },
      search === itemName.trim() ? 0 : 180,
    );
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [householdId, itemName, merchant, query]);

  const search = query.trim();
  const suggestions = suggestionsState.query === search ? suggestionsState.suggestions : [];
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
      loading={loadingQuery === search && search.length >= 2}
      error={false}
      onQueryChange={(nextQuery) => {
        setQueryState({ itemName, query: nextQuery });
        setLoadingQuery(nextQuery.trim());
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
