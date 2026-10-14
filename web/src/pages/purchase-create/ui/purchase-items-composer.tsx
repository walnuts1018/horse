import type { ReactNode, Ref } from "react";

import type { Household, Purchase } from "../../../shared/api";
import type { PurchaseDraftItem } from "../../../shared/lib";
import type { PurchaseItemsValue } from "../model/purchase-items";

import { EditPurchaseItems, type EditPurchaseItemsCommands } from "./edit-purchase-items";

export function PurchaseItemsComposer({
  householdId,
  merchant,
  copiedPurchase,
  household,
  history,
  value,
  commandsRef,
  itemTotal,
  hasDraftContent,
  hasUnsavedChanges,
  savedAt,
  classNames,
  onChange,
  onDirty,
  onHistoryAvailabilityChange,
  children,
}: {
  householdId: string;
  merchant: string;
  copiedPurchase: Purchase | null;
  household: Household;
  history: Purchase[];
  value: PurchaseItemsValue;
  commandsRef: Ref<EditPurchaseItemsCommands>;
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
    entryInputTarget: string;
    bottom: string;
  };
  onChange: (value: PurchaseItemsValue) => void;
  onDirty: () => void;
  onHistoryAvailabilityChange: (availability: { undo: boolean; redo: boolean }) => void;
  children: ReactNode;
}) {
  const items: PurchaseDraftItem[] = value.items;

  return (
    <>
      <section className={`panel item-entry-panel ${classNames.items}`}>
        <EditPurchaseItems
          householdId={householdId}
          merchant={merchant}
          copiedPurchase={copiedPurchase}
          household={household}
          history={history}
          value={value}
          commandsRef={commandsRef}
          classNames={{
            itemHeading: classNames.itemHeading,
            itemTable: classNames.itemTable,
            itemList: classNames.itemList,
            entryForm: classNames.entryForm,
            entryHint: classNames.entryHint,
            entryInputTarget: classNames.entryInputTarget,
          }}
          onChange={onChange}
          onDirty={onDirty}
          onHistoryAvailabilityChange={onHistoryAvailabilityChange}
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
