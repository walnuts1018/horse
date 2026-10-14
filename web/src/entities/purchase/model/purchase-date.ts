import type { PurchaseDatePrecisionValue } from "../../../shared/api";

export function purchaseOccurredAtLabel(occurredAt: string, precision: PurchaseDatePrecisionValue) {
  const date = new Date(occurredAt);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat("ja-JP", {
    timeZone: "Asia/Tokyo",
    month: "numeric",
    day: "numeric",
    ...(precision === "DATE_TIME" ? { hour: "2-digit", minute: "2-digit" } : {}),
  }).format(date);
}

export function purchaseDateInputValue(occurredAt: string) {
  const date = new Date(occurredAt);
  if (Number.isNaN(date.getTime())) return "";
  const parts = Object.fromEntries(
    new Intl.DateTimeFormat("en", {
      timeZone: "Asia/Tokyo",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    })
      .formatToParts(date)
      .map((part) => [part.type, part.value]),
  );
  return `${parts.year}-${parts.month}-${parts.day}`;
}
