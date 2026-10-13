import type { Dispatch, SetStateAction } from "react";
import { Check, X } from "lucide-react";
import type { Household } from "../../../shared/api";
import {
  type DocumentReview,
  type ExtractedItem,
  type ReceiptPaymentDraft,
  type ReviewTarget,
  yen,
} from "../model/imports";
import { ProductVariantSelect } from "./product-variant-select";

type ReceiptReviewProps = {
  household: Household;
  selectedDocument: DocumentReview;
  reviewTargets: ReviewTarget[];
  activeReviewIndex: number;
  activeReviewTarget: ReviewTarget | undefined;
  paymentDraftFor: (document: DocumentReview) => ReceiptPaymentDraft[];
  receiptTotal: (document: DocumentReview) => number;
  purchaseDate: string;
  markUnsaved: () => void;
  setReviewTargetIndex: Dispatch<SetStateAction<number>>;
  setReceiptPayments: Dispatch<SetStateAction<Record<string, ReceiptPaymentDraft[]>>>;
  editItem: (documentId: string, index: number, patch: Partial<ExtractedItem>) => void;
  onPurchaseDateChange: (date: string) => void;
  onMerchantChange: (merchant: string) => void;
  suggestedCategoryId: (item: ExtractedItem) => string;
  selectedCandidates: Record<string, string>;
  setSelectedCandidates: Dispatch<SetStateAction<Record<string, string>>>;
  manualAmountOverrides: Record<string, boolean>;
  setManualAmountOverrides: Dispatch<SetStateAction<Record<string, boolean>>>;
  manualAmountOverrideReasons: Record<string, string>;
  setManualAmountOverrideReasons: Dispatch<SetStateAction<Record<string, string>>>;
  pending: boolean;
  onConfirm: () => void;
};

export function ReceiptReview({
  household,
  selectedDocument,
  reviewTargets,
  activeReviewIndex,
  activeReviewTarget,
  paymentDraftFor,
  receiptTotal,
  purchaseDate,
  markUnsaved,
  setReviewTargetIndex,
  setReceiptPayments,
  editItem,
  onPurchaseDateChange,
  onMerchantChange,
  suggestedCategoryId,
  selectedCandidates,
  setSelectedCandidates,
  manualAmountOverrides,
  setManualAmountOverrides,
  manualAmountOverrideReasons,
  setManualAmountOverrideReasons,
  pending,
  onConfirm,
}: ReceiptReviewProps) {
  if (!selectedDocument.result?.receipt?.items?.length) return null;
  return (
    <div className="receipt-review">
      {reviewTargets.length > 0 && (
        <section className="panel-heading" aria-label="確認順">
          <div aria-live="polite">
            <span className="eyebrow">
              確認 {activeReviewIndex + 1}/{reviewTargets.length}
            </span>
            <h3>{activeReviewTarget?.label}</h3>
          </div>
          <div style={{ display: "flex", gap: 8 }}>
            <button
              type="button"
              className="button button-soft"
              disabled={activeReviewIndex === 0}
              onClick={() => setReviewTargetIndex((index) => index - 1)}
            >
              前へ
            </button>
            <button
              type="button"
              className="button button-soft"
              disabled={activeReviewIndex >= reviewTargets.length - 1}
              onClick={() => setReviewTargetIndex((index) => index + 1)}
            >
              次へ
            </button>
          </div>
        </section>
      )}
      <div className="review-fields">
        <div className="review-merchant">
          <label>
            店名
            <input
              value={selectedDocument.result.receipt.merchant ?? ""}
              onChange={(event) => onMerchantChange(event.target.value)}
            />
          </label>
        </div>
        <div className="review-merchant">
          <label>
            購入日（必須）
            <input
              id="receipt-review-purchase-date"
              type="date"
              required
              value={purchaseDate}
              onChange={(event) => onPurchaseDateChange(event.target.value)}
            />
          </label>
        </div>
        <div className="field-group" id="receipt-review-payments" tabIndex={-1}>
          <label>決済内訳</label>
          {paymentDraftFor(selectedDocument).map((payment, index) => (
            <div className="review-merchant" key={payment.key}>
              {(payment.method || payment.cardLast4) && (
                <small>
                  {payment.method}
                  {payment.cardLast4 ? ` · •••• ${payment.cardLast4}` : ""}
                </small>
              )}
              <select
                aria-label={`決済${index + 1}の口座`}
                value={payment.paymentAccountId}
                onChange={(event) => {
                  markUnsaved();
                  setReceiptPayments((current) => ({
                    ...current,
                    [selectedDocument.id]: paymentDraftFor(selectedDocument).map((entry) =>
                      entry.key === payment.key
                        ? { ...entry, paymentAccountId: event.target.value }
                        : entry,
                    ),
                  }));
                }}
              >
                <option value="">口座を選択</option>
                {household.accounts.map((account) => (
                  <option value={account.id} key={account.id}>
                    {account.name}
                  </option>
                ))}
              </select>
              <input
                aria-label={`決済${index + 1}の金額`}
                inputMode="numeric"
                type="number"
                min="0"
                step="1"
                value={payment.amountYen}
                onChange={(event) => {
                  markUnsaved();
                  setReceiptPayments((current) => ({
                    ...current,
                    [selectedDocument.id]: paymentDraftFor(selectedDocument).map((entry) =>
                      entry.key === payment.key
                        ? { ...entry, amountYen: Number(event.target.value) || 0 }
                        : entry,
                    ),
                  }));
                }}
              />
              <button
                className="text-button"
                type="button"
                aria-label={`決済${index + 1}を削除`}
                disabled={paymentDraftFor(selectedDocument).length === 1}
                onClick={() => {
                  markUnsaved();
                  setReceiptPayments((current) => ({
                    ...current,
                    [selectedDocument.id]: paymentDraftFor(selectedDocument).filter(
                      (entry) => entry.key !== payment.key,
                    ),
                  }));
                }}
              >
                <X size={16} />
              </button>
            </div>
          ))}
          <button
            className="text-button"
            type="button"
            disabled={household.accounts.length < 2}
            onClick={() => {
              markUnsaved();
              const current = paymentDraftFor(selectedDocument);
              const total = receiptTotal(selectedDocument);
              const rows =
                current.length === 1 && current[0].amountYen === total
                  ? [
                      { ...current[0], amountYen: Math.floor(total / 2) },
                      {
                        key: crypto.randomUUID(),
                        paymentAccountId: "",
                        amountYen: total - Math.floor(total / 2),
                      },
                    ]
                  : [
                      ...current,
                      {
                        key: crypto.randomUUID(),
                        paymentAccountId: "",
                        amountYen: 0,
                      },
                    ];
              setReceiptPayments((previous) => ({
                ...previous,
                [selectedDocument.id]: rows,
              }));
            }}
          >
            <Check size={14} />
            {paymentDraftFor(selectedDocument).length === 1 ? "決済を分割" : "決済を追加"}
          </button>
          <small>
            決済合計{" "}
            {yen(
              paymentDraftFor(selectedDocument).reduce(
                (total, payment) => total + payment.amountYen,
                0,
              ),
            )}{" "}
            購入額 {yen(receiptTotal(selectedDocument))}
          </small>
        </div>
        {selectedDocument.result.receipt.items.map((item, index) => (
          <div className="receipt-item-review" key={index}>
            <div>
              <input
                id={`receipt-review-item-${index}`}
                aria-label="品名"
                value={item.name}
                onChange={(event) =>
                  editItem(selectedDocument.id, index, { name: event.target.value })
                }
              />
              <small>
                {item.verification} · {Math.round(item.confidence * 100)}%
              </small>
              <select
                aria-label={`${item.name}のカテゴリ`}
                value={item.categoryId != null ? item.categoryId : suggestedCategoryId(item)}
                onChange={(event) =>
                  editItem(selectedDocument.id, index, {
                    categoryId: event.target.value,
                  })
                }
              >
                <option value="">カテゴリを確認</option>
                {household.categories
                  .filter((category) => !category.archived)
                  .map((category) => (
                    <option value={category.id} key={category.id}>
                      {category.name}
                    </option>
                  ))}
              </select>
              <ProductVariantSelect
                householdId={household.id}
                itemName={item.name}
                merchant={selectedDocument.result?.receipt?.merchant ?? ""}
                value={item.productVariantId ?? ""}
                onChange={(productVariantId) =>
                  editItem(selectedDocument.id, index, {
                    productVariantId: productVariantId || null,
                  })
                }
              />
            </div>
            <label>
              ¥
              <input
                inputMode="numeric"
                value={String(item.grossAmount ?? item.printedAmount)}
                onChange={(event) => {
                  markUnsaved();
                  setManualAmountOverrides((current) => ({
                    ...current,
                    [selectedDocument.id]: true,
                  }));
                  editItem(selectedDocument.id, index, {
                    grossAmount: Number(event.target.value.replace(/\D/g, "")),
                  });
                }}
              />
            </label>
            <details className="receipt-measure-details">
              <summary>数量・印字単価</summary>
              <label>
                数量の分子
                <input
                  inputMode="numeric"
                  value={item.quantityNumerator ?? ""}
                  onChange={(event) =>
                    editItem(selectedDocument.id, index, {
                      quantityNumerator: event.target.value || null,
                    })
                  }
                />
              </label>
              <label>
                数量の分母
                <input
                  inputMode="numeric"
                  value={item.quantityDenominator ?? ""}
                  onChange={(event) =>
                    editItem(selectedDocument.id, index, {
                      quantityDenominator: event.target.value || null,
                    })
                  }
                />
              </label>
              <label>
                数量単位
                <input
                  value={item.quantityUnit ?? ""}
                  onChange={(event) =>
                    editItem(selectedDocument.id, index, {
                      quantityUnit: event.target.value || null,
                    })
                  }
                />
              </label>
              <label>
                印字単価
                <input
                  inputMode="numeric"
                  value={item.printedUnitPriceYen ?? ""}
                  onChange={(event) =>
                    editItem(selectedDocument.id, index, {
                      printedUnitPriceYen: event.target.value
                        ? Number(event.target.value.replace(/\D/g, ""))
                        : null,
                    })
                  }
                />
              </label>
              <label>
                基準量の分子
                <input
                  inputMode="numeric"
                  value={item.printedUnitPriceQuantityNumerator ?? ""}
                  onChange={(event) =>
                    editItem(selectedDocument.id, index, {
                      printedUnitPriceQuantityNumerator: event.target.value || null,
                    })
                  }
                />
              </label>
              <label>
                基準量の分母
                <input
                  inputMode="numeric"
                  value={item.printedUnitPriceQuantityDenominator ?? ""}
                  onChange={(event) =>
                    editItem(selectedDocument.id, index, {
                      printedUnitPriceQuantityDenominator: event.target.value || null,
                    })
                  }
                />
              </label>
              <label>
                印字単位
                <input
                  value={item.printedUnitPriceUnit ?? ""}
                  onChange={(event) =>
                    editItem(selectedDocument.id, index, {
                      printedUnitPriceUnit: event.target.value || null,
                    })
                  }
                />
              </label>
            </details>
          </div>
        ))}
        {!!selectedDocument.result.candidates?.length && (
          <div className="candidate-review" id="receipt-review-candidates" tabIndex={-1}>
            <div className="panel-heading">
              <div>
                <span className="eyebrow">金額の解釈</span>
                <h3>計算候補</h3>
              </div>
            </div>
            {selectedDocument.result.candidates.map((candidate) => (
              <button
                type="button"
                className={`candidate-option ${selectedCandidates[selectedDocument.id] === candidate.id ? "candidate-selected" : ""}`}
                key={candidate.id}
                onClick={() => {
                  markUnsaved();
                  setSelectedCandidates((current) => ({
                    ...current,
                    [selectedDocument.id]: candidate.id,
                  }));
                }}
              >
                <span>
                  <b>{candidate.id || "候補"}</b>
                  <small>
                    {candidate.consistent ? "合計一致" : "合計差異"}
                    {candidate.issues.length ? ` · ${candidate.issues.join("、")}` : ""}
                  </small>
                </span>
                <strong>{yen(candidate.computedTotal)}</strong>
              </button>
            ))}
            <small>品目の印字額は保持されます。選んだ計算額を帳簿に使います。</small>
          </div>
        )}
        {(manualAmountOverrides[selectedDocument.id] ||
          (selectedCandidates[selectedDocument.id]
            ? !selectedDocument.result.candidates?.find(
                (candidate) => candidate.id === selectedCandidates[selectedDocument.id],
              )?.consistent
            : !selectedDocument.result.balanced)) && (
          <label className="field-group" id="receipt-review-amount-reason" tabIndex={-1}>
            金額の訂正理由
            <input
              maxLength={500}
              value={manualAmountOverrideReasons[selectedDocument.id] ?? ""}
              onChange={(event) => {
                markUnsaved();
                setManualAmountOverrideReasons((current) => ({
                  ...current,
                  [selectedDocument.id]: event.target.value,
                }));
              }}
            />
          </label>
        )}
        <div className="review-total">
          <span>認識した合計</span>
          <b>
            {yen(
              selectedDocument.result.candidates?.find(
                (candidate) => candidate.id === selectedCandidates[selectedDocument.id],
              )?.computedTotal ?? selectedDocument.result.computedTotal,
            )}
          </b>
        </div>
        <button
          className="button button-primary button-wide"
          disabled={pending || !purchaseDate}
          onClick={() => onConfirm()}
        >
          <Check size={16} />
          帳簿に追加
        </button>
      </div>
    </div>
  );
}
