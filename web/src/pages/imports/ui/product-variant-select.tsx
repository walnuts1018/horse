import { useEffect, useState } from "react";
import { readProductSuggestions, type ProductSuggestion } from "../../../entities/product";

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
  const [suggestions, setSuggestions] = useState<ProductSuggestion[]>([]);
  const [suggestionsQuery, setSuggestionsQuery] = useState("");
  const [selectedSuggestion, setSelectedSuggestion] = useState<{
    itemName: string;
    suggestion: ProductSuggestion;
  } | null>(null);
  const [searchEnabled, setSearchEnabled] = useState(false);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    const query = itemName.trim();
    if (!searchEnabled || query.length < 2) {
      setSuggestions([]);
      setLoading(false);
      return;
    }
    let active = true;
    const timer = window.setTimeout(() => {
      setLoading(true);
      void readProductSuggestions(householdId, query, merchant.trim() || null, 8)
        .then((result) => {
          if (!active) return;
          setSuggestions(result);
          setSuggestionsQuery(query);
        })
        .catch(() => active && setSuggestions([]))
        .finally(() => active && setLoading(false));
    }, 220);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [householdId, itemName, merchant, searchEnabled]);

  const currentSuggestions = suggestionsQuery === itemName.trim() ? suggestions : [];
  const currentSelectedSuggestion =
    selectedSuggestion?.itemName === itemName ? selectedSuggestion.suggestion : null;
  const options = currentSuggestions.some((suggestion) => suggestion.variant.id === value)
    ? currentSuggestions
    : currentSelectedSuggestion?.variant.id === value
      ? [currentSelectedSuggestion, ...currentSuggestions]
      : currentSuggestions;

  return (
    <label>
      商品
      <select
        aria-label={`${itemName || "品目"}の商品variant`}
        value={value}
        onFocus={() => setSearchEnabled(true)}
        onChange={(event) => {
          const variantId = event.target.value;
          const suggestion =
            currentSuggestions.find((entry) => entry.variant.id === variantId) ?? null;
          setSelectedSuggestion(suggestion ? { itemName, suggestion } : null);
          onChange(variantId);
        }}
      >
        <option value="">商品を選択</option>
        {options.map((suggestion) => (
          <option value={suggestion.variant.id} key={suggestion.variant.id}>
            {[suggestion.concept.name, suggestion.variant.name, suggestion.variant.brand]
              .filter(Boolean)
              .join(" · ")}
            {suggestion.variant.packageSize ? ` (${suggestion.variant.packageSize})` : ""}
          </option>
        ))}
      </select>
      {loading && <small>商品候補を検索中</small>}
      {searchEnabled &&
        !loading &&
        itemName.trim().length >= 2 &&
        currentSuggestions.length === 0 && <small>商品候補がありません</small>}
    </label>
  );
}
