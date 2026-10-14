import { describe, expect, it } from "vitest";

import { selectableLedgerItemIds } from "./ledger-item-selection";

describe("selectableLedgerItemIds", () => {
  it("締め済み購入の品目を一括編集対象から除外する", () => {
    const ids = selectableLedgerItemIds([
      { status: "OPEN", items: [{ id: "open-item" }] },
      { status: "CLOSED", items: [{ id: "closed-item" }] },
    ]);

    expect(ids).toEqual(new Set(["open-item"]));
  });
});
