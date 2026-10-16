import { BookOpen } from "lucide-react";
import { useState } from "react";

import type { Household, Purchase } from "../../../shared/api";
import type { LedgerSearch } from "../model/ledger-search";

import { EmptyState, InlineError } from "../../../shared/ui";
import { useLedgerItemBulkEdit } from "../model/ledger-item-bulk-edit";
import { useLedgerPurchaseEditor } from "../model/use-ledger-purchase-editor";
import { PurchaseItemEditor } from "./purchase-item-editor";

import styles from "./ledger-search.module.css";

type LedgerItemBulkEditState = ReturnType<typeof useLedgerItemBulkEdit>;
type PurchaseEditorState = ReturnType<typeof useLedgerPurchaseEditor>;

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

const purchaseMonth = (occurredAt: string) =>
  new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
  }).format(new Date(occurredAt));

export function LedgerItemsList({
  household,
  purchases,
  editor,
  bulkEdit,
  loadingMore,
  moreError,
  hasNextPage,
  loadMore,
  onAdd,
  updateFilters,
  setHighlightedEvidenceItem,
}: {
  household: Household;
  purchases: Purchase[];
  editor: PurchaseEditorState;
  bulkEdit: LedgerItemBulkEditState;
  loadingMore: boolean;
  moreError: string;
  hasNextPage: boolean;
  loadMore: () => void;
  onAdd: () => void;
  updateFilters: (updates: Partial<LedgerSearch>) => void;
  setHighlightedEvidenceItem: (itemId: string | null) => void;
}) {
  const [inlineEditingItem, setInlineEditingItem] = useState<string | null>(null);
  const filteredItems = purchases.flatMap((purchase) =>
    purchase.items.map((item) => ({ purchase, item })),
  );
  const itemCoverageComplete = !loadingMore && !hasNextPage && !moreError;
  const {
    catalog,
    correctionTarget,
    editing,
    itemDrafts,
    itemUndo,
    purchaseSaveState,
    cancelEditing,
    savePurchase,
    startEditing,
    undoItemFields,
    updateItemDraft,
  } = editor;
  const { pending: bulkPending, selectedItems, toggleSelection } = bulkEdit;
  return (
    <section className="panel ledger-list">
      {filteredItems.length === 0 && itemCoverageComplete ? (
        <EmptyState
          icon={<BookOpen />}
          title="品目が見つかりません"
          action="買い物を追加"
          onClick={onAdd}
        />
      ) : filteredItems.length === 0 ? (
        <div className="skeleton-list">
          <i />
          <i />
          <i />
        </div>
      ) : (
        filteredItems.map(({ purchase, item }) => (
          <div className="ledger-entry" key={item.id}>
            <div
              className={`ledger-item-row ${editing === purchase.id ? "ledger-item-row-editing" : ""}`}
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
                    "items",
                    filteredItems.map(({ item: visibleItem }) => visibleItem.id),
                    item.id,
                    event.nativeEvent instanceof MouseEvent && event.nativeEvent.shiftKey,
                  )
                }
                disabled={bulkPending || purchase.status === "CLOSED"}
              />
              {editing === purchase.id && inlineEditingItem === item.id && itemDrafts[item.id] ? (
                <div className="ledger-item-inline-editor">
                  <PurchaseItemEditor
                    item={item}
                    draft={itemDrafts[item.id]}
                    household={household}
                    merchant={purchase.merchant}
                    categories={household.categories}
                    catalog={catalog}
                    onChange={(updates) => updateItemDraft(item.id, updates)}
                  />
                  <div className="ledger-item-edit-actions">
                    <output
                      className={`purchase-edit-status purchase-edit-status-${purchaseSaveState}`}
                      aria-live="polite"
                    >
                      {purchaseSaveState === "saving"
                        ? "保存中"
                        : purchaseSaveState === "conflict"
                          ? "競合があります"
                          : purchaseSaveState === "unsaved"
                            ? "未保存"
                            : "保存済み"}
                    </output>
                    <button
                      className="button button-soft"
                      type="button"
                      disabled={purchaseSaveState === "saving"}
                      onClick={() => {
                        if (cancelEditing()) setInlineEditingItem(null);
                      }}
                    >
                      編集を終了
                    </button>
                    {itemUndo && (
                      <button
                        className="button button-soft"
                        type="button"
                        disabled={purchaseSaveState !== "saved"}
                        onClick={() => void undoItemFields()}
                      >
                        元に戻す
                      </button>
                    )}
                    <button
                      className="button button-primary"
                      type="button"
                      disabled={purchaseSaveState !== "unsaved"}
                      onClick={() => void savePurchase(purchase)}
                    >
                      変更を保存
                    </button>
                  </div>
                </div>
              ) : (
                <>
                  <div className="purchase-description">
                    <b>{item.name}</b>
                    <span>
                      {purchase.merchant || "店舗未入力"} · {purchaseMonth(purchase.occurredAt)} ·{" "}
                      {household.categories.find((category) => category.id === item.categoryId)
                        ?.name ?? "未分類"}
                      {item.allocations.length > 0 &&
                        ` · ${item.allocations.map((allocation) => allocation.poolName).join("・")}`}
                      {item.tags.length > 0 && ` · ${item.tags.map((tag) => tag.name).join("・")}`}
                      {purchase.status === "CLOSED" && " · 締め済み"}
                    </span>
                  </div>
                  <div className="purchase-meta">
                    <b>{yen(item.amountYen)}</b>
                    <span>購入 {yen(purchase.totalYen)}</span>
                  </div>
                  <div className="ledger-item-row-actions">
                    <button
                      className="text-button"
                      type="button"
                      disabled={
                        editing !== null ||
                        correctionTarget !== null ||
                        purchase.status === "CLOSED"
                      }
                      onClick={() => {
                        setInlineEditingItem(item.id);
                        startEditing(purchase, item.id);
                      }}
                    >
                      品目を編集
                    </button>
                    <button
                      className="text-button"
                      type="button"
                      disabled={editing !== null}
                      onClick={() => {
                        updateFilters({
                          recordMode: "purchases",
                          viewMode: "purchases",
                          purchaseId: purchase.id,
                        });
                        setHighlightedEvidenceItem(item.id);
                      }}
                    >
                      購入を開く
                    </button>
                  </div>
                </>
              )}
            </div>
          </div>
        ))
      )}
      {loadingMore && <div className={styles.more}>購入履歴を取得中</div>}
      {moreError && <InlineError message={moreError} />}
      {hasNextPage && !loadingMore && !moreError && (
        <div className={styles.more}>
          <button className="button button-soft" type="button" onClick={loadMore}>
            さらに品目を読み込む
          </button>
        </div>
      )}
      {moreError && hasNextPage && (
        <div className={styles.more}>
          <button
            className="button button-soft"
            type="button"
            disabled={loadingMore}
            onClick={() => loadMore()}
          >
            {loadingMore ? "読み込み中" : "取得を再試行"}
          </button>
        </div>
      )}
    </section>
  );
}
