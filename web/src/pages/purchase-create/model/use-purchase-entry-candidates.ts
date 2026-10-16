import { useCallback, useMemo, useState } from "react";

import type { ProductSuggestion } from "../../../entities/product";
import type { Purchase } from "../../../shared/api";
import type { PurchaseItemsValue } from "./purchase-items";

import { useProductSuggestions } from "../../../features/product-variant-select";
import {
  purchaseEntrySuggestions,
  recentPurchasePrice,
  type PurchaseEntrySuggestion,
} from "./product-suggestions";

export function usePurchaseEntryCandidates({
  householdId,
  merchant,
  history,
  query,
  selectedProductVariantId,
  effectiveCategoryId,
  categoryManuallySelected,
  isEnabledCategory,
  patchDraft,
  markDirty,
  focusAmount,
  focusName,
  setFocused,
}: {
  householdId: string;
  merchant: string;
  history: Purchase[];
  query: string;
  selectedProductVariantId: string;
  effectiveCategoryId: string;
  categoryManuallySelected: boolean;
  isEnabledCategory: (id: string | null | undefined) => id is string;
  patchDraft: (patch: Partial<PurchaseItemsValue>) => void;
  markDirty: () => void;
  focusAmount: () => void;
  focusName: () => void;
  setFocused: (focused: "name" | "amount" | null) => void;
}) {
  const { suggestions: productSuggestions } = useProductSuggestions({
    householdId,
    query,
    merchant,
    limit: 6,
    delay: 220,
  });
  const [suggestionsDismissed, setSuggestionsDismissed] = useState(false);
  const suggestions = useMemo(
    () => purchaseEntrySuggestions(history, query, productSuggestions),
    [history, query, productSuggestions],
  );
  const suggestedPrice = useMemo(
    () => recentPurchasePrice(history, query, selectedProductVariantId || null),
    [history, query, selectedProductVariantId],
  );
  const changeName = useCallback(
    (name: string) => {
      setSuggestionsDismissed(false);
      patchDraft({ itemName: name, selectedProductVariantId: "" });
    },
    [patchDraft],
  );
  const selectHistorySuggestion = useCallback(
    (name: string) => {
      changeName(name);
      focusAmount();
      setFocused("amount");
    },
    [changeName, focusAmount, setFocused],
  );
  const selectProductSuggestion = useCallback(
    (candidate: ProductSuggestion) => {
      if (!productSuggestions.includes(candidate)) return;
      markDirty();
      patchDraft({
        itemName: candidate.matchedName || query.trim(),
        selectedProductVariantId: candidate.variant.id,
        ...(isEnabledCategory(candidate.concept.categoryId) &&
        !effectiveCategoryId &&
        !categoryManuallySelected
          ? { categoryId: candidate.concept.categoryId }
          : {}),
      });
      focusAmount();
      setFocused("amount");
    },
    [
      productSuggestions,
      markDirty,
      patchDraft,
      query,
      isEnabledCategory,
      effectiveCategoryId,
      categoryManuallySelected,
      focusAmount,
      setFocused,
    ],
  );
  const selectEntrySuggestion = useCallback(
    (suggestion: PurchaseEntrySuggestion) => {
      if (suggestion.kind === "catalog") {
        selectProductSuggestion(suggestion.candidate);
        return;
      }
      if (suggestion.kind === "history") {
        selectHistorySuggestion(suggestion.name);
        return;
      }
      if (!productSuggestions.includes(suggestion.candidate)) return;
      markDirty();
      const suggestedCategoryId = isEnabledCategory(suggestion.categoryId)
        ? suggestion.categoryId
        : suggestion.candidate.concept.categoryId;
      patchDraft({
        itemName: suggestion.name,
        selectedProductVariantId: suggestion.candidate.variant.id,
        ...(!effectiveCategoryId &&
        !categoryManuallySelected &&
        isEnabledCategory(suggestedCategoryId)
          ? { categoryId: suggestedCategoryId }
          : {}),
      });
      focusAmount();
      setFocused("amount");
    },
    [
      productSuggestions,
      selectProductSuggestion,
      selectHistorySuggestion,
      markDirty,
      isEnabledCategory,
      effectiveCategoryId,
      categoryManuallySelected,
      patchDraft,
      focusAmount,
      setFocused,
    ],
  );
  const clearProductSelection = useCallback(() => {
    markDirty();
    patchDraft({ selectedProductVariantId: "" });
    setFocused("name");
    focusName();
  }, [markDirty, patchDraft, setFocused, focusName]);

  return {
    suggestions,
    suggestedPrice,
    suggestionsDismissed,
    setSuggestionsDismissed,
    changeName,
    selectEntrySuggestion,
    clearProductSelection,
  };
}
