export function unmatchedObservationPageSize(first = 50) {
  if (!Number.isFinite(first)) return 50;
  return Math.min(100, Math.max(1, Math.trunc(first)));
}
