import { Search } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import type { Household, Tag } from "../../../shared/api";
import type { LedgerSearch } from "../model/ledger-search";

import { isOneOf } from "../../../shared/lib";

import styles from "./ledger-search.module.css";

export function LedgerFilters({
  household,
  filters,
  tags,
  recordCount,
  onFiltersChange,
  onClearSelection,
}: {
  household: Household;
  filters: LedgerSearch;
  tags: Tag[];
  recordCount: string;
  onFiltersChange: (updates: Partial<LedgerSearch>) => void;
  onClearSelection: () => void;
}) {
  const updateFilters = (updates: Partial<LedgerSearch>) => onFiltersChange(updates);
  const [searchInput, setSearchInput] = useState({ source: filters.search, value: filters.search });
  const searchDraft = searchInput.source === filters.search ? searchInput.value : filters.search;
  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pendingSearch = useRef<string | null>(null);
  const {
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
  } = filters;

  useEffect(() => {
    if (pendingSearch.current !== search) {
      pendingSearch.current = null;
      if (searchTimer.current) clearTimeout(searchTimer.current);
    }
  }, [search]);

  useEffect(
    () => () => {
      if (searchTimer.current) clearTimeout(searchTimer.current);
    },
    [],
  );

  const flushSearch = () => {
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = null;
    const nextSearch = pendingSearch.current;
    pendingSearch.current = null;
    if (nextSearch !== null && nextSearch !== search) updateFilters({ search: nextSearch });
  };

  const changeSearch = (value: string) => {
    setSearchInput({ source: search, value });
    pendingSearch.current = value;
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => {
      searchTimer.current = null;
      const nextSearch = pendingSearch.current;
      pendingSearch.current = null;
      if (nextSearch !== null) updateFilters({ search: nextSearch });
    }, 300);
  };

  const activeDetailCount = (
    recordMode === "purchases"
      ? [accountFilter, tagFilter, originFilter, evidenceFilter, minimumAmount, maximumAmount]
      : [observationSourceFilter]
  ).filter(Boolean).length;
  const activeFilters: { label: string; clear: () => void }[] = [];
  const addActiveFilter = (label: string, clear: () => void) =>
    activeFilters.push({ label, clear });

  if (fromMonth) addActiveFilter(`開始: ${fromMonth}`, () => updateFilters({ fromMonth: "" }));
  if (toMonth) addActiveFilter(`終了: ${toMonth}`, () => updateFilters({ toMonth: "" }));
  if (recordMode === "purchases") {
    if (search) addActiveFilter(`検索: ${search}`, () => updateFilters({ search: "" }));
    if (categoryFilter) {
      const categoryName =
        categoryFilter === "__uncategorized"
          ? "未分類"
          : (household.categories.find((category) => category.id === categoryFilter)?.name ??
            "カテゴリ");
      addActiveFilter(`カテゴリ: ${categoryName}`, () => updateFilters({ categoryFilter: "" }));
    }
    if (poolFilter) {
      const poolName = household.pools.find((pool) => pool.id === poolFilter)?.name ?? "負担先";
      addActiveFilter(`負担先: ${poolName}`, () => updateFilters({ poolFilter: "" }));
    }
    if (accountFilter) {
      const accountName =
        household.accounts.find((account) => account.id === accountFilter)?.name ?? "決済元";
      addActiveFilter(`決済元: ${accountName}`, () => updateFilters({ accountFilter: "" }));
    }
    if (tagFilter) {
      const tagName = tags.find((tag) => tag.id === tagFilter)?.name ?? "タグ";
      addActiveFilter(`タグ: ${tagName}`, () => updateFilters({ tagFilter: "" }));
    }
    if (originFilter) {
      const originName = { AI: "AI入力", MANUAL: "手入力", UNKNOWN: "不明" }[originFilter];
      addActiveFilter(`入力元: ${originName}`, () => updateFilters({ originFilter: "" }));
    }
    if (evidenceFilter) {
      addActiveFilter(`原本: ${evidenceFilter === "true" ? "あり" : "なし"}`, () =>
        updateFilters({ evidenceFilter: "" }),
      );
    }
    if (minimumAmount) {
      addActiveFilter(`最低額: ${minimumAmount}円`, () => updateFilters({ minimumAmount: "" }));
    }
    if (maximumAmount) {
      addActiveFilter(`最高額: ${maximumAmount}円`, () => updateFilters({ maximumAmount: "" }));
    }
  } else {
    if (observationStateFilter !== "UNMATCHED") {
      const stateName = observationStateFilter === "CONFIRMED" ? "照合済み" : "すべて";
      addActiveFilter(`状態: ${stateName}`, () =>
        updateFilters({ observationStateFilter: "UNMATCHED" }),
      );
    }
    if (observationSourceFilter) {
      const sourceName = {
        RECEIPT: "レシート",
        ORDER: "注文",
        EMAIL: "メール",
        CARD: "カード",
        BANK: "銀行",
      }[observationSourceFilter];
      addActiveFilter(`入力元: ${sourceName}`, () =>
        updateFilters({ observationSourceFilter: "" }),
      );
    }
  }

  return (
    <>
      <div className={styles.controls}>
        {recordMode === "purchases" && (
          <label className={`input-with-icon ${styles.search}`}>
            <Search size={16} aria-hidden="true" />
            <input
              value={searchDraft}
              onChange={(event) => changeSearch(event.target.value)}
              onBlur={flushSearch}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  flushSearch();
                }
              }}
              placeholder="店名、品名で検索"
              aria-label="店名、品名で検索"
            />
          </label>
        )}
        <label className="select-filter">
          <span>開始</span>
          <input
            aria-label="開始月"
            type="month"
            value={fromMonth}
            onChange={(event) => updateFilters({ fromMonth: event.target.value })}
          />
        </label>
        <label className="select-filter">
          <span>終了</span>
          <input
            aria-label="終了月"
            type="month"
            value={toMonth}
            onChange={(event) => updateFilters({ toMonth: event.target.value })}
          />
        </label>
        <div className="record-count">{recordCount}</div>
      </div>
      <div className={styles.commonFilters}>
        <label className="select-filter">
          <span>表示</span>
          <select
            value={recordMode}
            onChange={(event) => {
              const mode = isOneOf(event.target.value, ["purchases", "observations"])
                ? event.target.value
                : "purchases";
              updateFilters({ recordMode: mode });
              if (mode === "observations") onClearSelection();
            }}
          >
            <option value="purchases">購入履歴</option>
            <option value="observations">照合候補</option>
          </select>
        </label>
        {recordMode === "purchases" ? (
          <>
            <label className="select-filter">
              <span>単位</span>
              <select
                value={viewMode}
                onChange={(event) => {
                  updateFilters({
                    viewMode: isOneOf(event.target.value, ["purchases", "items"])
                      ? event.target.value
                      : "purchases",
                  });
                  onClearSelection();
                }}
              >
                <option value="purchases">購入</option>
                <option value="items">品目</option>
              </select>
            </label>
            <label className="select-filter">
              <span>カテゴリ</span>
              <select
                value={categoryFilter}
                onChange={(event) => updateFilters({ categoryFilter: event.target.value })}
              >
                <option value="">すべて</option>
                <option value="__uncategorized">未分類</option>
                {household.categories.map((category) => (
                  <option key={category.id} value={category.id}>
                    {category.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="select-filter">
              <span>負担先</span>
              <select
                value={poolFilter}
                onChange={(event) => updateFilters({ poolFilter: event.target.value })}
              >
                <option value="">すべて</option>
                {household.pools.map((pool) => (
                  <option key={pool.id} value={pool.id}>
                    {pool.name}
                  </option>
                ))}
              </select>
            </label>
          </>
        ) : (
          <label className="select-filter">
            <span>状態</span>
            <select
              value={observationStateFilter}
              onChange={(event) =>
                updateFilters({
                  observationStateFilter: isOneOf(event.target.value, ["CONFIRMED", "UNMATCHED"])
                    ? event.target.value
                    : "",
                })
              }
            >
              <option value="UNMATCHED">未照合</option>
              <option value="CONFIRMED">照合済み</option>
              <option value="">すべて</option>
            </select>
          </label>
        )}
      </div>
      <details className={styles.details}>
        <summary>絞り込み{activeDetailCount > 0 && <span>{activeDetailCount}</span>}</summary>
        <div className={styles.filters}>
          {recordMode === "purchases" ? (
            <>
              <label className="select-filter">
                <span>決済元</span>
                <select
                  value={accountFilter}
                  onChange={(event) => updateFilters({ accountFilter: event.target.value })}
                >
                  <option value="">すべて</option>
                  {household.accounts.map((account) => (
                    <option key={account.id} value={account.id}>
                      {account.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="select-filter">
                <span>タグ</span>
                <select
                  value={tagFilter}
                  onChange={(event) => updateFilters({ tagFilter: event.target.value })}
                >
                  <option value="">すべて</option>
                  {tags
                    .filter((tag) => !tag.archived)
                    .map((tag) => (
                      <option key={tag.id} value={tag.id}>
                        {tag.name}
                      </option>
                    ))}
                </select>
              </label>
              <label className="select-filter">
                <span>入力元</span>
                <select
                  value={originFilter}
                  onChange={(event) =>
                    updateFilters({
                      originFilter: isOneOf(event.target.value, ["AI", "MANUAL", "UNKNOWN"])
                        ? event.target.value
                        : "",
                    })
                  }
                >
                  <option value="">すべて</option>
                  <option value="AI">AI入力</option>
                  <option value="MANUAL">手入力</option>
                  <option value="UNKNOWN">不明</option>
                </select>
              </label>
              <label className="select-filter">
                <span>原本</span>
                <select
                  value={evidenceFilter}
                  onChange={(event) =>
                    updateFilters({
                      evidenceFilter: isOneOf(event.target.value, ["true", "false"])
                        ? event.target.value
                        : "",
                    })
                  }
                >
                  <option value="">すべて</option>
                  <option value="true">あり</option>
                  <option value="false">なし</option>
                </select>
              </label>
              <label className="select-filter">
                <span>{viewMode === "items" ? "品目額" : "購入額"}</span>
                <input
                  aria-label="最低金額"
                  type="number"
                  min="0"
                  placeholder="下限"
                  value={minimumAmount}
                  onChange={(event) => updateFilters({ minimumAmount: event.target.value })}
                />
                <span>〜</span>
                <input
                  aria-label="最高金額"
                  type="number"
                  min="0"
                  placeholder="上限"
                  value={maximumAmount}
                  onChange={(event) => updateFilters({ maximumAmount: event.target.value })}
                />
              </label>
            </>
          ) : (
            <label className="select-filter">
              <span>入力元</span>
              <select
                value={observationSourceFilter}
                onChange={(event) =>
                  updateFilters({
                    observationSourceFilter: isOneOf(event.target.value, [
                      "RECEIPT",
                      "ORDER",
                      "EMAIL",
                      "CARD",
                      "BANK",
                    ])
                      ? event.target.value
                      : "",
                  })
                }
              >
                <option value="">すべて</option>
                <option value="RECEIPT">レシート</option>
                <option value="ORDER">注文</option>
                <option value="EMAIL">メール</option>
                <option value="CARD">カード</option>
                <option value="BANK">銀行</option>
              </select>
            </label>
          )}
        </div>
      </details>
      {activeFilters.length > 0 && (
        <div className={styles.activeFilters} aria-label="適用中の条件">
          {activeFilters.map(({ label, clear }) => (
            <button
              key={label}
              className={styles.filterChip}
              type="button"
              aria-label={`${label}を解除`}
              onClick={clear}
            >
              {label}
              <span aria-hidden="true">×</span>
            </button>
          ))}
        </div>
      )}
    </>
  );
}
