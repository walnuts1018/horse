export type PurchaseAllocationRule = {
  target:
    | { kind: "category"; id: string }
    | { kind: "concept"; id: string }
    | { kind: "variant"; id: string }
    | { kind: "merchant"; key: string };
  allocations: Array<{ poolId: string; weight: number }>;
};

export function resolvePurchaseAllocationRule(
  rules: PurchaseAllocationRule[],
  conceptIdsByVariant: ReadonlyMap<string, string>,
  merchant: string,
  categoryId: string,
  variantId?: string | null,
): PurchaseAllocationRule | null {
  const targets: PurchaseAllocationRule["target"][] = [];
  if (variantId) {
    targets.push({ kind: "variant", id: variantId });
    const conceptId = conceptIdsByVariant.get(variantId);
    if (conceptId) targets.push({ kind: "concept", id: conceptId });
  }
  if (categoryId) targets.push({ kind: "category", id: categoryId });
  const merchantKey = normalizeMerchant(merchant);
  if (merchantKey) targets.push({ kind: "merchant", key: merchantKey });

  for (const target of targets) {
    const rule = rules.find((candidate) => sameTarget(candidate.target, target));
    if (rule) return rule;
  }
  return null;
}

export function resolvePurchaseAllocations(
  rules: PurchaseAllocationRule[],
  conceptIdsByVariant: ReadonlyMap<string, string>,
  merchant: string,
  categoryId: string,
  variantId?: string | null,
): PurchaseAllocationRule["allocations"] | null {
  return (
    resolvePurchaseAllocationRule(rules, conceptIdsByVariant, merchant, categoryId, variantId)
      ?.allocations ?? null
  );
}

function sameTarget(
  left: PurchaseAllocationRule["target"],
  right: PurchaseAllocationRule["target"],
) {
  if (left.kind !== right.kind) return false;
  return left.kind === "merchant" && right.kind === "merchant"
    ? left.key === right.key
    : left.kind !== "merchant" && right.kind !== "merchant" && left.id === right.id;
}

function normalizeMerchant(value: string) {
  return value.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean).join(" ");
}
