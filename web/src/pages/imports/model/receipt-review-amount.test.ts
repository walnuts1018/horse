import { describe, expect, it } from "vitest";

import { parseReceiptReviewAmountDraft } from "./receipt-review-amount";

describe("parseReceiptReviewAmountDraft", () => {
  it("空欄を0として確定し、数字と桁区切りを整数へ変換する", () => {
    expect(parseReceiptReviewAmountDraft("")).toBe(0);
    expect(parseReceiptReviewAmountDraft("12,300")).toBe(12300);
  });

  it("数値以外と安全な整数の範囲を超える値を拒否する", () => {
    expect(parseReceiptReviewAmountDraft("12円")).toBeNull();
    expect(parseReceiptReviewAmountDraft("9007199254740992")).toBeNull();
  });
});
