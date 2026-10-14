export type ObservationMatch = {
  documentId: string;
  sourceReference: string;
  state: string;
  reasons: string[];
};

export async function readObservationMatches(documentId: string, sourceReference: string) {
  const query = new URLSearchParams({ sourceReference });
  const response = await fetch(
    `/documents/${encodeURIComponent(documentId)}/matches?${query.toString()}`,
    { credentials: "include" },
  );
  if (!response.ok) throw new Error(`照合候補を取得できませんでした (${response.status})`);
  const payload: unknown = await response.json();
  if (
    payload === null ||
    typeof payload !== "object" ||
    !("observations" in payload) ||
    !Array.isArray(payload.observations)
  )
    throw new Error("照合候補の形式が正しくありません");

  const observation = payload.observations.find(
    (entry: unknown) =>
      entry !== null &&
      typeof entry === "object" &&
      "sourceReference" in entry &&
      entry.sourceReference === sourceReference,
  );
  if (observation === undefined) return [];
  if (
    observation === null ||
    typeof observation !== "object" ||
    !("matches" in observation) ||
    !Array.isArray(observation.matches) ||
    !observation.matches.every(isObservationMatch)
  )
    throw new Error("照合候補の形式が正しくありません");
  return observation.matches;
}

function isObservationMatch(value: unknown): value is ObservationMatch {
  return (
    value !== null &&
    typeof value === "object" &&
    "documentId" in value &&
    typeof value.documentId === "string" &&
    "sourceReference" in value &&
    typeof value.sourceReference === "string" &&
    "state" in value &&
    typeof value.state === "string" &&
    "reasons" in value &&
    Array.isArray(value.reasons) &&
    value.reasons.every((reason: unknown) => typeof reason === "string")
  );
}
