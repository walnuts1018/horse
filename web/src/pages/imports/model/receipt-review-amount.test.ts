import { describe, expect, it } from "vitest";

import {
  parseReceiptReviewAmountDraft,
  receiptReviewAmountDraftOnBlur,
} from "./receipt-review-amount";

describe("parseReceiptReviewAmountDraft", () => {
  it("空欄と入力途中の桁区切りを未確定として扱う", () => {
    expect(parseReceiptReviewAmountDraft("")).toBeNull();
    expect(parseReceiptReviewAmountDraft("1,")).toBeNull();
    expect(parseReceiptReviewAmountDraft(",")).toBeNull();
  });

  it("blur時に未確定値を保持し、有効な金額だけを確定する", () => {
    expect(receiptReviewAmountDraftOnBlur("")).toEqual({ kind: "keep" });
    expect(receiptReviewAmountDraftOnBlur("1,")).toEqual({ kind: "keep" });
    expect(receiptReviewAmountDraftOnBlur("12,300")).toEqual({
      kind: "commit",
      amount: 12300,
    });
  });

  it("数字と完全な桁区切りを整数へ変換する", () => {
    expect(parseReceiptReviewAmountDraft("12300")).toBe(12300);
    expect(parseReceiptReviewAmountDraft("12,300")).toBe(12300);
  });

  it("数値以外と安全な整数の範囲を超える値を拒否する", () => {
    expect(parseReceiptReviewAmountDraft("12円")).toBeNull();
    expect(parseReceiptReviewAmountDraft("9007199254740992")).toBeNull();
  });
});
