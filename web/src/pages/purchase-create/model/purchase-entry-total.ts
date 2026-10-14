export function pendingPurchaseEntryAmount(name: string, amount: string): number {
  if (!name.trim() || !/^[1-9]\d*$/.test(amount)) return 0;
  const parsed = Number(amount);
  return Number.isFinite(parsed) ? parsed : 0;
}
