import type { SubmitEvent } from "react";

import { Plus } from "lucide-react";

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
}: {
  categories: Household["categories"];
  categoryName: string;
  disabled: boolean;
  hidden: boolean;
  onCategoryNameChange: (value: string) => void;
  onAdd: (event: SubmitEvent<HTMLFormElement>) => void;
  onArchive: (categoryId: string, archived: boolean) => void;
}) {
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
            <span
              className={`category-tag swatch-bg-${index % 6} ${category.archived ? "category-archived" : ""}`}
            >
              {category.name}
            </span>
            <button
              type="button"
              className="text-button"
              disabled={disabled}
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
