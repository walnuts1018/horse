import { afterEach, describe, expect, it, vi } from "vitest";

import { loadPurchaseDefaults, savePurchaseDefaults } from "./purchase-defaults";

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
});
