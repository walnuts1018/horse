import { Combobox } from "@base-ui/react/combobox";
import { useEffect, useId, useRef, useState } from "react";

import type { ProductConcept } from "../../../shared/api";

import { readProductSuggestions, type ProductSuggestion } from "../../../entities/product";

import "./ledger-product-variant-select.css";

function suggestionLabel(suggestion: ProductSuggestion) {
  return [suggestion.concept.name, suggestion.variant.name, suggestion.variant.brand]
    .filter(Boolean)
    .join(" · ");
}

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
  const inputId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
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
  const loading = loadingQuery === search && search.length >= 2;
  const currentSelection =
    suggestions.find((suggestion) => suggestion.variant.id === value) ??
    (selectedSuggestion?.variant.id === value ? selectedSuggestion : null) ??
    catalogSuggestion(catalog, value);

  return (
    <div className="ledger-product-variant-select">
      <label id={`${inputId}-label`} htmlFor={`${inputId}-input`}>
        正規商品
      </label>
      {value && (
        <div className="ledger-product-variant-select__selected" aria-live="polite">
          <span>選択中</span>
          <strong>{currentSelection ? suggestionLabel(currentSelection) : "登録済み商品"}</strong>
          {currentSelection?.variant.packageSize && (
            <small>{currentSelection.variant.packageSize}</small>
          )}
          <button
            type="button"
            onClick={() => {
              onChange("", "");
              setSelectedSuggestion(null);
              requestAnimationFrame(() => inputRef.current?.focus());
            }}
            aria-label="商品選択を解除"
          >
            商品を解除
          </button>
        </div>
      )}
      <Combobox.Root
        value={value || null}
        inputValue={query}
        onInputValueChange={(nextQuery) => {
          setSearchEnabled(true);
          setQueryState({ itemName, query: nextQuery });
          setLoadingQuery(nextQuery.trim());
          setErrorQuery("");
        }}
        onValueChange={(variantId) => {
          if (!variantId || variantId === value) return;
          const suggestion = suggestions.find((entry) => entry.variant.id === variantId);
          if (!suggestion) return;
          setSelectedSuggestion(suggestion);
          onChange(variantId, suggestion.variant.name);
        }}
        itemToStringLabel={(variantId) =>
          currentSelection?.variant.id === variantId ? suggestionLabel(currentSelection) : ""
        }
        filter={null}
        autoHighlight
      >
        <div className="ledger-product-variant-select__combobox">
          <Combobox.Input
            ref={inputRef}
            id={`${inputId}-input`}
            aria-labelledby={`${inputId}-label`}
            placeholder="品名から商品を検索"
            autoComplete="off"
            onFocus={() => {
              setSearchEnabled(true);
              if (errorQuery === query.trim()) {
                setSearchEnabled(false);
                requestAnimationFrame(() => setSearchEnabled(true));
              }
            }}
          />
          <Combobox.Positioner>
            <Combobox.Popup className="ledger-product-variant-select__popup">
              <Combobox.List className="ledger-product-variant-select__list" aria-label="商品候補">
                {suggestions.map((suggestion) => (
                  <Combobox.Item
                    key={suggestion.variant.id}
                    value={suggestion.variant.id}
                    disabled={suggestion.variant.id === value}
                  >
                    <span>{suggestionLabel(suggestion)}</span>
                    {suggestion.variant.packageSize && (
                      <small>{suggestion.variant.packageSize}</small>
                    )}
                    {suggestion.variant.id === value && <small>選択中</small>}
                  </Combobox.Item>
                ))}
                <Combobox.Empty>
                  {!loading && !errorQuery && search.length >= 2 && "商品候補がありません"}
                </Combobox.Empty>
              </Combobox.List>
              <div
                className="ledger-product-variant-select__status"
                aria-live="polite"
                aria-busy={loading}
              >
                {loading && "商品候補を検索中"}
                {!loading && errorQuery === search && "商品候補を取得できませんでした"}
                {!loading && !errorQuery && search.length < 2 && "2文字以上入力してください"}
              </div>
            </Combobox.Popup>
          </Combobox.Positioner>
        </div>
      </Combobox.Root>
    </div>
  );
}
