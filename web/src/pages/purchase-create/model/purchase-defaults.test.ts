import { afterEach, describe, expect, it, vi } from "vitest";

import {
  loadPaymentAccountForMerchant,
  loadPurchaseDefaults,
  savePurchaseDefaults,
} from "./purchase-defaults";

const values = new Map<string, string>();
const storage = {
  getItem: (key: string) => values.get(key) ?? null,
  setItem: (key: string, value: string) => values.set(key, value),
};

afterEach(() => {
  values.clear();
  vi.unstubAllGlobals();
});

describe("purchase defaults", () => {
  it("restores the last merchant and payment account for the same member and household", () => {
    vi.stubGlobal("window", { localStorage: storage });

    savePurchaseDefaults("member-1", "household-1", {
      merchant: "スーパーA",
      paymentAccountId: "account-2",
    });

    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1", "account-2"])).toEqual({
      merchant: "スーパーA",
      paymentAccountId: "account-2",
    });
  });

  it("keeps defaults separate by household and falls back when an account no longer exists", () => {
    vi.stubGlobal("window", { localStorage: storage });

    savePurchaseDefaults("member-1", "household-1", {
      merchant: "スーパーA",
      paymentAccountId: "removed-account",
    });

    expect(loadPurchaseDefaults("member-1", "household-2", ["account-2"])).toEqual({
      merchant: "",
      paymentAccountId: "account-2",
    });
    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1"])).toEqual({
      merchant: "スーパーA",
      paymentAccountId: "account-1",
    });
  });

  it("remembers the latest payment account for each merchant", () => {
    vi.stubGlobal("window", { localStorage: storage });

    savePurchaseDefaults("member-1", "household-1", {
      merchant: "スーパーA",
      paymentAccountId: "account-1",
    });
    savePurchaseDefaults("member-1", "household-1", {
      merchant: "スーパーB",
      paymentAccountId: "account-2",
    });

    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1", "account-2"])).toEqual({
      merchant: "スーパーB",
      paymentAccountId: "account-2",
    });
    expect(loadPaymentAccountForMerchant("member-1", "household-1", "スーパーA", [
      "account-1",
      "account-2",
    ])).toBe("account-1");
    expect(loadPaymentAccountForMerchant("member-1", "household-1", "未登録店舗", [
      "account-1",
      "account-2",
    ])).toBe("account-2");
  });

  it("supports the previous format and ignores malformed or removed account entries", () => {
    vi.stubGlobal("window", { localStorage: storage });
    const key = 'horse:purchase-defaults:["member-1","household-1"]';
    values.set(key, JSON.stringify({ merchant: "スーパーA", paymentAccountId: "account-2" }));

    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1", "account-2"]))
      .toEqual({ merchant: "スーパーA", paymentAccountId: "account-2" });

    values.set(
      key,
      JSON.stringify({
        merchant: "スーパーA",
        paymentAccountId: "removed-account",
        paymentAccountIdsByMerchant: { スーパーA: "removed-account" },
      }),
    );
    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1", "account-2"]))
      .toEqual({ merchant: "スーパーA", paymentAccountId: "account-1" });

    values.set(key, "invalid json");
    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1"]))
      .toEqual({ merchant: "", paymentAccountId: "account-1" });
    savePurchaseDefaults("member-1", "household-1", {
      merchant: "スーパーC",
      paymentAccountId: "account-1",
    });
    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1"]))
      .toEqual({ merchant: "スーパーC", paymentAccountId: "account-1" });
  });

  it("keeps merchant accounts isolated by member and household", () => {
    vi.stubGlobal("window", { localStorage: storage });
    savePurchaseDefaults("member-1", "household-1", {
      merchant: "スーパーA",
      paymentAccountId: "account-1",
    });

    expect(loadPaymentAccountForMerchant("member-2", "household-1", "スーパーA", [
      "account-2",
    ])).toBe("account-2");
    expect(loadPaymentAccountForMerchant("member-1", "household-2", "スーパーA", [
      "account-2",
    ])).toBe("account-2");
  });
});
