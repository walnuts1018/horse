export type WeightedAllocation = { poolId: string; weight: number };

function greatestCommonDivisor(left: number, right: number): number {
  let a = Math.abs(left);
  let b = Math.abs(right);
  while (b !== 0) [a, b] = [b, a % b];
  return a;
}

function normalizedAllocations(allocations: readonly WeightedAllocation[]): Map<string, number> {
  const weights = new Map<string, number>();
  for (const { poolId, weight } of allocations) {
    if (!Number.isSafeInteger(weight) || weight <= 0) continue;
    weights.set(poolId, (weights.get(poolId) ?? 0) + weight);
  }

  let divisor = 0;
  for (const weight of weights.values()) divisor = greatestCommonDivisor(divisor, weight);
  return new Map(
    [...weights].map(([poolId, weight]) => [poolId, divisor === 0 ? weight : weight / divisor]),
  );
}

export function areAllocationsEquivalent(
  left: readonly WeightedAllocation[],
  right: readonly WeightedAllocation[],
): boolean {
  const normalizedLeft = normalizedAllocations(left);
  const normalizedRight = normalizedAllocations(right);
  return (
    normalizedLeft.size === normalizedRight.size &&
    [...normalizedLeft].every(([poolId, weight]) => normalizedRight.get(poolId) === weight)
  );
}
