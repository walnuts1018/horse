import type { ReactNode } from "react";
import type { Household, Purchase } from "../../../shared/api";
import type { PurchaseDraftItem } from "../../../shared/lib";
import type { PurchaseItemsValue } from "../model/purchase-items";
import { EditPurchaseItems } from "./edit-purchase-items";

export function PurchaseItemsComposer({
  householdId,
  merchant,
  household,
  history,
  value,
  resetToken,
  copiedPurchase,
  undoToken,
  itemTotal,
  hasDraftContent,
  hasUnsavedChanges,
  savedAt,
  classNames,
  onChange,
  onDirty,
  onUndoAvailabilityChange,
  children,
}: {
  householdId: string;
  merchant: string;
  household: Household;
  history: Purchase[];
  value: PurchaseItemsValue;
  resetToken: number;
  copiedPurchase: Purchase | null;
  undoToken: number;
  itemTotal: number;
  hasDraftContent: boolean;
  hasUnsavedChanges: boolean;
  savedAt: Date | null;
  classNames: {
    items: string;
    itemHeading: string;
    itemTable: string;
    itemList: string;
    entryForm: string;
    entryHint: string;
    bottom: string;
  };
  onChange: (value: PurchaseItemsValue) => void;
  onDirty: () => void;
  onUndoAvailabilityChange: (available: boolean) => void;
  children: ReactNode;
}) {
  const items: PurchaseDraftItem[] = value.items;

  return (
    <>
      <section className={`panel item-entry-panel ${classNames.items}`}>
        <EditPurchaseItems
          householdId={householdId}
          merchant={merchant}
          household={household}
          history={history}
          value={value}
          resetToken={resetToken}
          copiedPurchase={copiedPurchase}
          undoToken={undoToken}
          classNames={{
            itemHeading: classNames.itemHeading,
            itemTable: classNames.itemTable,
            itemList: classNames.itemList,
            entryForm: classNames.entryForm,
            entryHint: classNames.entryHint,
          }}
          onChange={onChange}
          onDirty={onDirty}
          onUndoAvailabilityChange={onUndoAvailabilityChange}
        />
      </section>
      <div className={`composer-bottom ${classNames.bottom}`}>
        <div>
          <div className="total-label">
            合計 <strong>{yen(itemTotal)}</strong>
          </div>
          <span>
            {items.length}品目
            {hasDraftContent && (
              <>
                {" · "}
                {hasUnsavedChanges
                  ? "未保存"
                  : savedAt
                    ? "下書き保存 " +
                      savedAt.toLocaleTimeString("ja-JP", { hour: "2-digit", minute: "2-digit" })
                    : "未保存"}
              </>
            )}
          </span>
        </div>
        <div className="composer-actions">{children}</div>
      </div>
    </>
  );
}

const yen = (amount: number) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
