import { afterEach, describe, expect, it, vi } from "vitest";

import {
  loadPaymentAccountForMerchant,
  loadPurchaseDefaults,
  initialPurchaseDefaultsFromHistory,
  merchantsFromHistory,
  orderPaymentAccountsByUsage,
  paymentAccountFromHistory,
  purchaseDefaultHistoryForHousehold,
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
  it("hides the previous household history until the new household query completes", () => {
    const previousState = {
      householdId: "household-1",
      history: [
        {
          merchant: "スーパーA",
          paymentAccountId: "account-1",
          purchaseCount: 2,
          lastUsedAt: "2026-10-10T09:00:00Z",
        },
      ],
      loaded: true,
    };

    expect(purchaseDefaultHistoryForHousehold(previousState, "household-2")).toEqual({
      householdId: "household-2",
      history: [],
      loaded: false,
    });
    expect(purchaseDefaultHistoryForHousehold(previousState, "household-1")).toBe(previousState);
  });

  it("uses the latest household merchant and the most-used exact merchant account", () => {
    const history = [
      {
        merchant: "スーパーA",
        paymentAccountId: "account-1",
        purchaseCount: 2,
        lastUsedAt: "2026-10-10T09:00:00Z",
      },
      {
        merchant: "スーパーA",
        paymentAccountId: "account-2",
        purchaseCount: 5,
        lastUsedAt: "2026-10-09T09:00:00Z",
      },
      {
        merchant: "スーパーB",
        paymentAccountId: "account-2",
        purchaseCount: 1,
        lastUsedAt: "2026-10-11T09:00:00Z",
      },
    ];

    expect(initialPurchaseDefaultsFromHistory(history, ["account-1", "account-2"])).toEqual({
      merchant: "スーパーB",
      paymentAccountId: "account-2",
    });
    expect(paymentAccountFromHistory(history, "スーパーA", ["account-1", "account-2"])).toBe(
      "account-2",
    );
  });

  it("keeps unconfirmed merchant spellings separate and does not replace removed accounts", () => {
    const history = [
      {
        merchant: "AEON",
        paymentAccountId: "removed-account",
        purchaseCount: 3,
        lastUsedAt: "2026-10-10T09:00:00Z",
      },
      {
        merchant: "イオン",
        paymentAccountId: "account-1",
        purchaseCount: 2,
        lastUsedAt: "2026-10-09T09:00:00Z",
      },
    ];

    expect(paymentAccountFromHistory(history, "AEON", ["account-1"])).toBe("");
    expect(paymentAccountFromHistory(history, "AEON〇〇店", ["account-1"])).toBeUndefined();
    expect(merchantsFromHistory(history)).toEqual(["AEON", "イオン"]);
  });

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
