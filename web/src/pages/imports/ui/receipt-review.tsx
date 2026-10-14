import { Check, Plus, Undo2, X } from "lucide-react";
import { useEffect, useRef, useSyncExternalStore, type Dispatch, type SetStateAction } from "react";

import type { Household } from "../../../shared/api";

import { EditPurchaseItemAllocations } from "../../../entities/purchase";
import {
  type DocumentReview,
  type ExtractedItem,
  type ReceiptPaymentDraft,
  type ReviewTarget,
  yen,
} from "../model/imports";
import {
  getReceiptEvidenceSelection,
  selectReceiptEvidenceItem,
  subscribeReceiptEvidenceSelection,
} from "../model/receipt-evidence-selection";
import {
  receiptItemReviewStateLabels,
  type ReceiptItemReviewState,
} from "../model/receipt-item-review";
import { addReceiptPayment, updateReceiptPaymentAmount } from "../model/receipt-payment-split";
import { parseReceiptReviewAmountDraft } from "../model/receipt-review-amount";
import { ensureReceiptItemKeys } from "../model/receipt-review-draft";
import {
  candidateAmountDifferences,
  candidateAmountForItem,
  candidateInterpretationLabels,
} from "../model/tax-candidate-explanation";
import {
  effectiveReceiptItemAmount,
  manualAmountOverrideReasonOptions,
} from "../model/use-import-confirmation";
import { ProductVariantSelect } from "./product-variant-select";

import styles from "./imports-page.module.css";

type ReceiptReviewProps = {
  household: Household;
  selectedDocument: DocumentReview;
  reviewTargets: ReviewTarget[];
  activeReviewIndex: number;
  activeReviewTarget: ReviewTarget | undefined;
  canUndoItemDeletion: boolean;
  undoItemDeletion: () => void;
  paymentDraftFor: (document: DocumentReview) => ReceiptPaymentDraft[];
  receiptTotal: (document: DocumentReview) => number;
  purchaseDate: string;
  purchaseTime: string;
  markUnsaved: () => void;
  setReviewTargetIndex: Dispatch<SetStateAction<number>>;
  setReceiptPayments: Dispatch<SetStateAction<Record<string, ReceiptPaymentDraft[] | undefined>>>;
  receiptAmountDrafts: Record<string, Record<string, string>>;
  setReceiptAmountDrafts: Dispatch<SetStateAction<Record<string, Record<string, string>>>>;
  editItem: (documentId: string, index: number, patch: Partial<ExtractedItem>) => void;
  addItem: (documentId: string) => void;
  deleteItem: (documentId: string, itemKey: string) => void;
  itemAllocationsFor: (
    documentId: string,
    itemKey: string,
  ) => Array<{
    poolId: string;
    weight: number;
  }>;
  editItemAllocations: (
    documentId: string,
    itemKey: string,
    allocations: Array<{ poolId: string; weight: number }>,
  ) => void;
  onPurchaseDateChange: (date: string) => void;
  onPurchaseTimeChange: (time: string) => void;
  onMerchantChange: (merchant: string) => void;
  suggestedCategoryId: (item: ExtractedItem) => string;
  selectedCandidates: Record<string, string>;
  setSelectedCandidates: Dispatch<SetStateAction<Record<string, string>>>;
  manualAmountOverrides: Record<string, Record<string, number>>;
  setManualAmountOverrides: Dispatch<SetStateAction<Record<string, Record<string, number>>>>;
  receiptItemReviewStates: Record<string, Record<string, ReceiptItemReviewState>>;
  setReceiptItemReviewStates: Dispatch<
    SetStateAction<Record<string, Record<string, ReceiptItemReviewState>>>
  >;
  manualAmountOverrideReasons: Record<string, string | undefined>;
  setManualAmountOverrideReasons: Dispatch<SetStateAction<Record<string, string | undefined>>>;
  pending: boolean;
  onConfirm: () => void;
};

export function ReceiptReview({
  household,
  selectedDocument,
  reviewTargets,
  activeReviewIndex,
  activeReviewTarget,
  canUndoItemDeletion,
  undoItemDeletion,
  paymentDraftFor,
  receiptTotal,
  purchaseDate,
  purchaseTime,
  markUnsaved,
  setReviewTargetIndex,
  setReceiptPayments,
  receiptAmountDrafts,
  setReceiptAmountDrafts,
  editItem,
  addItem,
  deleteItem,
  itemAllocationsFor,
  editItemAllocations,
  onPurchaseDateChange,
  onPurchaseTimeChange,
  onMerchantChange,
  suggestedCategoryId,
  selectedCandidates,
  setSelectedCandidates,
  manualAmountOverrides,
  setManualAmountOverrides,
  receiptItemReviewStates,
  setReceiptItemReviewStates,
  manualAmountOverrideReasons,
  setManualAmountOverrideReasons,
  pending,
  onConfirm,
}: ReceiptReviewProps) {
  const focusNewItem = useRef(false);
  const itemNameInputs = useRef(new Map<string, HTMLInputElement>());
  const result = selectedDocument.result;
  const receipt = result?.receipt;
  const receiptItems = ensureReceiptItemKeys(receipt?.items ?? []);
  const evidenceSelection = useSyncExternalStore(
    (listener) => subscribeReceiptEvidenceSelection(selectedDocument.id, listener),
    () => getReceiptEvidenceSelection(selectedDocument.id),
    () => null,
  );

  useEffect(() => {
    if (!evidenceSelection?.focusEditor) return;
    itemNameInputs.current.get(evidenceSelection.itemKey)?.focus({ preventScroll: true });
  }, [evidenceSelection?.focusEditor, evidenceSelection?.itemKey]);
  if (!result || !receipt) return null;
  const itemsChanged = Boolean(
    (result as typeof result & { reviewReceiptItemsChanged?: boolean }).reviewReceiptItemsChanged,
  );
  const candidates = result.candidates ?? [];
  const amountDifferences = itemsChanged
    ? []
    : candidateAmountDifferences(candidates, receiptItems);
  const validReceiptItems =
    receiptItems.length > 0 &&
    receiptItems.every(
      (item) =>
        item.name.trim().length > 0 &&
        Number.isSafeInteger(item.grossAmount) &&
        item.grossAmount >= (item.verification === "MANUAL" ? 1 : 0),
    );
  const uncategorizedItemCount = receiptItems.filter((item) =>
    item.categoryId != null ? !item.categoryId : !suggestedCategoryId(item),
  ).length;
  const allReceiptItemsReviewed =
    receiptItems.length > 0 &&
    receiptItems.every((item, index) => {
      const itemKey = item.key ?? `item-${index}`;
      const state = receiptItemReviewStates[selectedDocument.id]?.[itemKey] ?? "UNREVIEWED";
      return state === "CONFIRMED" || state === "CORRECTED";
    });
  const compactReview =
    result.balanced === true &&
    !selectedDocument.duplicateOf &&
    (result.issues?.length ?? 0) === 0 &&
    allReceiptItemsReviewed &&
    reviewTargets.length === 0;
  const summaryPayments = paymentDraftFor(selectedDocument);
  const paymentSummary = summaryPayments
    .map((payment) => {
      const accountName =
        household.accounts.find((account) => account.id === payment.paymentAccountId)?.name ??
        "口座未選択";
      return `${accountName} ${yen(payment.amountYen)}`;
    })
    .join("、");
  const defaultAllocations =
    household.defaultAllocation.length > 0
      ? household.defaultAllocation
      : household.pools.map((pool) => ({
          poolId: pool.id,
          weight: pool.defaultShareWeight || 1,
        }));
  const manualAmountOverrideReason = manualAmountOverrideReasons[selectedDocument.id] ?? "";
  const selectedManualAmountOverrideReason = manualAmountOverrideReasonOptions.some(
    (reason) => reason === manualAmountOverrideReason,
  )
    ? manualAmountOverrideReason
    : manualAmountOverrideReason
      ? "その他"
      : "";
  const otherManualAmountOverrideReason =
    selectedManualAmountOverrideReason === "その他" && manualAmountOverrideReason !== "その他"
      ? manualAmountOverrideReason
      : "";
  const setManualAmountOverrideReason = (reason: string) => {
    markUnsaved();
    setManualAmountOverrideReasons((current) => ({
      ...current,
      [selectedDocument.id]: reason,
    }));
  };
  const focusReceiptItem = (index: number) => {
    const item = receiptItems[index];
    const itemKey = item.key ?? `item-${index}`;
    const targetId = `receipt-review-item-${encodeURIComponent(itemKey)}`;
    const targetIndex = reviewTargets.findIndex((target) => target.elementId === targetId);
    if (targetIndex >= 0) setReviewTargetIndex(targetIndex);
    selectReceiptEvidenceItem(selectedDocument.id, {
      itemKey,
      index,
      focusEditor: true,
    });
    document.getElementById(targetId)?.scrollIntoView({ block: "center", behavior: "smooth" });
    itemNameInputs.current.get(itemKey)?.focus({ preventScroll: true });
  };
  const navigateToIssue = (issue: string) => {
    const itemNumber = issue.match(/品目\s*(\d+)/)?.[1];
    const itemIndex = itemNumber ? Number(itemNumber) - 1 : -1;
    if (itemIndex >= 0 && itemIndex < receiptItems.length) {
      focusReceiptItem(itemIndex);
      return;
    }

    const target = /決済|支払/.test(issue)
      ? reviewTargets.find((entry) => entry.elementId === "receipt-review-payments")
      : /購入日|日付/.test(issue)
        ? reviewTargets.find((entry) => entry.elementId === "receipt-review-purchase-date")
        : /税|金額|合計|候補/.test(issue)
          ? reviewTargets.find((entry) => entry.key === "amount")
          : reviewTargets[0];
    if (target) {
      setReviewTargetIndex(reviewTargets.indexOf(target));
      return;
    }

    if (receiptItems.length === 0) return;
    const firstItem = receiptItems[0];
    const itemKey = firstItem.key ?? "item-0";
    selectReceiptEvidenceItem(selectedDocument.id, {
      itemKey,
      index: 0,
      focusEditor: true,
    });
    document.getElementById(`receipt-review-item-${encodeURIComponent(itemKey)}`)?.scrollIntoView({
      block: "center",
      behavior: "smooth",
    });
    itemNameInputs.current.get(itemKey)?.focus({ preventScroll: true });
  };
  const setReceiptItemReviewState = (itemKey: string, state: ReceiptItemReviewState) => {
    markUnsaved();
    setReceiptItemReviewStates((current) => ({
      ...current,
      [selectedDocument.id]: {
        ...current[selectedDocument.id],
        [itemKey]: state,
      },
    }));
  };
  const updateAmountDraftKeys = (previousItems: ExtractedItem[], nextItems: ExtractedItem[]) => {
    const previousKeys = ensureReceiptItemKeys(previousItems);
    const nextKeys = ensureReceiptItemKeys(nextItems);
    setReceiptAmountDrafts((current) => {
      const previousDrafts = current[selectedDocument.id] ?? {};
      const nextDrafts: Record<string, string> = {};
      for (const [index, previousItem] of previousKeys.entries()) {
        const nextItem = nextKeys.find((item) => item.key === previousItem.key);
        if (!nextItem?.key) continue;
        const previousKey = previousItems[index]?.key ?? `item-${index}`;
        const value = previousDrafts[previousKey] ?? previousDrafts[previousItem.key ?? ""];
        if (
          Object.hasOwn(previousDrafts, previousKey) ||
          Object.hasOwn(previousDrafts, previousItem.key ?? "")
        ) {
          nextDrafts[nextItem.key] = value;
        }
      }
      return { ...current, [selectedDocument.id]: nextDrafts };
    });
  };
  return (
    <div className="receipt-review">
      {(result.issues?.length ?? 0) > 0 && (
        <section className={styles.receiptIssues} aria-label="確認が必要な項目">
          <h3>確認が必要な項目</h3>
          <ul>
            {result.issues?.map((issue) => (
              <li key={issue}>
                <span>{issue}</span>
                <button
                  type="button"
                  className="text-button"
                  onClick={() => navigateToIssue(issue)}
                >
                  該当箇所へ
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}
      {(receiptItems.length > 0 || reviewTargets.length > 0) && (
        <section className="panel-heading" aria-label="確認順">
          <div aria-live="polite">
            <span className="eyebrow">
              {activeReviewTarget
                ? `確認 ${activeReviewIndex + 1}/${reviewTargets.length}`
                : `${receiptItems.length}品目を確認しました`}
            </span>
            <h3>{activeReviewTarget?.label ?? "未確認項目はありません"}</h3>
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
      {canUndoItemDeletion && (
        <output className={styles.receiptDeleteUndo}>
          <button type="button" className="button button-soft" onClick={undoItemDeletion}>
            <Undo2 size={16} />
            品目を復元
          </button>
        </output>
      )}
      {compactReview && (
        <section className={styles.receiptSummary} aria-label="購入内容の概要">
          <div className={styles.receiptSummaryMerchant}>
            <span>店名</span>
            <strong>{receipt.merchant?.trim() || "店名未入力"}</strong>
          </div>
          <div className={styles.receiptSummaryDate}>
            <span>購入日時</span>
            <strong>{purchaseTime ? `${purchaseDate} ${purchaseTime}` : purchaseDate}</strong>
          </div>
          <div>
            <span>合計</span>
            <strong>{yen(receiptTotal(selectedDocument))}</strong>
          </div>
          <div>
            <span>品目</span>
            <strong>{receiptItems.length}件</strong>
          </div>
          <div className={styles.receiptSummaryPayment}>
            <span>決済元</span>
            <strong>{paymentSummary || "決済情報なし"}</strong>
          </div>
        </section>
      )}
      <div className="review-fields">
        <details
          className={`${styles.receiptEditor} ${compactReview ? styles.receiptEditorCompact : ""}`}
          open={!compactReview}
        >
          <summary>内容を確認・編集</summary>
          <div className="review-merchant">
            <label>
              店名
              <input
                value={receipt.merchant ?? ""}
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
            {!purchaseTime && <small>時刻を入力してください</small>}
            <details className={styles.purchaseTimeEditor}>
              <summary>{purchaseTime ? "時刻を編集" : "時刻を入力"}</summary>
              <label>
                購入時刻
                <input
                  type="time"
                  value={purchaseTime}
                  onChange={(event) => onPurchaseTimeChange(event.target.value)}
                />
              </label>
            </details>
          </div>
          <div className="field-group" id="receipt-review-payments" tabIndex={-1}>
            <span>決済内訳</span>
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
                          ? Object.assign({}, entry, {
                              paymentAccountId: event.target.value,
                            })
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
                    setReceiptPayments((current) => {
                      const payments =
                        current[selectedDocument.id] ?? paymentDraftFor(selectedDocument);
                      return {
                        ...current,
                        [selectedDocument.id]: updateReceiptPaymentAmount(
                          payments,
                          payment.key,
                          Number(event.target.value) || 0,
                          receiptTotal(selectedDocument),
                        ),
                      };
                    });
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
                setReceiptPayments((previous) => {
                  const payments =
                    previous[selectedDocument.id] ?? paymentDraftFor(selectedDocument);
                  return {
                    ...previous,
                    [selectedDocument.id]: addReceiptPayment(payments, {
                      key: crypto.randomUUID(),
                      paymentAccountId: "",
                      amountYen: 0,
                    }),
                  };
                });
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
          {!receiptItems.length && <p className="receipt-items-empty">品目がありません。</p>}
          {receiptItems.map((item, index) => {
            const itemKey = item.key ?? `item-${index}`;
            const reviewState =
              receiptItemReviewStates[selectedDocument.id]?.[itemKey] ?? "UNREVIEWED";
            const provenance =
              item.verification === "MANUAL"
                ? "手入力"
                : item.region
                  ? "原本領域あり"
                  : "原本領域なし";
            return (
              <div
                className={`receipt-item-review ${amountDifferences.some((difference) => difference.index === index) ? "receipt-item-candidate-difference" : ""} ${evidenceSelection?.itemKey === item.key ? styles.receiptItemEvidenceSelected : ""}`}
                key={item.key}
                onFocusCapture={() =>
                  selectReceiptEvidenceItem(selectedDocument.id, {
                    itemKey: item.key ?? `item-${index}`,
                    index,
                    focusEditor: false,
                  })
                }
                onMouseEnter={() =>
                  selectReceiptEvidenceItem(selectedDocument.id, {
                    itemKey,
                    index,
                    focusEditor: false,
                  })
                }
              >
                <div>
                  <input
                    id={`receipt-review-item-${encodeURIComponent(itemKey)}`}
                    aria-label="品名"
                    ref={(element) => {
                      if (element) itemNameInputs.current.set(itemKey, element);
                      else itemNameInputs.current.delete(itemKey);
                      if (item.verification !== "MANUAL" || index !== receiptItems.length - 1)
                        return;
                      if (!element || !focusNewItem.current) return;
                      focusNewItem.current = false;
                      element.focus();
                    }}
                    value={item.name}
                    onChange={(event) =>
                      editItem(selectedDocument.id, index, { name: event.target.value })
                    }
                  />
                  <small>
                    {item.verification === "MANUAL"
                      ? "手入力"
                      : `AI認識 ${item.verification} · ${Math.round(item.confidence * 100)}%`}
                  </small>
                  <small className={styles.receiptItemProvenance}>{provenance}</small>
                  {item.printedName && item.printedName !== item.name && (
                    <small className={styles.receiptItemProvenance}>印字名と異なる</small>
                  )}
                  {item.verification === "UNRESOLVED" && (
                    <small className={styles.receiptItemProvenance}>税解釈未確定</small>
                  )}
                  <fieldset className={styles.receiptItemReviewState}>
                    <legend>
                      {item.name || `品目${index + 1}`}の確認状況:{" "}
                      {receiptItemReviewStateLabels[reviewState]}
                    </legend>
                    {(reviewState === "UNREVIEWED" || reviewState === "UNRESOLVED") && (
                      <button
                        type="button"
                        className="button button-primary"
                        onClick={() => setReceiptItemReviewState(itemKey, "CONFIRMED")}
                      >
                        {reviewState === "UNRESOLVED" ? "確認済みにする" : "一致を確認"}
                      </button>
                    )}
                    {reviewState !== "UNRESOLVED" && (
                      <button
                        type="button"
                        className="button button-soft"
                        onClick={() => setReceiptItemReviewState(itemKey, "UNRESOLVED")}
                      >
                        未解決にする
                      </button>
                    )}
                  </fieldset>
                  {amountDifferences.some((difference) => difference.index === index) && (
                    <small className="candidate-difference-label">候補差あり</small>
                  )}
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
                  <EditPurchaseItemAllocations
                    allocations={itemAllocationsFor(
                      selectedDocument.id,
                      item.key ?? `item-${index}`,
                    )}
                    defaultAllocations={defaultAllocations}
                    pools={household.pools}
                    onChange={(allocations) =>
                      editItemAllocations(
                        selectedDocument.id,
                        item.key ?? `item-${index}`,
                        allocations,
                      )
                    }
                  />
                </div>
                <label>
                  帳簿に登録する金額
                  <small>レシート印字額 {yen(item.printedAmount)}</small> ¥
                  {(() => {
                    const amount = effectiveReceiptItemAmount(
                      item,
                      index,
                      selectedDocument.result?.candidates?.find(
                        (candidate) => candidate.id === selectedCandidates[selectedDocument.id],
                      )?.amounts,
                      manualAmountOverrides[selectedDocument.id],
                    );
                    const hasDraft = Object.hasOwn(
                      receiptAmountDrafts[selectedDocument.id] ?? {},
                      itemKey,
                    );
                    const draft = receiptAmountDrafts[selectedDocument.id]?.[itemKey];
                    return (
                      <input
                        aria-label={`${item.name}の帳簿に登録する金額`}
                        inputMode="numeric"
                        value={hasDraft ? draft : String(amount)}
                        onChange={(event) => {
                          markUnsaved();
                          setReceiptAmountDrafts((current) => ({
                            ...current,
                            [selectedDocument.id]: {
                              ...current[selectedDocument.id],
                              [itemKey]: event.target.value,
                            },
                          }));
                        }}
                        onBlur={() => {
                          if (!hasDraft) return;
                          const nextAmount = parseReceiptReviewAmountDraft(draft);
                          if (nextAmount !== null) {
                            setManualAmountOverrides((current) => ({
                              ...current,
                              [selectedDocument.id]: {
                                ...current[selectedDocument.id],
                                [itemKey]: nextAmount,
                              },
                            }));
                            editItem(selectedDocument.id, index, {
                              grossAmount: nextAmount,
                            });
                          }
                          setReceiptAmountDrafts((current) => {
                            const documentDrafts = { ...current[selectedDocument.id] };
                            delete documentDrafts[itemKey];
                            return { ...current, [selectedDocument.id]: documentDrafts };
                          });
                        }}
                      />
                    );
                  })()}
                  {item.verification === "MANUAL" && item.grossAmount < 1 && (
                    <small role="alert">1円以上を入力してください。</small>
                  )}
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
                <button
                  className="text-button receipt-item-delete"
                  type="button"
                  aria-label={`${item.name || `品目${index + 1}`}を削除`}
                  onClick={() => {
                    const keyedItems = ensureReceiptItemKeys(receiptItems);
                    const stableItemKey = keyedItems[index]?.key ?? itemKey;
                    updateAmountDraftKeys(
                      receiptItems,
                      keyedItems.filter((entry) => entry.key !== stableItemKey),
                    );
                    deleteItem(selectedDocument.id, stableItemKey);
                  }}
                >
                  <X size={16} />
                  品目を削除
                </button>
              </div>
            );
          })}
          <button
            type="button"
            className="text-button receipt-item-add"
            onClick={() => {
              const keyedItems = ensureReceiptItemKeys(receiptItems);
              updateAmountDraftKeys(receiptItems, keyedItems);
              focusNewItem.current = true;
              addItem(selectedDocument.id);
            }}
          >
            <Plus size={14} />
            品目を追加
          </button>
          {!!candidates.length && (
            <div className="candidate-review" id="receipt-review-candidates" tabIndex={-1}>
              <div className="panel-heading">
                <div>
                  <span className="eyebrow">金額の解釈</span>
                  <h3>計算候補</h3>
                </div>
              </div>
              {itemsChanged && (
                <small>品目構成を変更したため税額候補は無効です。金額を確認してください。</small>
              )}
              {candidates.map((candidate, candidateIndex) => {
                const differences = amountDifferences.flatMap((difference) => {
                  const amount = candidateAmountForItem(candidate, difference);
                  return amount ? [amount] : [];
                });
                const interpretationLabels = itemsChanged
                  ? []
                  : candidateInterpretationLabels(candidate, {
                      items: receiptItems,
                      taxGroups: receipt.taxGroups ?? [],
                    });
                const selected = selectedCandidates[selectedDocument.id] === candidate.id;
                return (
                  <div className="candidate-option-row" key={candidate.id}>
                    <button
                      type="button"
                      className={`candidate-option ${selected ? "candidate-selected" : ""}`}
                      disabled={itemsChanged}
                      onClick={() => {
                        markUnsaved();
                        if (!selected) {
                          setManualAmountOverrides((current) => {
                            const next = { ...current };
                            delete next[selectedDocument.id];
                            return next;
                          });
                        }
                        setSelectedCandidates((current) => ({
                          ...current,
                          [selectedDocument.id]: candidate.id,
                        }));
                      }}
                    >
                      <span>
                        <b>候補{candidateIndex + 1}</b>
                        <small>税解釈候補</small>
                        {interpretationLabels.map((label) => (
                          <small className="candidate-interpretation-label" key={label}>
                            {label}
                          </small>
                        ))}
                        {!itemsChanged && interpretationLabels.length === 0 && (
                          <small>税区分変更なし</small>
                        )}
                        {differences.slice(0, 3).map((difference) => (
                          <small className="candidate-amount-difference" key={difference.index}>
                            {difference.name} {yen(difference.amount)}
                          </small>
                        ))}
                        {differences.length > 3 && <small>ほか{differences.length - 3}品目</small>}
                        <small>
                          {candidate.consistent ? "計算一致" : "計算不一致"}
                          {candidate.issues.length ? ` · ${candidate.issues.join("、")}` : ""}
                        </small>
                      </span>
                      <strong>{yen(candidate.computedTotal)}</strong>
                    </button>
                    {differences.length > 0 && (
                      <button
                        className="candidate-difference-jump"
                        type="button"
                        onClick={() => {
                          focusReceiptItem(differences[0].index);
                        }}
                      >
                        差額品目へ
                      </button>
                    )}
                  </div>
                );
              })}
            </div>
          )}
          {(Object.keys(manualAmountOverrides[selectedDocument.id] ?? {}).length > 0 ||
            (selectedCandidates[selectedDocument.id]
              ? !result.candidates?.find(
                  (candidate) => candidate.id === selectedCandidates[selectedDocument.id],
                )?.consistent
              : !result.balanced)) && (
            <div className="field-group" id="receipt-review-amount-reason" tabIndex={-1}>
              <fieldset className={styles.amountReasonOptions}>
                <legend>金額の訂正理由</legend>
                {manualAmountOverrideReasonOptions.map((reason) => (
                  <button
                    className={`button ${selectedManualAmountOverrideReason === reason ? "button-primary" : "button-soft"}`}
                    type="button"
                    aria-pressed={selectedManualAmountOverrideReason === reason}
                    key={reason}
                    onClick={() => setManualAmountOverrideReason(reason)}
                  >
                    {reason}
                  </button>
                ))}
              </fieldset>
              {selectedManualAmountOverrideReason === "その他" && (
                <label>
                  その他の内容
                  <input
                    maxLength={500}
                    value={otherManualAmountOverrideReason}
                    placeholder="補足を入力（任意）"
                    onChange={(event) =>
                      setManualAmountOverrideReason(event.target.value || "その他")
                    }
                  />
                </label>
              )}
            </div>
          )}
        </details>
        <div className="review-total">
          <span>帳簿に登録する金額の合計</span>
          <b>{yen(receiptTotal(selectedDocument))}</b>
        </div>
        {uncategorizedItemCount > 0 && (
          <small className={styles.uncategorizedNotice}>未分類 {uncategorizedItemCount}品目</small>
        )}
        <button
          type="button"
          className="button button-primary button-wide"
          disabled={pending || !purchaseDate || !purchaseTime || !validReceiptItems}
          onClick={() => onConfirm()}
        >
          <Check size={16} />
          帳簿に追加
        </button>
      </div>
    </div>
  );
}
