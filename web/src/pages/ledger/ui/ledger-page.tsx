import { Plus } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useHouseholdPurchases } from "../../../entities/purchase";
import { readTags } from "../../../entities/tag";
import {
  type Household,
  type SourceKind,
  type Tag,
  type UnmatchedObservationState,
} from "../../../shared/api";
import { errorMessage } from "../../../shared/lib";
import { InlineError } from "../../../shared/ui";
import { useBulkDeletePurchaseItemsMutation } from "../api/bulk-delete-purchase-items";
import { useUnmatchedObservations } from "../api/observation/use-unmatched-observations";
import { useLedgerItemBulkEdit } from "../model/ledger-item-bulk-edit";
import { toPurchaseAmountVariables, type LedgerSearch } from "../model/ledger-search";
import { usePurchaseBulkEdit } from "../model/purchase-bulk-edit";
import { useLedgerPurchaseEditor } from "../model/use-ledger-purchase-editor";
import { LedgerFilters } from "./ledger-filters";
import { LedgerItemBulkActions } from "./ledger-item-bulk-actions";
import { LedgerItemsList } from "./ledger-items-list";
import { LedgerPurchasesList } from "./ledger-purchases-list";
import { ObservationLedger } from "./observation-ledger";
import { PurchaseBulkActions } from "./purchase-bulk-actions";

import "./ledger-page.css";

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
    () => ({
      ...filters,
      categoryFilter,
      poolFilter,
      accountFilter,
      tagFilter,
    }),
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
  const { mutate: deletePurchaseItems } = useBulkDeletePurchaseItemsMutation();
  const ledgerItemBulkEdit = useLedgerItemBulkEdit({
    householdId: household.id,
    purchases,
    refresh,
    deletePurchaseItems,
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
    purchaseSaveState,
    setEditError,
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
        setTagsState((current) => ({
          ...current,
          householdId: household.id,
          loaded: true,
        }));
      });
    return () => {
      active = false;
    };
  }, [household.id, setBulkError]);
  const filteredItems = purchases.flatMap((purchase) =>
    purchase.items.map((item) => ({ purchase, item })),
  );
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
            ? "照合候補"
            : viewMode === "items"
              ? `${filteredItems.length}品目表示 · ${purchases.length}/${totalCount}件購入取得`
              : `${purchases.length}件表示 · ${purchases.length}/${totalCount}件読込`
        }
        onFiltersChange={onFiltersChange}
        onClearSelection={() => {
          ledgerItemBulkEdit.clearSelection();
          purchaseBulkEdit.clearSelection();
        }}
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
        <ObservationLedgerData
          household={household}
          from={fromMonth || undefined}
          to={toMonth || undefined}
          sourceKind={observationSourceFilter || undefined}
          state={observationStateFilter || undefined}
          onUnsavedChanges={(dirty) => reportDirtySource("observations", dirty)}
        />
      )}
      {recordMode === "purchases" && viewMode === "items" && (
        <LedgerItemsList
          household={household}
          purchases={purchases}
          editor={purchaseEditor}
          bulkEdit={ledgerItemBulkEdit}
          loadingMore={loadingMore}
          moreError={moreError ?? ""}
          hasNextPage={hasNextPage}
          loadMore={() => loadMore()}
          onAdd={onAdd}
          updateFilters={updateFilters}
          setHighlightedEvidenceItem={setHighlightedEvidenceItem}
        />
      )}
      {recordMode === "purchases" && viewMode === "purchases" && (
        <LedgerPurchasesList
          household={household}
          purchases={purchases}
          filters={filters}
          onFiltersChange={onFiltersChange}
          purchaseBulkEdit={purchaseBulkEdit}
          ledgerItemBulkEdit={ledgerItemBulkEdit}
          purchaseEditor={purchaseEditor}
          correctionPages={correctionPages}
          loadCorrections={loadCorrections}
          refresh={refresh}
          highlightedEvidenceItem={highlightedEvidenceItem}
          setHighlightedEvidenceItem={setHighlightedEvidenceItem}
          filtersActive={filtersActive}
          loadingMore={loadingMore}
          moreError={moreError ?? ""}
          hasNextPage={hasNextPage}
          loadMore={() => loadMore()}
          onAdd={onAdd}
        />
      )}
    </>
  );
}

function ObservationLedgerData({
  household,
  from,
  to,
  sourceKind,
  state,
  onUnsavedChanges,
}: {
  household: Household;
  from?: string;
  to?: string;
  sourceKind?: SourceKind;
  state?: UnmatchedObservationState;
  onUnsavedChanges: (dirty: boolean) => void;
}) {
  const unmatched = useUnmatchedObservations(household.id, {
    from,
    to,
    sourceKind,
    state,
  });
  return (
    <ObservationLedger
      household={household}
      unmatched={unmatched}
      onUnsavedChanges={onUnsavedChanges}
    />
  );
}
