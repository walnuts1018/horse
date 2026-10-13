import type { KeyboardEvent, RefObject } from "react";
import { ReceiptJapaneseYen, X } from "lucide-react";
import type { Household } from "../../../shared/api";
import type { PurchaseDraftItem } from "../../../shared/lib";
import { EditPurchaseItemAllocations } from "./edit-purchase-item-allocations";

export function PurchaseItemsGrid({
  items,
  household,
  defaultAllocations,
  enabledCategories,
  classNames,
  gridRef,
  onKeyDown,
  onPatch,
  onRemove,
}: {
  items: PurchaseDraftItem[];
  household: Household;
  defaultAllocations: PurchaseDraftItem["allocations"];
  enabledCategories: Household["categories"];
  classNames: { itemTable: string; itemList: string };
  gridRef: RefObject<HTMLDivElement | null>;
  onKeyDown: (
    event: KeyboardEvent<HTMLInputElement>,
    row: number,
    column: "name" | "amount",
  ) => void;
  onPatch: (key: string, patch: Partial<PurchaseDraftItem>) => void;
  onRemove: (key: string) => void;
}) {
  return (
    <>
      <div className={`item-table-head ${classNames.itemTable}`}>
        <span>品名</span>
        <span>金額</span>
        <span>カテゴリ</span>
        <span>負担</span>
        <span />
      </div>
      <div className={`item-list ${classNames.itemList}`} ref={gridRef}>
        {items.map((item, index) => (
          <div className="item-entry" key={item.key}>
            <div className="item-edit-row">
              <span className="item-index">{String(index + 1).padStart(2, "0")}</span>
              <input
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
