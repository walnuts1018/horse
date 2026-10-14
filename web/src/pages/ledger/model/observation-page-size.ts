export function unmatchedObservationPageSize(enabled: boolean | undefined, first = 50) {
  if (enabled === false) return 1;
  if (!Number.isFinite(first)) return 50;
  return Math.min(100, Math.max(1, Math.trunc(first)));
}
