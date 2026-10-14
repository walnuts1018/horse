import { ReceiptJapaneseYen, X } from "lucide-react";
import { useRef, type KeyboardEvent, type RefObject } from "react";

import type { Household } from "../../../shared/api";
import type { PurchaseDraftItem } from "../../../shared/lib";

import { EditPurchaseItemAllocations } from "./edit-purchase-item-allocations";

import styles from "./edit-purchase-items.module.css";

export function PurchaseItemsGrid({
  items,
  household,
  defaultAllocations,
  enabledCategories,
  classNames,
  selectedKeys,
  mobileSelectionMode,
  gridRef,
  onKeyDown,
  onSelectionChange,
  onPatch,
  onRemove,
}: {
  items: PurchaseDraftItem[];
  household: Household;
  defaultAllocations: PurchaseDraftItem["allocations"];
  enabledCategories: Household["categories"];
  classNames: { itemTable: string; itemList: string };
  selectedKeys: ReadonlySet<string>;
  mobileSelectionMode: boolean;
  gridRef: RefObject<HTMLDivElement | null>;
  onKeyDown: (
    event: KeyboardEvent<HTMLInputElement>,
    row: number,
    column: "name" | "amount",
  ) => void;
  onSelectionChange: (key: string, shiftKey: boolean) => void;
  onPatch: (key: string, patch: Partial<PurchaseDraftItem>) => void;
  onRemove: (key: string) => void;
}) {
  const selectionShiftKey = useRef(false);

  return (
    <>
      <div className={`item-table-head ${classNames.itemTable}`}>
        <span>品名</span>
        <span>金額</span>
        <span>カテゴリ</span>
        <span>負担</span>
        <span />
      </div>
      <div
        className={`item-list ${classNames.itemList} ${styles.itemList}${mobileSelectionMode ? ` ${styles.mobileSelectionMode}` : ""}`}
        ref={gridRef}
      >
        {items.map((item, index) => (
          <div className={`item-entry ${styles.itemEntry}`} key={item.key}>
            <div className="item-edit-row">
              <span className={`item-index ${styles.itemIndex}`}>
                <input
                  className={styles.selectionCheckbox}
                  type="checkbox"
                  aria-label={`${item.name || "品目"}を選択`}
                  checked={selectedKeys.has(item.key)}
                  onClick={(event) => {
                    selectionShiftKey.current = event.shiftKey;
                  }}
                  onChange={() => {
                    onSelectionChange(item.key, selectionShiftKey.current);
                    selectionShiftKey.current = false;
                  }}
                />
                <span>{String(index + 1).padStart(2, "0")}</span>
              </span>
              <input
                className={styles.selectionName}
                aria-label="品名"
                data-grid-row={index}
                data-grid-column="name"
                value={item.name}
                onChange={(event) => onPatch(item.key, { name: event.target.value })}
                onKeyDown={(event) => onKeyDown(event, index, "name")}
              />
              <div className="yen-input">
                <span>¥</span>
                <input
                  aria-label="金額"
                  data-grid-row={index}
                  data-grid-column="amount"
                  inputMode="numeric"
                  value={item.amount}
                  onChange={(event) =>
                    onPatch(item.key, { amount: event.target.value.replace(/\D/g, "") })
                  }
                  onKeyDown={(event) => onKeyDown(event, index, "amount")}
                />
              </div>
              <select
                className={styles.selectionCategory}
                aria-label="カテゴリ"
                value={item.categoryId}
                onChange={(event) => onPatch(item.key, { categoryId: event.target.value })}
              >
                <option value="">未分類</option>
                {enabledCategories.map((category) => (
                  <option key={category.id} value={category.id}>
                    {category.name}
                  </option>
                ))}
              </select>
              <EditPurchaseItemAllocations
                allocations={item.allocations}
                defaultAllocations={defaultAllocations}
                pools={household.pools}
                onChange={(allocations) => onPatch(item.key, { allocations })}
              />
              <button
                type="button"
                className="row-remove"
                aria-label="品目を削除"
                onClick={() => onRemove(item.key)}
              >
                <X size={15} />
              </button>
            </div>
            <details className="item-measure-details">
              <summary>数量</summary>
              <label>
                数量
                <input
                  aria-label={`${item.name}の数量`}
                  value={item.quantity ?? ""}
                  onChange={(event) => onPatch(item.key, { quantity: event.target.value })}
                  placeholder="1、1/2"
                />
              </label>
              <label>
                単位
                <input
                  aria-label={`${item.name}の数量単位`}
                  value={item.quantityUnit ?? ""}
                  onChange={(event) => onPatch(item.key, { quantityUnit: event.target.value })}
                  placeholder="個、g、ml"
                  maxLength={32}
                />
              </label>
            </details>
          </div>
        ))}
        {items.length === 0 && (
          <div className="empty-items">
            <ReceiptJapaneseYen size={24} />
            <span>品名と金額を入力して追加</span>
          </div>
        )}
      </div>
    </>
  );
}
