import type { Household, ProductConcept, Tag } from "../../../shared/api";
import type { useLedgerItemBulkEdit } from "../model/ledger-item-bulk-edit";

import { InlineError } from "../../../shared/ui";

import styles from "./ledger-search.module.css";

type LedgerItemBulkEdit = ReturnType<typeof useLedgerItemBulkEdit>;

export function LedgerItemBulkActions({
  edit,
  household,
  catalog,
  availableTags,
  disabled,
}: {
  edit: LedgerItemBulkEdit;
  household: Household;
  catalog: ProductConcept[];
  availableTags: Tag[];
  disabled: boolean;
}) {
  const { selectedCount, pending } = edit;
  return (
    <>
      {selectedCount > 0 && (
        <section
          className={`panel ledger-bulk-actions ${styles.bulkActions}`}
          aria-label="選択品目の一括編集"
        >
          <span>{selectedCount}品目を選択中</span>
          <label className="select-filter">
            <span>カテゴリ</span>
            <select
              value={edit.category}
              disabled={pending || disabled}
              onChange={(event) => edit.setCategory(event.target.value)}
            >
              <option value="">変更なし</option>
              <option value="__uncategorized">未分類</option>
              {household.categories
                .filter((category) => !category.archived)
                .map((category) => (
                  <option key={category.id} value={category.id}>
                    {category.name}
                  </option>
                ))}
            </select>
          </label>
          <fieldset className="ledger-bulk-allocations" disabled={pending || disabled}>
            <legend>負担先を置換</legend>
            <label>
              <input
                type="checkbox"
                checked={edit.allocationsActive}
                onChange={(event) => edit.setAllocationsActive(event.target.checked)}
              />
              {edit.allocationsActive ? "置換" : "変更なし"}
            </label>
            {edit.allocationsActive &&
              household.pools.map((pool) => (
                <label key={pool.id}>
                  <input
                    type="checkbox"
                    checked={edit.allocations[pool.id] !== undefined}
                    onChange={(event) =>
                      edit.setAllocations((current) => {
                        const next = { ...current };
                        if (event.target.checked) next[pool.id] = "1";
                        else delete next[pool.id];
                        return next;
                      })
                    }
                  />
                  {pool.name}
                  {edit.allocations[pool.id] !== undefined && (
                    <input
                      aria-label={`${pool.name}の負担比率`}
                      inputMode="decimal"
                      type="number"
                      min="0.01"
                      step="0.01"
                      value={edit.allocations[pool.id]}
                      onChange={(event) =>
                        edit.setAllocations((current) => ({
                          ...current,
                          [pool.id]: event.target.value,
                        }))
                      }
                    />
                  )}
                </label>
              ))}
          </fieldset>
          <label className="select-filter">
            <span>商品</span>
            <select
              value={edit.variantId}
              disabled={pending || disabled}
              onChange={(event) => edit.setVariantId(event.target.value)}
            >
              <option value="">変更なし</option>
              {catalog.flatMap((concept) =>
                concept.variants
                  .filter((variant) => !variant.archived)
                  .map((variant) => (
                    <option key={variant.id} value={variant.id}>
                      {concept.name} · {variant.name}
                    </option>
                  )),
              )}
              <option value="__none">商品を解除</option>
            </select>
          </label>
          <label className="select-filter">
            <span>タグを置換</span>
            <input
              type="checkbox"
              checked={edit.tagsActive}
              disabled={pending || disabled}
              onChange={(event) => edit.setTagsActive(event.target.checked)}
            />
            <select
              aria-label="一括設定するタグ"
              multiple
              value={edit.tags}
              disabled={!edit.tagsActive || pending || disabled}
              onChange={(event) =>
                edit.setTags(
                  Array.from(event.currentTarget.selectedOptions, (option) => option.value),
                )
              }
            >
              {availableTags
                .filter((tag) => !tag.archived)
                .map((tag) => (
                  <option key={tag.id} value={tag.id}>
                    {tag.name}
                  </option>
                ))}
            </select>
          </label>
          <button
            className="button button-primary"
            type="button"
            disabled={
              pending ||
              disabled ||
              (!edit.category && !edit.allocationsActive && !edit.variantId && !edit.tagsActive) ||
              (edit.allocationsActive &&
                Object.values(edit.allocations).some(
                  (weight) => !Number.isFinite(Number(weight)) || Number(weight) <= 0,
                ))
            }
            onClick={() => void edit.apply()}
          >
            {pending ? "更新中" : "一括変更"}
          </button>
          <button
            className="text-button"
            type="button"
            disabled={pending}
            onClick={edit.clearSelection}
          >
            選択解除
          </button>
        </section>
      )}
      {edit.undo && (
        <section className={`panel ledger-bulk-actions ${styles.bulkActions}`} aria-live="polite">
          <span>{edit.undo.length}品目の変更</span>
          <button
            className="button button-soft"
            type="button"
            disabled={pending}
            onClick={() => void edit.undoChanges()}
          >
            元に戻す
          </button>
        </section>
      )}
      {edit.error && <InlineError message={edit.error} />}
    </>
  );
}
