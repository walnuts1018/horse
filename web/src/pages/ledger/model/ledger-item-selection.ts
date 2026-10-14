export function selectableLedgerItemIds(
  purchases: Array<{ status: string; items: Array<{ id: string }> }>,
): Set<string> {
  return new Set(
    purchases.flatMap((purchase) =>
      purchase.status === "CLOSED" ? [] : purchase.items.map((item) => item.id),
    ),
  );
}
