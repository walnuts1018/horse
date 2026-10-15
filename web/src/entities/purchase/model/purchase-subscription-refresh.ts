import type { PurchaseChangesSubscription } from "..";

export type PurchaseChange = PurchaseChangesSubscription["response"]["purchaseChanges"];

export type PurchaseChangeFilters = {
  from?: string;
  to?: string;
  search?: string;
  categoryId?: string;
  uncategorized?: boolean;
  tagId?: string;
  paymentAccountId?: string;
  poolId?: string;
  minimumAmount?: number;
  maximumAmount?: number;
};

export function purchaseChangeAffectsMembership(
  change: PurchaseChange,
  filters: PurchaseChangeFilters,
) {
  const fields = change.changedFields as readonly string[];
  const hasField = (field: string) => fields.includes(field);
  const hasSearch = Boolean(filters.search?.trim());

  if (change.kind === "CREATED") return true;
  if (change.kind === "UPDATED") {
    return (
      hasField("OCCURRED_AT") ||
      (hasSearch && hasField("MERCHANT")) ||
      (Boolean(filters.paymentAccountId) && hasField("PAYMENT_ALLOCATIONS"))
    );
  }
  if (change.kind !== "ITEM_UPDATED" && change.kind !== "ITEM_DELETED") return false;

  return (
    (hasSearch && hasField("NAME")) ||
    ((filters.minimumAmount !== undefined || filters.maximumAmount !== undefined) &&
      (hasField("AMOUNT_YEN") || hasField("DELETED"))) ||
    ((Boolean(filters.categoryId) || filters.uncategorized) &&
      (hasField("CATEGORY_ID") || hasField("DELETED"))) ||
    (Boolean(filters.tagId) && (hasField("TAGS") || hasField("DELETED"))) ||
    (Boolean(filters.poolId) && (hasField("ALLOCATIONS") || hasField("DELETED"))) ||
    (change.kind === "ITEM_DELETED" &&
      Boolean(
        filters.search?.trim() ||
        filters.categoryId ||
        filters.uncategorized ||
        filters.tagId ||
        filters.poolId ||
        filters.minimumAmount !== undefined ||
        filters.maximumAmount !== undefined,
      ))
  );
}
