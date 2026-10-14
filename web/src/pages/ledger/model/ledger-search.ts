import type { PurchaseAmountTarget, PurchasesQuery } from "../../../entities/purchase";
import type { Purchase, SourceKind, UnmatchedObservationState } from "../../../shared/api";

export type LedgerSearch = {
  purchaseId: string;
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

export type LedgerMonthPreset = "thisMonth" | "lastMonth" | "lastThreeMonths";

function formatLedgerMonth(monthIndex: number): string {
  const year = Math.floor(monthIndex / 12);
  const month = (monthIndex % 12) + 1;
  return `${String(year).padStart(4, "0")}-${String(month).padStart(2, "0")}`;
}

export function getLedgerMonthPresetRange(
  preset: LedgerMonthPreset,
  now = new Date(),
): Pick<LedgerSearch, "fromMonth" | "toMonth"> {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
  }).formatToParts(now);
  const year = Number(parts.find((part) => part.type === "year")?.value);
  const month = Number(parts.find((part) => part.type === "month")?.value);
  const currentMonth = year * 12 + month - 1;
  const offset = preset === "lastMonth" ? 1 : preset === "lastThreeMonths" ? 2 : 0;
  const fromMonthIndex = currentMonth - offset;
  const toMonthIndex = preset === "lastMonth" ? fromMonthIndex : currentMonth;

  return {
    fromMonth: formatLedgerMonth(fromMonthIndex),
    toMonth: formatLedgerMonth(toMonthIndex),
  };
}

export const defaultLedgerSearch: LedgerSearch = {
  purchaseId: "",
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
): Pick<PurchasesQuery["variables"]["filter"], "minimumAmount" | "maximumAmount" | "amountTarget"> {
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
    purchaseId: stringValue(search.purchaseId, 128),
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
  if (filters.purchaseId !== defaultLedgerSearch.purchaseId) result.purchaseId = filters.purchaseId;
  if (filters.recordMode !== defaultLedgerSearch.recordMode) result.recordMode = filters.recordMode;
  if (filters.viewMode !== defaultLedgerSearch.viewMode) result.viewMode = filters.viewMode;
  if (filters.search !== defaultLedgerSearch.search) result.search = filters.search;
  if (filters.fromMonth !== defaultLedgerSearch.fromMonth) result.fromMonth = filters.fromMonth;
  if (filters.toMonth !== defaultLedgerSearch.toMonth) result.toMonth = filters.toMonth;
  if (filters.categoryFilter !== defaultLedgerSearch.categoryFilter)
    result.categoryFilter = filters.categoryFilter;
  if (filters.poolFilter !== defaultLedgerSearch.poolFilter) result.poolFilter = filters.poolFilter;
  if (filters.accountFilter !== defaultLedgerSearch.accountFilter)
    result.accountFilter = filters.accountFilter;
  if (filters.tagFilter !== defaultLedgerSearch.tagFilter) result.tagFilter = filters.tagFilter;
  if (filters.originFilter !== defaultLedgerSearch.originFilter)
    result.originFilter = filters.originFilter;
  if (filters.evidenceFilter !== defaultLedgerSearch.evidenceFilter)
    result.evidenceFilter = filters.evidenceFilter;
  if (filters.observationSourceFilter !== defaultLedgerSearch.observationSourceFilter)
    result.observationSourceFilter = filters.observationSourceFilter;
  if (filters.minimumAmount !== defaultLedgerSearch.minimumAmount)
    result.minimumAmount = filters.minimumAmount;
  if (filters.maximumAmount !== defaultLedgerSearch.maximumAmount)
    result.maximumAmount = filters.maximumAmount;
  if (filters.observationStateFilter !== defaultLedgerSearch.observationStateFilter)
    result.observationStateFilter = filters.observationStateFilter || "ALL";
  return result;
}
