import type { ClipboardEvent, KeyboardEvent, RefObject } from "react";

import { Plus, Search } from "lucide-react";

import type { ProductSuggestion } from "../../../entities/product";
import type { Household } from "../../../shared/api";

export function PurchaseItemEntry({
  itemName,
  itemAmount,
  categoryId,
  categories,
  suggestions,
  productSuggestions,
  selectedProductVariantId,
  activeSuggestionIndex,
  suggestionsDismissed,
  focused,
  classNames,
  nameRef,
  amountRef,
  onNameChange,
  onNameFocus,
  onNameBlur,
  onNameKey,
  onPaste,
  onCompositionStart,
  onCompositionEnd,
  onSelectHistorySuggestion,
  onSelectProductSuggestion,
  onSuggestionHover,
  onAmountChange,
  onAmountFocus,
  onAmountBlur,
  onAmountKey,
  onCategoryChange,
  onAdd,
}: {
  itemName: string;
  itemAmount: string;
  categoryId: string;
  categories: Household["categories"];
  suggestions: string[];
  productSuggestions: ProductSuggestion[];
  selectedProductVariantId: string;
  activeSuggestionIndex: number;
  suggestionsDismissed: boolean;
  focused: "name" | "amount" | null;
  classNames: { entryForm: string; entryHint: string; entryInputTarget: string };
  nameRef: RefObject<HTMLInputElement | null>;
  amountRef: RefObject<HTMLInputElement | null>;
  onNameChange: (name: string) => void;
  onNameFocus: () => void;
  onNameBlur: () => void;
  onNameKey: (event: KeyboardEvent<HTMLInputElement>) => void;
  onPaste: (event: ClipboardEvent<HTMLInputElement>) => void;
  onCompositionStart: () => void;
  onCompositionEnd: () => void;
  onSelectHistorySuggestion: (name: string) => void;
  onSelectProductSuggestion: (candidate: ProductSuggestion) => void;
  onSuggestionHover: (index: number) => void;
  onAmountChange: (amount: string) => void;
  onAmountFocus: () => void;
  onAmountBlur: () => void;
  onAmountKey: (event: KeyboardEvent<HTMLInputElement>) => void;
  onCategoryChange: (id: string) => void;
  onAdd: () => void;
}) {
  const visibleSuggestionCount =
    productSuggestions.length > 0 && !selectedProductVariantId
      ? productSuggestions.length
      : suggestions.length;

  return (
    <>
      <div className={`entry-form ${focused ? "entry-focused" : ""} ${classNames.entryForm}`}>
        <div className={`entry-input-wrap name-wrap ${classNames.entryInputTarget}`}>
          <input
            ref={nameRef}
            value={itemName}
            onChange={(event) => onNameChange(event.target.value)}
            onFocus={onNameFocus}
            onBlur={onNameBlur}
            onKeyDown={onNameKey}
            onPaste={onPaste}
            onCompositionStart={onCompositionStart}
            onCompositionEnd={onCompositionEnd}
            enterKeyHint="next"
            placeholder="品名を入力"
            aria-label="品名"
            role="combobox"
            aria-autocomplete="list"
            aria-controls="purchase-item-suggestions"
            aria-expanded={
              focused === "name" && visibleSuggestionCount > 0 && !suggestionsDismissed
            }
            aria-activedescendant={
              activeSuggestionIndex >= 0 && !suggestionsDismissed
                ? `purchase-item-suggestion-${activeSuggestionIndex}`
                : undefined
            }
            autoComplete="off"
          />
          <kbd>Enter</kbd>
          {focused === "name" &&
            !suggestionsDismissed &&
            suggestions.length > 0 &&
            productSuggestions.length === 0 && (
              <div
                className="suggestions"
                id="purchase-item-suggestions"
                role="listbox"
                aria-label="購入履歴の商品候補"
              >
                {suggestions.map((name, index) => (
                  <button
                    type="button"
                    key={name}
                    id={`purchase-item-suggestion-${index}`}
                    role="option"
                    aria-selected={activeSuggestionIndex === index}
                    style={{ background: activeSuggestionIndex === index ? "#f3f7f3" : undefined }}
                    onMouseDown={(event) => event.preventDefault()}
                    onMouseEnter={() => onSuggestionHover(index)}
                    onClick={() => onSelectHistorySuggestion(name)}
                  >
                    <Search size={13} />
                    {name}
                    <span>購入履歴</span>
                  </button>
                ))}
              </div>
            )}
          {focused === "name" &&
            !suggestionsDismissed &&
            productSuggestions.length > 0 &&
            !selectedProductVariantId && (
              <div
                className="suggestions product-suggestions"
                id="purchase-item-suggestions"
                role="listbox"
                aria-label="商品候補"
              >
                {productSuggestions.map((candidate, index) => (
                  <button
                    type="button"
                    key={candidate.variant.id}
                    id={`purchase-item-suggestion-${index}`}
                    role="option"
                    aria-selected={activeSuggestionIndex === index}
                    style={{ background: activeSuggestionIndex === index ? "#f3f7f3" : undefined }}
                    onMouseDown={(event) => event.preventDefault()}
                    onMouseEnter={() => onSuggestionHover(index)}
                    onClick={() => onSelectProductSuggestion(candidate)}
                  >
                    <Search size={13} />
                    <span>{candidate.matchedName}</span>
                    <small>
                      {candidate.concept.name} · {candidate.variant.name}
                    </small>
                  </button>
                ))}
              </div>
            )}
          {selectedProductVariantId && (
            <small className="selected-product-label">商品を選択済み</small>
          )}
        </div>
        <div className={`entry-input-wrap amount-wrap ${classNames.entryInputTarget}`}>
          <span>¥</span>
          <input
            ref={amountRef}
            value={itemAmount}
            inputMode="numeric"
            onChange={(event) => onAmountChange(event.target.value)}
            onFocus={onAmountFocus}
            onBlur={onAmountBlur}
            onKeyDown={onAmountKey}
            onCompositionStart={onCompositionStart}
            onCompositionEnd={onCompositionEnd}
            enterKeyHint="next"
            onPaste={onPaste}
            placeholder="金額"
            aria-label="金額"
          />
        </div>
        <select
          aria-label="新しい品目のカテゴリ"
          value={categoryId}
          onChange={(event) => onCategoryChange(event.target.value)}
        >
          <option value="">未分類</option>
          {categories.map((category) => (
            <option key={category.id} value={category.id}>
              {category.name}
            </option>
          ))}
        </select>
        <button
          type="button"
          className="button button-add"
          disabled={!itemName.trim() || !itemAmount.trim()}
          onClick={onAdd}
        >
          <Plus size={16} />
          <span>品目を追加</span>
        </button>
      </div>
      <div className={`entry-hint ${classNames.entryHint}`}>
        <span>
          <kbd>Enter</kbd>で品名から金額へ、もう一度で追加
        </span>
        <span>表計算の行を貼り付けできます</span>
      </div>
    </>
  );
}
