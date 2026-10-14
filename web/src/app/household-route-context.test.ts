import { describe, expect, it, vi } from "vitest";

import { createUnsavedChangesRegistry, hasSameNavigationTarget } from "./household-route-context";

describe("unsaved changes registry", () => {
  it("keeps another route dirty when one route releases its registration", () => {
    const registry = createUnsavedChangesRegistry();
    const listener = vi.fn();
    registry.subscribe(listener);

    registry.report("household:a:page:ledger", true);
    registry.report("household:b:page:ledger", true);
    registry.clear("household:a:page:ledger");

    expect(registry.hasUnsavedChanges()).toBe(true);
    expect(registry.getDirtyIds()).toEqual(["household:b:page:ledger"]);
    expect(listener).toHaveBeenCalledTimes(3);
  });

  it("treats a household parameter change as a different navigation target", () => {
    const current = {
      pathname: "/households/one/ledger",
      params: { householdId: "one" },
      search: { period: "2026-10" },
    };

    expect(
      hasSameNavigationTarget(current, {
        ...current,
        params: { householdId: "two" },
      }),
    ).toBe(false);
    expect(hasSameNavigationTarget(current, { ...current })).toBe(true);
  });
});
