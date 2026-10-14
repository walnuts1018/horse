import { useEffect, useState } from "react";

import type { ProductConcept } from "../../../shared/api";

import { readProductSuggestions, type ProductSuggestion } from "../../../entities/product";
import { ProductVariantSelect as ProductVariantSelectFeature } from "../../../features/product-variant-select";

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
  const [suggestionsState, setSuggestionsState] = useState<{
    query: string;
    suggestions: ProductSuggestion[];
  }>({ query: "", suggestions: [] });
  const [loadingQuery, setLoadingQuery] = useState("");
  const [errorQuery, setErrorQuery] = useState("");
  const [selectedSuggestion, setSelectedSuggestion] = useState<ProductSuggestion | null>(null);
  const [searchEnabled, setSearchEnabled] = useState(false);

  useEffect(() => {
    const search = query.trim();
    if (!searchEnabled || search.length < 2) return;

    let active = true;
    const timer = window.setTimeout(
      () => {
        setLoadingQuery(search);
        setErrorQuery("");
        void readProductSuggestions(householdId, search, merchant.trim() || null, 20)
          .then((suggestions) => {
            if (active) setSuggestionsState({ query: search, suggestions });
          })
          .catch(() => {
            if (!active) return;
            setSuggestionsState({ query: search, suggestions: [] });
            setErrorQuery(search);
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
  }, [householdId, itemName, merchant, query, searchEnabled]);

  const search = query.trim();
  const suggestions = suggestionsState.query === search ? suggestionsState.suggestions : [];
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
      loading={loadingQuery === search && search.length >= 2}
      error={errorQuery === search}
      onQueryChange={(nextQuery) => {
        setSearchEnabled(true);
        setQueryState({ itemName, query: nextQuery });
        setLoadingQuery(nextQuery.trim());
        setErrorQuery("");
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
        if (errorQuery === query.trim()) {
          setSearchEnabled(false);
          requestAnimationFrame(() => setSearchEnabled(true));
        }
      }}
    />
  );
}
