export function parseReceiptReviewAmountDraft(value: string): number | null {
  if (value === "") return 0;
  if (!/^[\d,]+$/.test(value)) return null;
  const amount = Number(value.replace(/,/g, ""));
  return Number.isSafeInteger(amount) ? amount : null;
}
