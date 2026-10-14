import { Search } from "lucide-react";

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

  return (
    <>
      <div className={styles.controls}>
        {recordMode === "purchases" && (
          <label className="input-with-icon">
            <Search size={16} />
            <input
              value={search}
              onChange={(event) => updateFilters({ search: event.target.value })}
              placeholder="店名、品名で検索"
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
      <div className={styles.filters}>
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
        {recordMode === "purchases" && (
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
        )}
        {recordMode === "purchases" ? (
          <>
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
          <>
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
          </>
        )}
      </div>
    </>
  );
}
