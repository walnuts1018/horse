import { describe, expect, it } from "vitest";

import { amountInputCaret, formatAmountInput } from "./amount-input";

describe("amount input formatting", () => {
  it("adds digit separators without changing the stored digits", () => {
    expect(formatAmountInput("1234567")).toBe("1,234,567");
    expect(formatAmountInput("12a345")).toBe("12,345");
    expect(formatAmountInput("")).toBe("");
  });

  it("preserves leading zeroes and formats integers beyond the safe number range", () => {
    expect(formatAmountInput("0001234")).toBe("0,001,234");
    expect(formatAmountInput("0000")).toBe("0,000");
    expect(formatAmountInput("123456789012345678901234567890")).toBe(
      "123,456,789,012,345,678,901,234,567,890",
    );
  });

  it("groups long zero-prefixed input without changing its digits", () => {
    const digits = `${"0".repeat(12000)}1`;
    const formatted = formatAmountInput(digits);

    expect(formatted.replace(/\D/g, "")).toBe(digits);
    expect(formatted).toContain(",");
  });

  it("groups long non-zero-prefixed input without changing its digits", () => {
    const digits = `1${"2".repeat(12000)}`;
    const formatted = formatAmountInput(digits);

    expect(formatted.replace(/\D/g, "")).toBe(digits);
    expect(formatted).toContain(",");
  });

  it("restores the caret after the same number of digits", () => {
    expect(amountInputCaret("12,345", 2)).toBe(3);
    expect(amountInputCaret("123,456", 4)).toBe(4);
    expect(amountInputCaret("12934", 2)).toBe(3);
    expect(amountInputCaret("123,456", 0)).toBe(0);
  });
});
