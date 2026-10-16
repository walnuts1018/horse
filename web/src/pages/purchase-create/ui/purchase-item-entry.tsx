import { Combobox } from "@base-ui/react/combobox";
import { Plus } from "lucide-react";
import {
  useEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type KeyboardEvent,
  type RefObject,
} from "react";

import type { Household } from "../../../shared/api";
import type { PurchaseEntrySuggestion, RecentPurchasePrice } from "../model/product-suggestions";

import {
  ProductCandidateList,
  ProductCandidateOption,
} from "../../../features/product-variant-select";

export function PurchaseItemEntry({
  itemName,
  itemAmount,
  categoryId,
  categories,
  suggestions,
  selectedProductVariantId,
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
  onSuggestionsOpenChange,
  onAmountChange,
  recentPurchasePrice,
  onApplyRecentPrice,
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
  onSuggestionsOpenChange: (open: boolean) => void;
  onAmountChange: (amount: string) => void;
  recentPurchasePrice: RecentPurchasePrice | null;
  onApplyRecentPrice: () => void;
  onAmountFocus: () => void;
  onAmountBlur: () => void;
  onAmountKey: (event: KeyboardEvent<HTMLInputElement>) => void;
  onCategoryChange: (id: string) => void;
  onAdd: () => void;
  invalidEntryField: "name" | "amount" | null;
}) {
  const [categoryPickerOpen, setCategoryPickerOpen] = useState(false);
  const categorySelectRef = useRef<HTMLSelectElement>(null);
  useEffect(() => {
    if (categoryPickerOpen) categorySelectRef.current?.focus();
  }, [categoryPickerOpen]);
  const dateLabel = recentPurchasePrice
    ? new Intl.DateTimeFormat("ja-JP", {
        month: "numeric",
        day: "numeric",
        timeZone: "Asia/Tokyo",
      }).format(new Date(recentPurchasePrice.occurredAt))
    : "";
  const amountLabel = recentPurchasePrice
    ? new Intl.NumberFormat("ja-JP").format(recentPurchasePrice.amountYen)
    : "";
  const suggestionsVisible =
    focused === "name" &&
    suggestions.length > 0 &&
    !selectedProductVariantId &&
    !suggestionsDismissed;

  return (
    <>
      <div className={`entry-form ${focused ? "entry-focused" : ""} ${classNames.entryForm}`}>
        <div className={`entry-input-wrap name-wrap ${classNames.entryInputTarget}`}>
          <Combobox.Root
            value={null}
            inputValue={itemName}
            open={suggestionsVisible}
            onOpenChange={onSuggestionsOpenChange}
            onInputValueChange={onNameChange}
            onValueChange={(value) => {
              const index = Number(value);
              const suggestion = Number.isInteger(index) ? suggestions[index] : undefined;
              if (suggestion) onSelectSuggestion(suggestion);
            }}
            itemToStringLabel={(value) => {
              const index = Number(value);
              const suggestion = Number.isInteger(index) ? suggestions[index] : undefined;
              return suggestion
                ? suggestion.kind === "catalog"
                  ? suggestion.candidate.matchedName
                  : suggestion.name
                : "";
            }}
            filter={null}
          >
            <Combobox.Input
              ref={nameRef}
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
              autoComplete="off"
            />
            <kbd>Enter</kbd>
            <Combobox.Portal>
              <Combobox.Positioner>
                <Combobox.Popup className="suggestions product-suggestions product-variant-select__popup">
                  <ProductCandidateList
                    label="確認済み商品、購入履歴、推定商品候補"
                    candidates={suggestions.map((suggestion, index) => ({
                      id: String(index),
                      content:
                        suggestion.kind === "catalog" ? (
                          <ProductCandidateOption
                            label={suggestion.candidate.matchedName}
                            description={`${suggestion.candidate.concept.name} · ${suggestion.candidate.variant.name}`}
                            meta={suggestion.candidate.variant.packageSize ?? undefined}
                            badge="推定候補"
                          />
                        ) : (
                          <ProductCandidateOption
                            label={suggestion.name}
                            description={
                              suggestion.kind === "confirmed"
                                ? `${suggestion.candidate.concept.name} · ${suggestion.candidate.variant.name}`
                                : undefined
                            }
                            meta={
                              suggestion.kind === "confirmed"
                                ? (suggestion.candidate.variant.packageSize ?? undefined)
                                : undefined
                            }
                            badge={suggestion.kind === "confirmed" ? "確認済み商品" : "購入履歴"}
                          />
                        ),
                    }))}
                  />
                </Combobox.Popup>
              </Combobox.Positioner>
            </Combobox.Portal>
          </Combobox.Root>
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
        <div className="amount-entry">
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
          {recentPurchasePrice && (
            <div className="recent-price-action">
              <span>{`${recentPurchasePrice.merchant} · ${dateLabel}`}</span>
              <button
                type="button"
                aria-label={`${dateLabel}の${recentPurchasePrice.merchant}での購入価格¥${amountLabel}を金額に適用`}
                onClick={onApplyRecentPrice}
              >
                {`前回 ¥${amountLabel}`}
              </button>
            </div>
          )}
        </div>
        <div className={`entry-category ${categoryId ? "" : "entry-category-uncategorized"}`}>
          {categoryPickerOpen ? (
            <select
              ref={categorySelectRef}
              aria-label="新しい品目のカテゴリ"
              value={categoryId}
              onChange={(event) => {
                onCategoryChange(event.target.value);
                setCategoryPickerOpen(false);
              }}
            >
              <option value="">未分類</option>
              {categories.map((category) => (
                <option key={category.id} value={category.id}>
                  {category.name}
                </option>
              ))}
            </select>
          ) : (
            <button
              type="button"
              className="entry-category-button"
              aria-label={categoryId ? "品目カテゴリを変更" : "未分類。カテゴリを選択"}
              onClick={() => setCategoryPickerOpen(true)}
            >
              <span>
                {categories.find((category) => category.id === categoryId)?.name ?? "未分類"}
              </span>
              <span>{categoryId ? "変更" : "選択"}</span>
            </button>
          )}
        </div>
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
