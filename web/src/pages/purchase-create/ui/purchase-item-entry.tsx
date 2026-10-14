import type { ClipboardEvent, KeyboardEvent, RefObject } from "react";

import { Plus, Search } from "lucide-react";

import type { Household } from "../../../shared/api";
import type { PurchaseEntrySuggestion } from "../model/product-suggestions";

export function PurchaseItemEntry({
  itemName,
  itemAmount,
  categoryId,
  categories,
  suggestions,
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
  onSelectSuggestion,
  onClearProductSelection,
  onSuggestionHover,
  onAmountChange,
  onAmountFocus,
  onAmountBlur,
  onAmountKey,
  onCategoryChange,
  onAdd,
  invalidEntryField,
}: {
  itemName: string;
  itemAmount: string;
  categoryId: string;
  categories: Household["categories"];
  suggestions: PurchaseEntrySuggestion[];
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
  onSelectSuggestion: (suggestion: PurchaseEntrySuggestion) => void;
  onClearProductSelection: () => void;
  onSuggestionHover: (index: number) => void;
  onAmountChange: (amount: string) => void;
  onAmountFocus: () => void;
  onAmountBlur: () => void;
  onAmountKey: (event: KeyboardEvent<HTMLInputElement>) => void;
  onCategoryChange: (id: string) => void;
  onAdd: () => void;
  invalidEntryField: "name" | "amount" | null;
}) {
  const visibleSuggestionCount = selectedProductVariantId ? 0 : suggestions.length;

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
            aria-invalid={invalidEntryField === "name"}
            aria-describedby={
              invalidEntryField === "name" ? "purchase-item-entry-error" : undefined
            }
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
            !selectedProductVariantId && (
              <div
                className="suggestions product-suggestions"
                id="purchase-item-suggestions"
                aria-label="確認済み商品、購入履歴、推定商品候補"
              >
                {suggestions.map((suggestion, index) => (
                  <button
                    type="button"
                    key={
                      suggestion.kind === "catalog" || suggestion.kind === "confirmed"
                        ? suggestion.candidate.variant.id
                        : `${suggestion.kind}-${suggestion.name}`
                    }
                    id={`purchase-item-suggestion-${index}`}
                    aria-current={activeSuggestionIndex === index}
                    style={{ background: activeSuggestionIndex === index ? "#f3f7f3" : undefined }}
                    onMouseDown={(event) => event.preventDefault()}
                    onMouseEnter={() => onSuggestionHover(index)}
                    onClick={() => onSelectSuggestion(suggestion)}
                  >
                    <Search size={13} />
                    {suggestion.kind === "catalog" ? (
                      <>
                        <span>{suggestion.candidate.matchedName}</span>
                        <small>
                          {suggestion.candidate.concept.name} · {suggestion.candidate.variant.name}
                        </small>
                        <span>推定候補</span>
                      </>
                    ) : (
                      <>
                        <span>{suggestion.name}</span>
                        {suggestion.kind === "confirmed" && (
                          <small>
                            {suggestion.candidate.concept.name} ·{" "}
                            {suggestion.candidate.variant.name}
                          </small>
                        )}
                        <span>{suggestion.kind === "confirmed" ? "確認済み商品" : "購入履歴"}</span>
                      </>
                    )}
                  </button>
                ))}
              </div>
            )}
          {selectedProductVariantId && (
            <div className="selected-product-label">
              <span>商品を選択済み</span>
              <button
                type="button"
                aria-label="選択した商品を解除"
                onClick={onClearProductSelection}
              >
                商品を解除
              </button>
            </div>
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
            enterKeyHint="done"
            onPaste={onPaste}
            placeholder="金額"
            aria-label="金額"
            aria-invalid={invalidEntryField === "amount"}
            aria-describedby={
              invalidEntryField === "amount" ? "purchase-item-entry-error" : undefined
            }
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
      {invalidEntryField && (
        <small className="entry-error" id="purchase-item-entry-error" role="alert">
          {invalidEntryField === "name"
            ? "品名を入力してください"
            : "1円以上の金額を入力してください"}
        </small>
      )}
      <div className={`entry-hint ${classNames.entryHint}`}>
        <span>
          <kbd>Enter</kbd>で品名から金額へ、もう一度で追加
        </span>
        <span>表計算の行を貼り付けできます</span>
      </div>
    </>
  );
}
