import { Combobox } from "@base-ui/react/combobox";
import { useEffect, useId, useRef, useState } from "react";

import { readProductSuggestions, type ProductSuggestion } from "../../../entities/product";

import "./product-variant-select.css";

function suggestionLabel(suggestion: ProductSuggestion) {
  return [suggestion.concept.name, suggestion.variant.name, suggestion.variant.brand]
    .filter(Boolean)
    .join(" · ");
}

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
  const listboxId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
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
  const loading = loadingQuery === search && search.length >= 2;
  const currentSelection =
    suggestions.find((suggestion) => suggestion.variant.id === value) ??
    (selectedSuggestion?.variant.id === value ? selectedSuggestion : null);

  return (
    <div className="product-variant-select">
      <label id={`${listboxId}-label`} htmlFor={`${listboxId}-input`}>
        商品
      </label>
      {value && (
        <div className="product-variant-select__selected" aria-live="polite">
          <span>選択中</span>
          <strong>{currentSelection ? suggestionLabel(currentSelection) : "登録済み商品"}</strong>
          {currentSelection?.variant.packageSize && (
            <small>{currentSelection.variant.packageSize}</small>
          )}
          <button
            type="button"
            onClick={() => {
              onChange("");
              setSelectedSuggestion(null);
              requestAnimationFrame(() => inputRef.current?.focus());
            }}
          >
            解除
          </button>
        </div>
      )}
      <Combobox.Root
        value={value || null}
        inputValue={query}
        onInputValueChange={(nextQuery) => {
          setQueryState({ itemName, query: nextQuery });
          setLoadingQuery(nextQuery.trim());
        }}
        onValueChange={(variantId) => {
          if (!variantId || variantId === value) return;
          const suggestion = suggestions.find((entry) => entry.variant.id === variantId);
          if (suggestion) setSelectedSuggestion(suggestion);
          onChange(variantId);
        }}
        itemToStringLabel={(variantId) =>
          currentSelection?.variant.id === variantId ? suggestionLabel(currentSelection) : ""
        }
        filter={null}
        autoHighlight
      >
        <div className="product-variant-select__combobox">
          <Combobox.Input
            ref={inputRef}
            id={`${listboxId}-input`}
            aria-labelledby={`${listboxId}-label`}
            placeholder="商品名で検索"
            autoComplete="off"
          />
          <Combobox.Positioner>
            <Combobox.Popup className="product-variant-select__popup">
              <Combobox.List className="product-variant-select__list" aria-label="商品候補">
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
                  {!loading && search.length >= 2 && "商品候補がありません"}
                </Combobox.Empty>
              </Combobox.List>
              <div
                className="product-variant-select__status"
                aria-live="polite"
                aria-busy={loading}
              >
                {loading && "商品候補を検索中"}
                {!loading && search.length < 2 && "2文字以上入力してください"}
              </div>
            </Combobox.Popup>
          </Combobox.Positioner>
        </div>
      </Combobox.Root>
    </div>
  );
}
