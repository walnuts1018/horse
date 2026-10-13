import * as fc from "fast-check";
import { describe, expect, it } from "vitest";

import { defaultLedgerSearch, parseLedgerSearch, serializeLedgerSearch } from "./ledger-search";

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
});
