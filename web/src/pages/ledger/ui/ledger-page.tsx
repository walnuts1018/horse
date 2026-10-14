import { BookOpen, ChevronDown, ChevronRight, Plus } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useHouseholdPurchases } from "../../../entities/purchase";
import { readTags } from "../../../entities/tag";
import { type Household, type Tag } from "../../../shared/api";
import { errorMessage } from "../../../shared/lib";
import { EmptyState, InlineError } from "../../../shared/ui";
import { useUnmatchedObservations } from "../api/observation/use-unmatched-observations";
import { useLedgerItemBulkEdit } from "../model/ledger-item-bulk-edit";
import { toPurchaseAmountVariables, type LedgerSearch } from "../model/ledger-search";
import { usePurchaseBulkEdit } from "../model/purchase-bulk-edit";
import { getRemainingRefundAmount } from "../model/purchase-correction";
import { useLedgerPurchaseEditor } from "../model/use-ledger-purchase-editor";
import { usePurchaseEvidence } from "../model/use-purchase-evidence";
import { LedgerFilters } from "./ledger-filters";
import { LedgerItemBulkActions } from "./ledger-item-bulk-actions";
import { ObservationLedger } from "./observation-ledger";
import { PurchaseBulkActions } from "./purchase-bulk-actions";
import { PurchaseCorrection } from "./purchase-correction";
import { PurchaseEditForm } from "./purchase-edit-form";
import { PurchaseEvidence } from "./purchase-evidence";
import { PurchaseItemEditor } from "./purchase-item-editor";

import styles from "./ledger-search.module.css";
import "./ledger-page.css";

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
const nextMonth = (month: string) => {
  const [year, monthNumber] = month.split("-").map(Number);
  const serial = year * 12 + monthNumber;
  return `${Math.floor(serial / 12)}-${String((serial % 12) + 1).padStart(2, "0")}`;
};

export function LedgerPage({
  household,
  filters,
  onFiltersChange,
  onAdd,
  onUnsavedChanges,
}: {
  household: Household;
  filters: LedgerSearch;
  onFiltersChange: (updates: Partial<LedgerSearch>) => void;
  onAdd: () => void;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  const dirtySources = useRef(new Set<string>());
  const reportDirtySource = useCallback(
    (source: string, dirty: boolean) => {
      const sources = dirtySources.current;
      if (sources.has(source) === dirty) return;
      if (dirty) sources.add(source);
      else sources.delete(source);
      onUnsavedChanges(sources.size > 0);
    },
    [onUnsavedChanges],
  );
  useEffect(() => {
    const sources = dirtySources.current;
    return () => {
      sources.clear();
      onUnsavedChanges(false);
    };
  }, [onUnsavedChanges]);
  const {
    recordMode,
    viewMode,
    search,
    fromMonth,
    toMonth,
    categoryFilter: rawCategoryFilter,
    poolFilter: rawPoolFilter,
    accountFilter: rawAccountFilter,
    tagFilter: rawTagFilter,
    originFilter,
    evidenceFilter,
    observationSourceFilter,
    observationStateFilter,
    minimumAmount,
    maximumAmount,
  } = filters;
  // タグ一覧は世帯ごとに保持し、読み込み完了までは検証に使わない
  const [tagsState, setTagsState] = useState<{
    householdId: string;
    tags: Tag[];
    loaded: boolean;
  }>(() => ({ householdId: household.id, tags: [], loaded: false }));
  const tags = tagsState.tags;
  const tagsLoaded = tagsState.householdId === household.id && tagsState.loaded;
  // 存在しない絞り込み条件は無効化してレンダー中に導出する(親の状態には書き戻さない)
  const categoryFilter =
    rawCategoryFilter &&
    rawCategoryFilter !== "__uncategorized" &&
    !household.categories.some((category) => category.id === rawCategoryFilter)
      ? ""
      : rawCategoryFilter;
  const poolFilter =
    rawPoolFilter && !household.pools.some((pool) => pool.id === rawPoolFilter)
      ? ""
      : rawPoolFilter;
  const accountFilter =
    rawAccountFilter && !household.accounts.some((account) => account.id === rawAccountFilter)
      ? ""
      : rawAccountFilter;
  const tagFilter =
    tagsLoaded && rawTagFilter && !tags.some((tag) => tag.id === rawTagFilter) ? "" : rawTagFilter;
  const effectiveFilters = useMemo<LedgerSearch>(
    () => ({ ...filters, categoryFilter, poolFilter, accountFilter, tagFilter }),
    [filters, categoryFilter, poolFilter, accountFilter, tagFilter],
  );
  const updateFilters = (updates: Partial<LedgerSearch>) => onFiltersChange(updates);
  const filtersActive = Boolean(
    search ||
    fromMonth ||
    toMonth ||
    categoryFilter ||
    poolFilter ||
    accountFilter ||
    tagFilter ||
    originFilter ||
    evidenceFilter ||
    minimumAmount ||
    maximumAmount,
  );
  const unmatchedObservations = useUnmatchedObservations(household.id, {
    enabled: recordMode === "observations",
    from: fromMonth || undefined,
    to: toMonth || undefined,
    sourceKind: observationSourceFilter || undefined,
    state: observationStateFilter || undefined,
  });
  const {
    purchases,
    loadingMore,
    moreError,
    hasNextPage,
    totalCount,
    correctionPages,
    loadCorrections,
    loadMore,
    refresh,
  } = useHouseholdPurchases(household.id, 100, {
    from: fromMonth ? `${fromMonth}-01T00:00:00+09:00` : undefined,
    to: toMonth ? `${nextMonth(toMonth)}-01T00:00:00+09:00` : undefined,
    origin: recordMode === "purchases" ? originFilter || undefined : undefined,
    hasEvidence:
      recordMode === "purchases" && evidenceFilter !== "" ? evidenceFilter === "true" : undefined,
    search: recordMode === "purchases" ? search || undefined : undefined,
    categoryId:
      recordMode === "purchases" && categoryFilter && categoryFilter !== "__uncategorized"
        ? categoryFilter
        : undefined,
    uncategorized:
      recordMode === "purchases" && categoryFilter === "__uncategorized" ? true : undefined,
    tagId: recordMode === "purchases" ? tagFilter || undefined : undefined,
    paymentAccountId: recordMode === "purchases" ? accountFilter || undefined : undefined,
    poolId: recordMode === "purchases" ? poolFilter || undefined : undefined,
    ...(recordMode === "purchases"
      ? toPurchaseAmountVariables({ minimumAmount, maximumAmount }, viewMode)
      : {}),
  });
  const purchaseBulkEdit = usePurchaseBulkEdit({
    householdId: household.id,
    purchases,
    refresh,
    selectionKey: JSON.stringify([
      household.id,
      recordMode,
      viewMode,
      search,
      fromMonth,
      toMonth,
      categoryFilter,
      poolFilter,
      accountFilter,
      tagFilter,
      originFilter,
      evidenceFilter,
      observationSourceFilter,
      observationStateFilter,
      minimumAmount,
      maximumAmount,
    ]),
  });
  const ledgerItemBulkEdit = useLedgerItemBulkEdit({
    householdId: household.id,
    purchases,
    refresh,
    selectionKey: JSON.stringify([
      household.id,
      recordMode,
      viewMode,
      search,
      fromMonth,
      toMonth,
      categoryFilter,
      poolFilter,
      accountFilter,
      tagFilter,
      originFilter,
      evidenceFilter,
      observationSourceFilter,
      observationStateFilter,
      minimumAmount,
      maximumAmount,
    ]),
  });
  const setBulkError = ledgerItemBulkEdit.setError;
  const purchaseRowRefs = useRef(new Map<string, HTMLDivElement>());
  const expanded = filters.purchaseId || null;
  const purchaseCount = purchases.length;
  useEffect(() => {
    if (!expanded || purchaseCount === 0) return;
    purchaseRowRefs.current.get(expanded)?.scrollIntoView({ behavior: "smooth", block: "center" });
  }, [expanded, purchaseCount]);
  const { evidence: purchaseEvidence, error: evidenceError } = usePurchaseEvidence(expanded);
  const purchaseEditor = useLedgerPurchaseEditor({
    household,
    refresh,
    onDirtyChange: (dirty) => reportDirtySource("purchase-editor", dirty),
  });
  const {
    catalog,
    conflict,
    correctionTarget,
    editError,
    editing,
    itemDrafts,
    postDraftPurchase,
    postingPurchase,
    purchaseDraft,
    purchaseSaveState,
    savePurchase,
    setCorrectionTarget,
    setEditError,
    startCorrection,
    startEditing,
    updateItemDraft,
    updatePurchaseDraft,
    cancelEditing,
  } = purchaseEditor;
  const [highlightedEvidenceItem, setHighlightedEvidenceItem] = useState<string | null>(null);
  useEffect(() => {
    let active = true;
    void readTags(household.id)
      .then((result) => {
        if (!active) return;
        setTagsState({ householdId: household.id, tags: result, loaded: true });
      })
      .catch((reason) => {
        if (!active) return;
        setBulkError(errorMessage(reason));
        setTagsState((current) => ({ ...current, householdId: household.id, loaded: true }));
      });
    return () => {
      active = false;
    };
  }, [household.id, setBulkError]);
  const filtered = purchases;
  const filteredItems = purchases.flatMap((purchase) =>
    purchase.items.map((item) => ({ purchase, item })),
  );
  const itemCoverageComplete = !loadingMore && !hasNextPage && !moreError;
  const { selectedItems, pending: bulkPending, toggleSelection } = ledgerItemBulkEdit;
  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">購入と品目</span>
          <h1>帳簿</h1>
          <p>買い物の記録をまとめて確認</p>
        </div>
        <button className="button button-primary" type="button" onClick={onAdd}>
          <Plus size={17} />
          買い物を追加
        </button>
      </div>
      <LedgerFilters
        household={household}
        filters={effectiveFilters}
        tags={tags}
        recordCount={
          recordMode === "observations"
            ? `${unmatchedObservations.observations.length}/${unmatchedObservations.totalCount}件表示`
            : viewMode === "items"
              ? `${filteredItems.length}品目表示 · ${purchases.length}/${totalCount}件購入取得`
              : `${filtered.length}件表示 · ${purchases.length}/${totalCount}件読込`
        }
        onFiltersChange={onFiltersChange}
        onClearSelection={ledgerItemBulkEdit.clearSelection}
      />
      <PurchaseBulkActions
        accounts={household.accounts}
        bulkEdit={purchaseBulkEdit}
        showSelectionControls={recordMode === "purchases" && viewMode === "purchases"}
      />
      <LedgerItemBulkActions
        edit={ledgerItemBulkEdit}
        household={household}
        catalog={catalog}
        availableTags={tags}
        disabled={editing !== null || Boolean(correctionTarget)}
      />
      {editError && <InlineError message={editError} />}
      {conflict && (
        <section className="panel conflict-panel" aria-live="polite">
          <div>
            <b>{conflict.label}が別の更新を受けています</b>
            <span>あなたの変更: {conflict.localValue}</span>
            <span>最新の記録: {conflict.serverValue}</span>
          </div>
          <div className="conflict-actions">
            <button
              className="button button-soft"
              type="button"
              disabled={purchaseSaveState === "saving"}
              onClick={() => void conflict.discard()}
            >
              最新の記録を使う
            </button>
            <button
              className="button button-primary"
              type="button"
              disabled={purchaseSaveState === "saving"}
              onClick={() =>
                void conflict.retry().catch((reason) => setEditError(errorMessage(reason)))
              }
            >
              自分の変更を再適用
            </button>
          </div>
        </section>
      )}
      {recordMode === "observations" && (
        <ObservationLedger
          household={household}
          unmatched={unmatchedObservations}
          onUnsavedChanges={(dirty) => reportDirtySource("observations", dirty)}
        />
      )}
      {recordMode === "purchases" && viewMode === "items" && (
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
                <div className="ledger-item-row">
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
                  <button
                    className="text-button"
                    type="button"
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
              </div>
            ))
          )}
          {loadingMore && <div className={styles.more}>購入履歴を取得中</div>}
          {moreError && <InlineError message={moreError} />}
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
      )}
      {recordMode === "purchases" && viewMode === "purchases" && (
        <section className="panel ledger-list">
          {filtersActive && loadingMore ? (
            <div className="skeleton-list">
              <i />
              <i />
              <i />
            </div>
          ) : filtered.length === 0 ? (
            <EmptyState
              icon={<BookOpen />}
              title="買い物が見つかりません"
              action="買い物を追加"
              onClick={onAdd}
            />
          ) : (
            filtered.map((purchase) => (
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
                        filtered.map((visiblePurchase) => visiblePurchase.id),
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
                      {new Date(purchase.occurredAt).toLocaleString("ja-JP", {
                        month: "numeric",
                        day: "numeric",
                        hour: "2-digit",
                        minute: "2-digit",
                      })}{" "}
                      · {purchase.items.length}品目 ·{" "}
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
                  {expanded === purchase.id ? (
                    <ChevronDown size={16} />
                  ) : (
                    <ChevronRight size={16} />
                  )}
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
                          className={
                            correction.amountDeltaYen < 0 ? "settlement-credit" : undefined
                          }
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
                        {correctionPages[purchase.id].loading
                          ? "読み込み中"
                          : "過去の訂正を読み込む"}
                      </button>
                    )}
                    {purchase.items.map((item) => (
                      <div
                        className={`expanded-item ${highlightedEvidenceItem === item.id ? "evidence-item-highlighted" : ""}`}
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
                          <PurchaseItemEditor
                            item={item}
                            draft={itemDrafts[item.id]}
                            household={household}
                            categories={household.categories}
                            catalog={catalog}
                            onChange={(updates) => updateItemDraft(item.id, updates)}
                          />
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
                                refresh();
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
      )}
      {recordMode === "purchases" && moreError && <InlineError message={moreError} />}
      {recordMode === "purchases" && hasNextPage && !moreError && (
        <div className={styles.more}>
          <button
            className="button button-soft"
            type="button"
            disabled={loadingMore}
            onClick={() => loadMore()}
          >
            {loadingMore
              ? "読み込み中"
              : filtersActive
                ? "検索結果をさらに読み込む"
                : "古い記録を読み込む"}
          </button>
        </div>
      )}
      {recordMode === "purchases" && viewMode === "purchases" && moreError && hasNextPage && (
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
    </>
  );
}
