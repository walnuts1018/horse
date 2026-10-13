import type { Purchase, SourceKind, UnmatchedObservationState } from "../../../shared/api";
import type { PurchaseAmountTarget, PurchasesQuery } from "../../../entities/purchase";

export type LedgerSearch = {
  recordMode: "purchases" | "observations";
  viewMode: "purchases" | "items";
  search: string;
  fromMonth: string;
  toMonth: string;
  categoryFilter: string;
  poolFilter: string;
  accountFilter: string;
  tagFilter: string;
  originFilter: Purchase["origin"] | "";
  evidenceFilter: "true" | "false" | "";
  observationSourceFilter: SourceKind | "";
  observationStateFilter: UnmatchedObservationState | "";
  minimumAmount: string;
  maximumAmount: string;
};

export const defaultLedgerSearch: LedgerSearch = {
  recordMode: "purchases",
  viewMode: "purchases",
  search: "",
  fromMonth: "",
  toMonth: "",
  categoryFilter: "",
  poolFilter: "",
  accountFilter: "",
  tagFilter: "",
  originFilter: "",
  evidenceFilter: "",
  observationSourceFilter: "",
  observationStateFilter: "UNMATCHED",
  minimumAmount: "",
  maximumAmount: "",
};

export function toPurchaseAmountVariables(
  filters: Pick<LedgerSearch, "minimumAmount" | "maximumAmount">,
  viewMode: LedgerSearch["viewMode"],
): Pick<PurchasesQuery["variables"], "minimumAmount" | "maximumAmount" | "amountTarget"> {
  return {
    minimumAmount: filters.minimumAmount ? Number(filters.minimumAmount) : undefined,
    maximumAmount: filters.maximumAmount ? Number(filters.maximumAmount) : undefined,
    amountTarget: (viewMode === "items" ? "ITEM" : "PURCHASE") satisfies PurchaseAmountTarget,
  };
}

function stringValue(value: unknown, maxLength = 200): string {
  return typeof value === "string" ? value.slice(0, maxLength) : "";
}

function monthValue(value: unknown): string {
  if (typeof value !== "string" || !/^\d{4}-(0[1-9]|1[0-2])$/.test(value)) return "";
  return value;
}

function amountValue(value: unknown): string {
  if (typeof value !== "string" || !/^\d{1,12}$/.test(value)) return "";
  return String(Number(value));
}

export function parseLedgerSearch(search: Record<string, unknown>): LedgerSearch {
  const fromMonth = monthValue(search.fromMonth);
  let toMonth = monthValue(search.toMonth);
  if (fromMonth && toMonth && fromMonth > toMonth) toMonth = "";

  const minimumAmount = amountValue(search.minimumAmount);
  let maximumAmount = amountValue(search.maximumAmount);
  if (minimumAmount && maximumAmount && Number(minimumAmount) > Number(maximumAmount))
    maximumAmount = "";

  return {
    recordMode: search.recordMode === "observations" ? "observations" : "purchases",
    viewMode: search.viewMode === "items" ? "items" : "purchases",
    search: stringValue(search.search),
    fromMonth,
    toMonth,
    categoryFilter: stringValue(search.categoryFilter, 128),
    poolFilter: stringValue(search.poolFilter, 128),
    accountFilter: stringValue(search.accountFilter, 128),
    tagFilter: stringValue(search.tagFilter, 128),
    originFilter:
      search.originFilter === "AI" ||
      search.originFilter === "MANUAL" ||
      search.originFilter === "UNKNOWN"
        ? search.originFilter
        : "",
    evidenceFilter:
      search.evidenceFilter === "true" || search.evidenceFilter === "false"
        ? search.evidenceFilter
        : "",
    observationSourceFilter:
      search.observationSourceFilter === "RECEIPT" ||
      search.observationSourceFilter === "ORDER" ||
      search.observationSourceFilter === "EMAIL" ||
      search.observationSourceFilter === "CARD" ||
      search.observationSourceFilter === "BANK"
        ? search.observationSourceFilter
        : "",
    observationStateFilter:
      search.observationStateFilter === "CONFIRMED" || search.observationStateFilter === "UNMATCHED"
        ? search.observationStateFilter
        : search.observationStateFilter === "ALL"
          ? ""
          : "UNMATCHED",
    minimumAmount,
    maximumAmount,
  };
}

export function serializeLedgerSearch(filters: LedgerSearch): Omit<
  Partial<LedgerSearch>,
  "observationStateFilter"
> & {
  observationStateFilter?: UnmatchedObservationState | "ALL";
} {
  const result: Omit<Partial<LedgerSearch>, "observationStateFilter"> & {
    observationStateFilter?: UnmatchedObservationState | "ALL";
  } = {};
  for (const key of Object.keys(defaultLedgerSearch) as Array<keyof LedgerSearch>) {
    if (key === "observationStateFilter") continue;
    if (filters[key] !== defaultLedgerSearch[key]) result[key] = filters[key] as never;
  }
  if (filters.observationStateFilter !== defaultLedgerSearch.observationStateFilter)
    result.observationStateFilter = filters.observationStateFilter || "ALL";
  return result;
}
