import { Combobox } from "@base-ui/react/combobox";
import { useId, useRef } from "react";

import type { ProductSuggestion } from "../../../entities/product";

import { ProductCandidateOption } from "./product-candidate-option";

import "./product-variant-select.css";

function suggestionLabel(suggestion: ProductSuggestion) {
  return [suggestion.concept.name, suggestion.variant.name, suggestion.variant.brand]
    .filter(Boolean)
    .join(" · ");
}

export function ProductVariantSelect({
  label,
  placeholder,
  value,
  query,
  suggestions,
  selectedSuggestion,
  loading,
  error,
  onQueryChange,
  onSelect,
  onClear,
  onFocus,
}: {
  label: string;
  placeholder: string;
  value: string;
  query: string;
  suggestions: ProductSuggestion[];
  selectedSuggestion: ProductSuggestion | null;
  loading: boolean;
  error: boolean;
  onQueryChange: (query: string) => void;
  onSelect: (suggestion: ProductSuggestion) => void;
  onClear: () => void;
  onFocus?: () => void;
}) {
  const inputId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const currentSelection = selectedSuggestion?.variant.id === value ? selectedSuggestion : null;
  const search = query.trim();

  return (
    <div className="product-variant-select">
      <label id={`${inputId}-label`} htmlFor={`${inputId}-input`}>
        {label}
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
            aria-label="商品選択を解除"
            onClick={() => {
              onClear();
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
        onInputValueChange={onQueryChange}
        onValueChange={(variantId) => {
          if (!variantId || variantId === value) return;
          const suggestion = suggestions.find((entry) => entry.variant.id === variantId);
          if (suggestion) onSelect(suggestion);
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
            id={`${inputId}-input`}
            aria-labelledby={`${inputId}-label`}
            placeholder={placeholder}
            autoComplete="off"
            onFocus={onFocus}
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
                    <ProductCandidateOption
                      label={suggestionLabel(suggestion)}
                      meta={suggestion.variant.packageSize ?? undefined}
                      badge={suggestion.variant.id === value ? "選択中" : undefined}
                    />
                  </Combobox.Item>
                ))}
                <Combobox.Empty>
                  {!loading && !error && search.length >= 2 && "商品候補がありません"}
                </Combobox.Empty>
              </Combobox.List>
              <div
                className="product-variant-select__status"
                aria-live="polite"
                aria-busy={loading}
              >
                {loading && "商品候補を検索中"}
                {!loading && error && "商品候補を取得できませんでした"}
                {!loading && !error && search.length < 2 && "2文字以上入力してください"}
              </div>
            </Combobox.Popup>
          </Combobox.Positioner>
        </div>
      </Combobox.Root>
    </div>
  );
}
