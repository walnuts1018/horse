import { Plus } from "lucide-react";
import { useState, type SubmitEvent } from "react";

import type { Household } from "../../../shared/api";

import "./settings-page.css";
import styles from "./settings-page.module.css";

export function ManageCategoriesSection({
  categories,
  categoryName,
  disabled,
  hidden,
  onCategoryNameChange,
  onAdd,
  onArchive,
  onRename,
  onFormDirty,
  onFormClean,
}: {
  categories: Household["categories"];
  categoryName: string;
  disabled: boolean;
  hidden: boolean;
  onCategoryNameChange: (value: string) => void;
  onAdd: (event: SubmitEvent<HTMLFormElement>) => void;
  onArchive: (categoryId: string, archived: boolean) => void;
  onRename: (categoryId: string, name: string) => Promise<boolean>;
  onFormDirty: (formID: string) => void;
  onFormClean: (formID: string) => void;
}) {
  const [categoryNames, setCategoryNames] = useState<Record<string, string>>({});
  const [pendingCategoryId, setPendingCategoryId] = useState<string | null>(null);
  return (
    <section className={`panel ${styles.settingsSection}`} hidden={hidden}>
      <div className={`panel-heading ${styles.panelHeading}`}>
        <div>
          <span className="eyebrow">品目の分類</span>
          <h2>カテゴリ</h2>
        </div>
        <span className="count-badge">
          {categories.filter((category) => !category.archived).length}
        </span>
      </div>
      <div className="category-management">
        {categories.map((category, index) => (
          <div className="category-management-row" key={category.id}>
            <span className={`category-tag swatch-bg-${index % 6}`} aria-hidden="true" />
            <input
              aria-label={`${category.name}のカテゴリ名`}
              value={categoryNames[category.id] ?? category.name}
              maxLength={120}
              disabled={disabled}
              onChange={(event) => {
                const name = event.target.value;
                setCategoryNames((current) => ({ ...current, [category.id]: name }));
                if (name === category.name) onFormClean(`category:${category.id}`);
                else onFormDirty(`category:${category.id}`);
              }}
            />
            <button
              type="button"
              className="text-button"
              disabled={
                disabled ||
                !(categoryNames[category.id] ?? category.name).trim() ||
                (categoryNames[category.id] ?? category.name).trim() === category.name
              }
              onClick={() => {
                const name = (categoryNames[category.id] ?? category.name).trim();
                setPendingCategoryId(category.id);
                void onRename(category.id, name).then((saved) => {
                  if (saved) {
                    setCategoryNames((current) => ({ ...current, [category.id]: name }));
                    onFormClean(`category:${category.id}`);
                  }
                  setPendingCategoryId(null);
                });
              }}
            >
              保存
            </button>
            {(categoryNames[category.id] ?? category.name) !== category.name && (
              <button
                type="button"
                className="text-button"
                onClick={() => {
                  setCategoryNames((current) => ({ ...current, [category.id]: category.name }));
                  onFormClean(`category:${category.id}`);
                }}
              >
                取消
              </button>
            )}
            <button
              type="button"
              className="text-button"
              disabled={disabled || pendingCategoryId === category.id}
              onClick={() => onArchive(category.id, !category.archived)}
            >
              {category.archived ? "再表示" : "非表示"}
            </button>
          </div>
        ))}
      </div>
      <form className="inline-create" data-unsaved-id="create-category" onSubmit={onAdd}>
        <input
          value={categoryName}
          onChange={(event) => onCategoryNameChange(event.target.value)}
          placeholder="カテゴリ名"
          required
        />
        <button type="submit" className="button button-soft" disabled={disabled}>
          <Plus size={15} />
          追加
        </button>
      </form>
    </section>
  );
}
