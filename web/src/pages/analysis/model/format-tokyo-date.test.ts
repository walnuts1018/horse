import { describe, expect, it } from "vitest";

import { formatTokyoDate } from "./format-tokyo-date";

describe("formatTokyoDate", () => {
  it("formats timestamps using the Japan calendar date", () => {
    expect(formatTokyoDate("2026-10-31T15:00:00Z")).toBe("2026-11-01");
  });

  it("labels missing or invalid timestamps as undated", () => {
    expect(formatTokyoDate(null)).toBe("日付不明");
    expect(formatTokyoDate("invalid")).toBe("日付不明");
  });
});
