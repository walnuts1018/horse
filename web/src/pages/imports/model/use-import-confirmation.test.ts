import { describe, expect, it } from "vitest";

import {
  receiptDateInputValue,
  receiptOccurredAt,
  receiptTimeInputValue,
  persistedStatementConfirmationKeys,
  statementConfirmationIssue,
  statementSourceReference,
} from "./imports";

describe("receipt purchase date confirmation", () => {
  it("does not invent a date when extraction has no date", () => {
    expect(receiptDateInputValue()).toBe("");
    expect(receiptOccurredAt()).toBeNull();
  });

  it("uses the extracted date in Japan time for the date input", () => {
    expect(receiptDateInputValue("2025-04-01T00:30:00+09:00")).toBe("2025-04-01");
    expect(receiptDateInputValue("2025-04-01")).toBe("2025-04-01");
  });

  it("uses a representative timestamp for a date-only purchase without showing it as the actual time", () => {
    expect(receiptOccurredAt("2025-04-01", undefined, undefined, "DATE_ONLY")).toBe(
      "2025-04-01T03:00:00.000Z",
    );
    expect(receiptOccurredAt(undefined, "2025-04-01", undefined, "DATE_ONLY")).toBe(
      "2025-04-01T03:00:00.000Z",
    );
    expect(receiptTimeInputValue("2025-04-01T03:00:00.000Z", "DATE_ONLY")).toBe("");
    expect(receiptTimeInputValue("2025-04-01")).toBe("");
  });

  it("uses an explicitly entered time in Japan", () => {
    expect(receiptOccurredAt(undefined, "2025-04-01", "09:15")).toBe("2025-04-01T00:15:00.000Z");
  });

  it("preserves the extracted purchase time when correcting its date", () => {
    expect(receiptOccurredAt("2025-03-31T16:45:12.345Z", "2025-04-02")).toBe(
      "2025-04-01T16:45:12.345Z",
    );
    expect(receiptOccurredAt("2025-04-01T08:30:00+09:00", undefined, "09:15")).toBe(
      "2025-04-01T00:15:00.000Z",
    );
  });

  it("shows extracted time in Japan for the time input", () => {
    expect(receiptTimeInputValue("2025-03-31T16:45:12.345Z")).toBe("01:45");
  });

  it("rejects invalid manual dates and malformed extracted dates", () => {
    expect(receiptOccurredAt(undefined, "2025-02-30")).toBeNull();
    expect(receiptOccurredAt("invalid-date")).toBeNull();
  });
});

describe("statement purchase confirmation", () => {
  const entry = {
    row: 2,
    kind: "CARD_PURCHASE",
    occurredAt: "2025-04-01T12:00:00+09:00",
    merchant: "Market",
    amount: 100,
  };

  it("rejects return and refund amounts instead of changing their sign", () => {
    expect(statementConfirmationIssue({ ...entry, amount: -100 })).toBe(
      "返品・返金は確定できません",
    );
  });

  it("rejects zero, unsafe, and date-less purchase entries", () => {
    expect(statementConfirmationIssue({ ...entry, amount: 0 })).toBe("明細金額を確認してください");
    expect(statementConfirmationIssue({ ...entry, amount: Number.MAX_SAFE_INTEGER + 1 })).toBe(
      "明細金額を確認してください",
    );
    expect(statementConfirmationIssue({ ...entry, occurredAt: "" })).toBe(
      "購入日を確認してください",
    );
  });

  it("allows positive card purchases", () => {
    expect(statementConfirmationIssue(entry)).toBeUndefined();
  });

  it("accepts signed account transfers and rejects zero or unsafe amounts", () => {
    expect(
      statementConfirmationIssue({ ...entry, kind: "ACCOUNT_TRANSFER", amount: -100 }),
    ).toBeUndefined();
    expect(statementConfirmationIssue({ ...entry, kind: "ACCOUNT_TRANSFER", amount: 0 })).toBe(
      "振替金額を確認してください",
    );
    expect(
      statementConfirmationIssue({
        ...entry,
        kind: "ACCOUNT_TRANSFER",
        amount: Number.MAX_SAFE_INTEGER + 1,
      }),
    ).toBe("振替金額を確認してください");
  });

  it("accepts negative card bill payments and rejects invalid amount or date", () => {
    expect(
      statementConfirmationIssue({ ...entry, kind: "CARD_BILL_PAYMENT", amount: -100 }),
    ).toBeUndefined();
    expect(statementConfirmationIssue({ ...entry, kind: "CARD_BILL_PAYMENT", amount: 100 })).toBe(
      "カード引落額を確認してください",
    );
    expect(statementConfirmationIssue({ ...entry, kind: "CARD_BILL_PAYMENT", amount: 0 })).toBe(
      "カード引落額を確認してください",
    );
    expect(
      statementConfirmationIssue({
        ...entry,
        kind: "CARD_BILL_PAYMENT",
        amount: -100,
        occurredAt: "invalid-date",
      }),
    ).toBe("カード引落日を確認してください");
  });

  it("accepts positive external income and rejects invalid amount or date", () => {
    expect(
      statementConfirmationIssue({ ...entry, kind: "EXTERNAL_INCOME", amount: 100 }),
    ).toBeUndefined();
    expect(statementConfirmationIssue({ ...entry, kind: "EXTERNAL_INCOME", amount: -100 })).toBe(
      "入金額を確認してください",
    );
    expect(statementConfirmationIssue({ ...entry, kind: "EXTERNAL_INCOME", amount: 0 })).toBe(
      "入金額を確認してください",
    );
    expect(
      statementConfirmationIssue({
        ...entry,
        kind: "EXTERNAL_INCOME",
        amount: 100,
        occurredAt: "invalid-date",
      }),
    ).toBe("入金日を確認してください");
  });

  it("allows negative refund rows and rejects non-negative refund rows", () => {
    expect(statementConfirmationIssue({ ...entry, kind: "REFUND", amount: -100 })).toBeUndefined();
    expect(statementConfirmationIssue({ ...entry, kind: "REFUND", amount: 100 })).toBe(
      "返金額を確認してください",
    );
  });

  it("uses the parser row reference even when a transaction ID exists", () => {
    expect(
      statementSourceReference({ ...entry, sourceReference: "row:9", transactionId: "7" }),
    ).toBe("row:9");
    expect(statementSourceReference(entry)).toBe("row:2");
  });

  it("restores confirmed rows from document data after reload", () => {
    expect(
      persistedStatementConfirmationKeys({
        id: "document-1",
        status: "SUCCEEDED",
        confirmations: [{ sourceReference: "row:2", purchaseId: "purchase-1" }],
      }),
    ).toEqual(["document-1:row:2"]);
  });
});
