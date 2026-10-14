import * as fc from "fast-check";
import { describe, expect, it } from "vitest";

import {
  defaultLedgerSearch,
  getLedgerMonthPresetRange,
  parseLedgerSearch,
  serializeLedgerSearch,
} from "./ledger-search";

const arbitraryUnknown = fc.oneof(
  fc.string({ maxLength: 240 }),
  fc.integer(),
  fc.boolean(),
  fc.constant(null),
  fc.constant(undefined),
);

const arbitraryMonth = fc.oneof(
  fc
    .tuple(fc.integer({ min: 0, max: 9999 }), fc.integer({ min: 1, max: 12 }))
    .map(([year, month]) => `${String(year).padStart(4, "0")}-${String(month).padStart(2, "0")}`),
  arbitraryUnknown,
);

const arbitraryAmount = fc.oneof(
  fc.integer({ min: 0, max: 999_999_999_999 }).map(String),
  arbitraryUnknown,
);

const arbitrarySearch = fc.record({
  purchaseId: arbitraryUnknown,
  recordMode: fc.oneof(fc.constantFrom("purchases", "observations"), arbitraryUnknown),
  viewMode: fc.oneof(fc.constantFrom("purchases", "items"), arbitraryUnknown),
  search: arbitraryUnknown,
  fromMonth: arbitraryMonth,
  toMonth: arbitraryMonth,
  categoryFilter: arbitraryUnknown,
  poolFilter: arbitraryUnknown,
  accountFilter: arbitraryUnknown,
  tagFilter: arbitraryUnknown,
  originFilter: fc.oneof(fc.constantFrom("AI", "MANUAL", "UNKNOWN"), arbitraryUnknown),
  evidenceFilter: fc.oneof(fc.constantFrom("true", "false"), arbitraryUnknown),
  observationSourceFilter: fc.oneof(
    fc.constantFrom("RECEIPT", "ORDER", "EMAIL", "CARD", "BANK"),
    arbitraryUnknown,
  ),
  observationStateFilter: fc.oneof(
    fc.constantFrom("CONFIRMED", "UNMATCHED", "ALL"),
    arbitraryUnknown,
  ),
  minimumAmount: arbitraryAmount,
  maximumAmount: arbitraryAmount,
});

describe("ledger search", () => {
  it("東京の現在月を基準に月範囲プリセットを計算する", () => {
    const now = new Date("2025-12-31T15:00:00.000Z");

    expect(getLedgerMonthPresetRange("thisMonth", now)).toEqual({
      fromMonth: "2026-01",
      toMonth: "2026-01",
    });
    expect(getLedgerMonthPresetRange("lastMonth", now)).toEqual({
      fromMonth: "2025-12",
      toMonth: "2025-12",
    });
    expect(getLedgerMonthPresetRange("lastThreeMonths", now)).toEqual({
      fromMonth: "2025-11",
      toMonth: "2026-01",
    });
  });

  it("parseとserializeの往復で検索条件を保持する", () => {
    fc.assert(
      fc.property(arbitrarySearch, (search) => {
        const parsed = parseLedgerSearch(search);
        expect(parseLedgerSearch(serializeLedgerSearch(parsed))).toEqual(parsed);
      }),
      { numRuns: 500 },
    );
  });

  it("無効な月と逆順の期間を除外する", () => {
    expect(parseLedgerSearch({ fromMonth: "2025-13", toMonth: "2025-01" })).toMatchObject({
      fromMonth: "",
      toMonth: "2025-01",
    });

    expect(parseLedgerSearch({ fromMonth: "2025-11", toMonth: "2025-01" })).toMatchObject({
      fromMonth: "2025-11",
      toMonth: "",
    });
  });

  it("初期値を省略し、全状態をURLに表す", () => {
    expect(serializeLedgerSearch(defaultLedgerSearch)).toEqual({});
    expect(serializeLedgerSearch({ ...defaultLedgerSearch, observationStateFilter: "" })).toEqual({
      observationStateFilter: "ALL",
    });
    expect(parseLedgerSearch({ observationStateFilter: "ALL" }).observationStateFilter).toBe("");
  });

  it("帳簿で未対応のtabパラメーターを正規化する", () => {
    expect(parseLedgerSearch({ tab: "accounts" })).toEqual(defaultLedgerSearch);
    expect(serializeLedgerSearch(parseLedgerSearch({ tab: "accounts" }))).toEqual({});
  });

  it("対象購入のIDをURLに保持する", () => {
    expect(parseLedgerSearch({ purchaseId: "purchase-1" }).purchaseId).toBe("purchase-1");
    expect(serializeLedgerSearch({ ...defaultLedgerSearch, purchaseId: "purchase-1" })).toEqual({
      purchaseId: "purchase-1",
    });
  });
});
