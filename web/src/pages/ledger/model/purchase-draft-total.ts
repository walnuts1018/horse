type PurchaseDraftLine = { id: string; amountYen: number };

export function purchaseDraftTotalYen(
  purchase: { items: readonly PurchaseDraftLine[] },
  itemDrafts: Record<string, { amountYen: string } | undefined>,
) {
  return purchase.items.reduce(
    (total, item) => total + Number(itemDrafts[item.id]?.amountYen ?? item.amountYen),
    0,
  );
}
