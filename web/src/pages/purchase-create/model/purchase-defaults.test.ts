import { afterEach, describe, expect, it, vi } from "vitest";

import {
  loadPaymentAccountForMerchant,
  loadPurchaseDefaults,
  orderPaymentAccountsByUsage,
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
  it("prioritizes frequently used payment accounts without changing tie order", () => {
    const accounts = [{ id: "account-1" }, { id: "account-2" }, { id: "account-3" }];
    const history = [
      { paymentAllocations: [{ paymentAccountId: "account-2" }] },
      {
        paymentAllocations: [{ paymentAccountId: "account-1" }, { paymentAccountId: "account-2" }],
      },
      {
        paymentAllocations: [{ paymentAccountId: "account-1" }, { paymentAccountId: "account-2" }],
      },
    ];

    expect(orderPaymentAccountsByUsage(accounts, history).map(({ id }) => id)).toEqual([
      "account-2",
      "account-1",
      "account-3",
    ]);
  });

  it("leaves the account blank when several accounts exist without a saved choice", () => {
    vi.stubGlobal("window", { localStorage: storage });

    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1", "account-2"])).toEqual({
      merchant: "",
      paymentAccountId: "",
    });
    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1"])).toEqual({
      merchant: "",
      paymentAccountId: "account-1",
    });
  });

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

  it("keeps defaults separate by household and leaves removed accounts unselected", () => {
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
      paymentAccountId: "",
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
    expect(
      loadPaymentAccountForMerchant("member-1", "household-1", "スーパーA", [
        "account-1",
        "account-2",
      ]),
    ).toBe("account-1");
    expect(
      loadPaymentAccountForMerchant("member-1", "household-1", "未登録店舗", [
        "account-1",
        "account-2",
      ]),
    ).toBe("account-2");
  });

  it("does not replace a deleted merchant account with another account", () => {
    vi.stubGlobal("window", { localStorage: storage });

    savePurchaseDefaults("member-1", "household-1", {
      merchant: "スーパーA",
      paymentAccountId: "removed-account",
    });
    savePurchaseDefaults("member-1", "household-1", {
      merchant: "スーパーB",
      paymentAccountId: "account-2",
    });

    expect(
      loadPaymentAccountForMerchant("member-1", "household-1", "スーパーA", [
        "account-1",
        "account-2",
      ]),
    ).toBe("");
    expect(
      loadPaymentAccountForMerchant("member-1", "household-1", "未登録店舗", [
        "account-1",
        "account-2",
      ]),
    ).toBe("account-2");
  });

  it("supports the previous format and ignores malformed or removed account entries", () => {
    vi.stubGlobal("window", { localStorage: storage });
    const key = 'horse:purchase-defaults:["member-1","household-1"]';
    values.set(key, JSON.stringify({ merchant: "スーパーA", paymentAccountId: "account-2" }));

    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1", "account-2"])).toEqual({
      merchant: "スーパーA",
      paymentAccountId: "account-2",
    });

    values.set(
      key,
      JSON.stringify({
        merchant: "スーパーA",
        paymentAccountId: "removed-account",
        paymentAccountIdsByMerchant: { スーパーA: "removed-account" },
      }),
    );
    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1", "account-2"])).toEqual({
      merchant: "スーパーA",
      paymentAccountId: "",
    });

    values.set(key, "invalid json");
    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1", "account-2"])).toEqual({
      merchant: "",
      paymentAccountId: "",
    });
    savePurchaseDefaults("member-1", "household-1", {
      merchant: "スーパーC",
      paymentAccountId: "account-1",
    });
    expect(loadPurchaseDefaults("member-1", "household-1", ["account-1"])).toEqual({
      merchant: "スーパーC",
      paymentAccountId: "account-1",
    });
  });

  it("keeps merchant accounts isolated by member and household", () => {
    vi.stubGlobal("window", { localStorage: storage });
    savePurchaseDefaults("member-1", "household-1", {
      merchant: "スーパーA",
      paymentAccountId: "account-1",
    });

    expect(
      loadPaymentAccountForMerchant("member-2", "household-1", "スーパーA", ["account-2"]),
    ).toBe("account-2");
    expect(
      loadPaymentAccountForMerchant("member-1", "household-2", "スーパーA", ["account-2"]),
    ).toBe("account-2");
  });
});
