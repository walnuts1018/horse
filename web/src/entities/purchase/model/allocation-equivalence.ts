export type WeightedAllocation = { poolId: string; weight: number };

function greatestCommonDivisor(left: number, right: number): number {
  let a = Math.abs(left);
  let b = Math.abs(right);
  while (b !== 0) [a, b] = [b, a % b];
  return a;
}

function normalizedAllocations(allocations: readonly WeightedAllocation[]): string {
  const weights = new Map<string, number>();
  for (const { poolId, weight } of allocations) {
    if (!Number.isSafeInteger(weight) || weight <= 0) continue;
    weights.set(poolId, (weights.get(poolId) ?? 0) + weight);
  }

  let divisor = 0;
  for (const weight of weights.values()) divisor = greatestCommonDivisor(divisor, weight);
  return [...weights]
    .map(([poolId, weight]) => [poolId, divisor === 0 ? weight : weight / divisor] as const)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([poolId, weight]) => `${poolId}\u0000${weight}`)
    .join("\u0001");
}

export function areAllocationsEquivalent(
  left: readonly WeightedAllocation[],
  right: readonly WeightedAllocation[],
): boolean {
  return normalizedAllocations(left) === normalizedAllocations(right);
}
