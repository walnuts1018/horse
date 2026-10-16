import { useEffect, useRef, useState } from "react";

import type { Household, ProductConcept, Tag } from "../../../shared/api";
import type { useLedgerItemBulkEdit } from "../model/ledger-item-bulk-edit";

import { InlineError } from "../../../shared/ui";
import { LedgerProductVariantSelect } from "./ledger-product-variant-select";

import styles from "./ledger-item-bulk-actions.module.css";

type LedgerItemBulkEdit = ReturnType<typeof useLedgerItemBulkEdit>;
type BulkAction = "category" | "allocations" | "product" | "name" | "tags";

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
  const [activeAction, setActiveAction] = useState<BulkAction | null>(null);
  const [deleteConfirmationOpen, setDeleteConfirmationOpen] = useState(false);
  const deleteDialogRef = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dialog = deleteDialogRef.current;
    if (!dialog) return;
    if (deleteConfirmationOpen && !dialog.open) dialog.showModal();
    if (!deleteConfirmationOpen && dialog.open) dialog.close();
  }, [deleteConfirmationOpen]);

  return (
    <>
      {selectedCount > 0 && (
        <section
          className={`panel ledger-bulk-actions ${styles.bulkActions} ${styles.selectionPanel}`}
          aria-label="選択品目の一括操作"
        >
          <span className={styles.selectionCount}>{selectedCount}品目を選択中</span>
          {selectedCount > 100 && (
            <output className={styles.selectionLimit}>一括操作は100品目までです</output>
          )}
          <fieldset className={styles.actionList} disabled={pending || disabled}>
            <legend>変更する項目</legend>
            {(
              [
                ["category", "カテゴリ"],
                ["allocations", "負担"],
                ["product", "商品"],
                ["name", "品名"],
                ["tags", "タグ"],
              ] as const
            ).map(([action, label]) => (
              <button
                key={action}
                className={`button button-soft ${styles.actionButton} ${activeAction === action ? styles.activeAction : ""}`}
                type="button"
                aria-expanded={activeAction === action}
                aria-controls="ledger-item-bulk-action-settings"
                disabled={pending || disabled}
                onClick={() => setActiveAction(action)}
              >
                {label}
              </button>
            ))}
          </fieldset>
          {activeAction && (
            <div className={styles.settings} id="ledger-item-bulk-action-settings">
              {activeAction === "category" && (
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
              )}
              {activeAction === "allocations" && (
                <fieldset
                  className={`ledger-bulk-allocations ${styles.allocations}`}
                  disabled={pending || disabled}
                >
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
              )}
              {activeAction === "product" && (
                <div className={styles.productSetting}>
                  {edit.variantId === "__none" ? (
                    <div className={styles.productRemoval}>
                      <span>商品を解除</span>
                      <button
                        className="text-button"
                        type="button"
                        disabled={pending || disabled}
                        onClick={() => edit.setVariantId("")}
                      >
                        取り消す
                      </button>
                    </div>
                  ) : (
                    <LedgerProductVariantSelect
                      householdId={household.id}
                      itemName=""
                      merchant=""
                      value={edit.variantId}
                      catalog={catalog}
                      onChange={(variantId) => edit.setVariantId(variantId)}
                    />
                  )}
                  {edit.variantId !== "__none" && (
                    <button
                      className="button button-soft"
                      type="button"
                      disabled={pending || disabled}
                      onClick={() => edit.setVariantId("__none")}
                    >
                      商品を解除
                    </button>
                  )}
                </div>
              )}
              {activeAction === "name" && (
                <label>
                  <span>選択した品目の品名</span>
                  <input
                    type="text"
                    maxLength={240}
                    value={edit.name}
                    disabled={pending || disabled}
                    onChange={(event) => edit.setName(event.target.value)}
                  />
                </label>
              )}
              {activeAction === "tags" && (
                <fieldset className={styles.tagEditor} disabled={pending || disabled}>
                  <legend>タグを置換</legend>
                  <label className={styles.tagToggle}>
                    <input
                      type="checkbox"
                      checked={edit.tagsActive}
                      onChange={(event) => edit.setTagsActive(event.target.checked)}
                    />
                    <span>選択したタグで置換</span>
                  </label>
                  <div className={styles.selectedTags} aria-live="polite">
                    {edit.tags.length === 0 ? (
                      <span className={styles.emptyTags}>タグなしに置換</span>
                    ) : (
                      availableTags
                        .filter((tag) => edit.tags.includes(tag.id))
                        .map((tag) => (
                          <span className={styles.tagChip} key={tag.id}>
                            {tag.name}
                          </span>
                        ))
                    )}
                  </div>
                  <div className={styles.tagChoices}>
                    {availableTags
                      .filter((tag) => !tag.archived)
                      .map((tag) => (
                        <label className={styles.tagChoice} key={tag.id}>
                          <input
                            type="checkbox"
                            checked={edit.tags.includes(tag.id)}
                            disabled={!edit.tagsActive}
                            onChange={(event) =>
                              edit.setTags((current) =>
                                event.target.checked
                                  ? [...current, tag.id]
                                  : current.filter((id) => id !== tag.id),
                              )
                            }
                          />
                          <span>{tag.name}</span>
                        </label>
                      ))}
                    {availableTags.every((tag) => tag.archived) && (
                      <span className={styles.emptyTags}>利用できるタグがありません</span>
                    )}
                  </div>
                </fieldset>
              )}
            </div>
          )}
          <button
            className={`button button-primary ${styles.applyButton}`}
            type="button"
            disabled={
              pending ||
              disabled ||
              selectedCount > 100 ||
              (!edit.category &&
                !edit.allocationsActive &&
                !edit.variantId &&
                !edit.name.trim() &&
                !edit.tagsActive) ||
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
            className={`button ${styles.deleteButton}`}
            type="button"
            disabled={pending || disabled || selectedCount === 0 || selectedCount > 100}
            onClick={() => setDeleteConfirmationOpen(true)}
          >
            {pending ? "処理中" : `${selectedCount}品目を削除`}
          </button>
          <button
            className={`text-button ${styles.clearButton}`}
            type="button"
            disabled={pending}
            onClick={edit.clearSelection}
          >
            選択解除
          </button>
          <dialog
            ref={deleteDialogRef}
            className={`panel ${styles.deleteDialog}`}
            aria-labelledby="ledger-item-delete-title"
            onClose={() => setDeleteConfirmationOpen(false)}
          >
            <h2 id="ledger-item-delete-title">品目の削除</h2>
            <p>選択した{selectedCount}品目を削除します。この操作は取り消せません。</p>
            <div className={styles.deleteDialogActions}>
              <button
                className="button button-soft"
                type="button"
                onClick={() => setDeleteConfirmationOpen(false)}
              >
                キャンセル
              </button>
              <button
                className={`button ${styles.deleteButton}`}
                type="button"
                disabled={pending || disabled || selectedCount === 0 || selectedCount > 100}
                onClick={() => {
                  setDeleteConfirmationOpen(false);
                  void edit.deleteSelected();
                }}
              >
                削除する
              </button>
            </div>
          </dialog>
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
