export function parseReceiptReviewAmountDraft(value: string): number | null {
  if (!/^(?:\d+|\d{1,3}(?:,\d{3})+)$/.test(value)) return null;
  const amount = Number(value.replace(/,/g, ""));
  return Number.isSafeInteger(amount) ? amount : null;
}

export function receiptReviewAmountDraftOnBlur(
  value: string,
): { kind: "keep" } | { kind: "commit"; amount: number } {
  const amount = parseReceiptReviewAmountDraft(value);
  return amount === null ? { kind: "keep" } : { kind: "commit", amount };
}
