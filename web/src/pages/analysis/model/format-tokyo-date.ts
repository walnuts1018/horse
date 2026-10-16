const dateFormatter = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Asia/Tokyo",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

export function formatTokyoDate(value: string | null): string {
  if (!value) return "日付不明";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "日付不明" : dateFormatter.format(date);
}
