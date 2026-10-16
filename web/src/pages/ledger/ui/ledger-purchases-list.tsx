import { BookOpen, ChevronDown, ChevronRight } from "lucide-react";
import { useEffect, useRef } from "react";

import type { Household, Purchase } from "../../../shared/api";
import type { LedgerSearch } from "../model/ledger-search";

import { purchaseOccurredAtLabel, useHouseholdPurchases } from "../../../entities/purchase";
import { EmptyState, InlineError } from "../../../shared/ui";
import { useLedgerItemBulkEdit } from "../model/ledger-item-bulk-edit";
import { usePurchaseBulkEdit } from "../model/purchase-bulk-edit";
import { getRemainingRefundAmount } from "../model/purchase-correction";
import { useLedgerPurchaseEditor } from "../model/use-ledger-purchase-editor";
import { usePurchaseEvidence } from "../model/use-purchase-evidence";
import { PurchaseCorrection } from "./purchase-correction";
import { PurchaseEditForm } from "./purchase-edit-form";
import { PurchaseEvidence } from "./purchase-evidence";
import { PurchaseItemEditor } from "./purchase-item-editor";

import styles from "./ledger-search.module.css";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

type PurchaseQueryState = ReturnType<typeof useHouseholdPurchases>;

export function LedgerPurchasesList({
  household,
  purchases,
  filters,
  onFiltersChange,
  purchaseBulkEdit,
  ledgerItemBulkEdit,
  purchaseEditor,
  correctionPages,
  loadCorrections,
  refresh,
  highlightedEvidenceItem,
  setHighlightedEvidenceItem,
  filtersActive,
  loadingMore,
  moreError,
  hasNextPage,
  loadMore,
  onAdd,
}: {
  household: Household;
  purchases: Purchase[];
  filters: LedgerSearch;
  onFiltersChange: (updates: Partial<LedgerSearch>) => void;
  purchaseBulkEdit: ReturnType<typeof usePurchaseBulkEdit>;
  ledgerItemBulkEdit: ReturnType<typeof useLedgerItemBulkEdit>;
  purchaseEditor: ReturnType<typeof useLedgerPurchaseEditor>;
  correctionPages: PurchaseQueryState["correctionPages"];
  loadCorrections: PurchaseQueryState["loadCorrections"];
  refresh: () => void | Promise<void>;
  highlightedEvidenceItem: string | null;
  setHighlightedEvidenceItem: (itemId: string | null) => void;
  filtersActive: boolean;
  loadingMore: boolean;
  moreError: string;
  hasNextPage: boolean;
  loadMore: () => void;
  onAdd: () => void;
}) {
  const purchaseRowRefs = useRef(new Map<string, HTMLDivElement>());
  const expanded = filters.purchaseId || null;
  const { evidence: purchaseEvidence, error: evidenceError } = usePurchaseEvidence(expanded);
  const purchaseCount = purchases.length;
  useEffect(() => {
    if (!expanded || purchaseCount === 0) return;
    purchaseRowRefs.current.get(expanded)?.scrollIntoView({ behavior: "smooth", block: "center" });
  }, [expanded, purchaseCount]);
  const {
    catalog,
    correctionTarget,
    editing,
    itemDrafts,
    postDraftPurchase,
    postingPurchase,
    purchaseDraft,
    purchaseSaveState,
    savePurchase,
    setCorrectionTarget,
    startCorrection,
    startEditing,
    updateItemDraft,
    updatePurchaseDraft,
    cancelEditing,
  } = purchaseEditor;
  const { pending: bulkPending, selectedItems, toggleSelection } = ledgerItemBulkEdit;
  return (
    <>
      <section className="panel ledger-list">
        {filtersActive && loadingMore ? (
          <div className="skeleton-list">
            <i />
            <i />
            <i />
          </div>
        ) : purchases.length === 0 ? (
          <EmptyState
            icon={<BookOpen />}
            title="買い物が見つかりません"
            action="買い物を追加"
            onClick={onAdd}
          />
        ) : (
          purchases.map((purchase) => (
            <div
              key={purchase.id}
              ref={(node) => {
                if (node) purchaseRowRefs.current.set(purchase.id, node);
                else purchaseRowRefs.current.delete(purchase.id);
              }}
              className={`ledger-entry ${expanded === purchase.id ? "ledger-expanded" : ""}`}
            >
              <label>
                <input
                  type="checkbox"
                  aria-label={`${purchase.merchant || "店舗未入力"}の購入を選択`}
                  checked={purchaseBulkEdit.selectedPurchases.has(purchase.id)}
                  disabled={purchaseBulkEdit.pending || purchase.status === "CLOSED"}
                  onChange={(event) =>
                    purchaseBulkEdit.selectPurchase(
                      purchases.map((visiblePurchase) => visiblePurchase.id),
                      purchase.id,
                      event.nativeEvent instanceof MouseEvent && event.nativeEvent.shiftKey,
                    )
                  }
                />
                選択
              </label>
              <button
                className="ledger-purchase"
                type="button"
                onClick={() => {
                  setHighlightedEvidenceItem(null);
                  const opening = expanded !== purchase.id;
                  onFiltersChange({ purchaseId: opening ? purchase.id : "" });
                  if (
                    opening &&
                    (!correctionPages[purchase.id] || correctionPages[purchase.id].error)
                  ) {
                    void loadCorrections(purchase.id);
                  }
                }}
              >
                <div className="merchant-icon">{purchase.merchant.slice(0, 1) || "買"}</div>
                <div className="purchase-description">
                  <b>{purchase.merchant || "店舗未入力"}</b>
                  <span>
                    {purchaseOccurredAtLabel(purchase.occurredAt, purchase.datePrecision)} ·{" "}
                    {purchase.items.length}品目 ·{" "}
                    {purchase.paymentAllocations.length
                      ? purchase.paymentAllocations
                          .map((payment) => {
                            const accountName =
                              payment.account?.name ??
                              household.accounts.find(
                                (account) => account.id === payment.paymentAccountId,
                              )?.name ??
                              "決済元";
                            return `${accountName} ${yen(payment.amountYen)}`;
                          })
                          .join(" / ")
                      : "決済元未設定"}
                  </span>
                </div>
                <div className="purchase-meta">
                  <b>{yen(purchase.totalYen)}</b>
                  <span>
                    {purchase.status === "CLOSED"
                      ? "締め済み"
                      : purchase.status === "DRAFT"
                        ? "下書き"
                        : "記録済み"}{" "}
                    ·{" "}
                    {purchase.origin === "AI"
                      ? "AI入力"
                      : purchase.origin === "MANUAL"
                        ? "手入力"
                        : "入力元不明"}
                    {purchase.hasEvidence ? " · 原本あり" : " · 原本なし"}
                  </span>
                </div>
                {expanded === purchase.id ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
              </button>
              {expanded === purchase.id && (
                <div className="expanded-items">
                  {purchase.status === "DRAFT" && (
                    <section className="panel ledger-evidence-panel">
                      <div className="panel-heading">
                        <div>
                          <span className="eyebrow">下書き</span>
                        </div>
                        <button
                          className="button button-primary"
                          type="button"
                          disabled={postingPurchase === purchase.id}
                          onClick={() => void postDraftPurchase(purchase)}
                        >
                          {postingPurchase === purchase.id ? "記録しています" : "支出に記録"}
                        </button>
                      </div>
                    </section>
                  )}
                  <PurchaseEvidence
                    purchase={purchase}
                    evidence={purchaseEvidence}
                    error={evidenceError}
                    highlightedItemId={highlightedEvidenceItem}
                    onHighlightItem={setHighlightedEvidenceItem}
                  />
                  {editing === purchase.id && purchaseDraft ? (
                    <PurchaseEditForm
                      purchase={purchase}
                      draft={purchaseDraft}
                      itemDrafts={itemDrafts}
                      accounts={household.accounts}
                      saveState={purchaseSaveState}
                      onChange={updatePurchaseDraft}
                      onCancel={cancelEditing}
                      onSave={savePurchase}
                    />
                  ) : (
                    <button
                      className="text-button"
                      type="button"
                      onClick={() => startEditing(purchase)}
                    >
                      買い物を編集
                    </button>
                  )}
                  {(purchase.corrections ?? []).map((correction) => (
                    <div className="purchase-correction-entry" key={correction.id}>
                      <span>
                        {correction.__typename === "PurchaseItemCorrection"
                          ? "品目返品"
                          : "購入訂正"}
                      </span>
                      <b
                        className={correction.amountDeltaYen < 0 ? "settlement-credit" : undefined}
                      >
                        {yen(correction.amountDeltaYen)}
                      </b>
                      <small>{correction.reason}</small>
                    </div>
                  ))}
                  {correctionPages[purchase.id]?.loading && <small>訂正履歴を取得中</small>}
                  {correctionPages[purchase.id]?.error && (
                    <>
                      <InlineError message={correctionPages[purchase.id].error} />
                      <button
                        className="text-button"
                        type="button"
                        disabled={correctionPages[purchase.id].loading}
                        onClick={() => void loadCorrections(purchase.id)}
                      >
                        再試行
                      </button>
                    </>
                  )}
                  {correctionPages[purchase.id]?.hasNextPage && (
                    <button
                      className="text-button"
                      type="button"
                      disabled={correctionPages[purchase.id].loading}
                      onClick={() =>
                        void loadCorrections(
                          purchase.id,
                          correctionPages[purchase.id].after ?? undefined,
                        )
                      }
                    >
                      {correctionPages[purchase.id].loading ? "読み込み中" : "過去の訂正を読み込む"}
                    </button>
                  )}
                  {purchase.items.map((item) => (
                    <div
                      className={`expanded-item ${editing === purchase.id ? "expanded-item-editing" : ""} ${highlightedEvidenceItem === item.id ? "evidence-item-highlighted" : ""}`}
                      key={item.id}
                    >
                      <input
                        className="item-select"
                        type="checkbox"
                        aria-label={
                          purchase.status === "CLOSED"
                            ? `${item.name}は締め済みのため選択できません`
                            : `${item.name}を選択`
                        }
                        checked={selectedItems.has(item.id)}
                        onChange={(event) =>
                          toggleSelection(
                            `purchase:${purchase.id}`,
                            purchase.items.map((visibleItem) => visibleItem.id),
                            item.id,
                            event.nativeEvent instanceof MouseEvent && event.nativeEvent.shiftKey,
                          )
                        }
                        disabled={
                          editing === purchase.id || bulkPending || purchase.status === "CLOSED"
                        }
                      />
                      {editing === purchase.id ? (
                        <div className="expanded-item-editor">
                          <PurchaseItemEditor
                            item={item}
                            draft={itemDrafts[item.id]}
                            household={household}
                            merchant={purchase.merchant}
                            categories={household.categories}
                            catalog={catalog}
                            onChange={(updates) => updateItemDraft(item.id, updates)}
                          />
                        </div>
                      ) : (
                        <>
                          <span>{item.name}</span>
                          <span>
                            {household.categories.find(
                              (category) => category.id === item.categoryId,
                            )?.name ?? "未分類"}
                          </span>
                          <b>{yen(item.amountYen)}</b>
                          <span>
                            {item.allocations
                              .map((allocation) => `${allocation.poolName} ${allocation.weight}`)
                              .join(" / ")}
                          </span>
                          {item.tags.length > 0 && (
                            <span className="item-tag-list">
                              {item.tags.map((tag) => (
                                <i key={tag.id}>{tag.name}</i>
                              ))}
                            </span>
                          )}
                        </>
                      )}
                      {editing !== purchase.id &&
                        (correctionTarget?.purchaseId === purchase.id &&
                        correctionTarget.itemId === item.id ? (
                          <PurchaseCorrection
                            householdId={household.id}
                            accounts={household.accounts}
                            purchase={purchase}
                            item={item}
                            onCancel={() => setCorrectionTarget(null)}
                            onSaved={async () => {
                              setCorrectionTarget(null);
                              await refresh();
                            }}
                          />
                        ) : (
                          <button
                            className="text-button correction-trigger"
                            type="button"
                            disabled={getRemainingRefundAmount(item) <= 0}
                            onClick={() => startCorrection(purchase, item)}
                          >
                            返品を記録
                          </button>
                        ))}
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))
        )}
      </section>
      {moreError && <InlineError message={moreError} />}
      {moreError && !hasNextPage && (
        <div className={styles.more}>
          <button className="button button-soft" type="button" onClick={refresh}>
            取得を再試行
          </button>
        </div>
      )}
      {hasNextPage && !moreError && (
        <div className={styles.more}>
          <button
            className="button button-soft"
            type="button"
            disabled={loadingMore}
            onClick={loadMore}
          >
            {loadingMore
              ? "読み込み中"
              : filtersActive
                ? "検索結果をさらに読み込む"
                : "古い記録を読み込む"}
          </button>
        </div>
      )}
      {moreError && hasNextPage && (
        <div className={styles.more}>
          <button
            className="button button-soft"
            type="button"
            disabled={loadingMore}
            onClick={loadMore}
          >
            {loadingMore ? "読み込み中" : "取得を再試行"}
          </button>
        </div>
      )}
    </>
  );
}
