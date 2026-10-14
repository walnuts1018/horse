import { describe, expect, it } from "vitest";

import { receiptEvidenceSelectionIndex } from "./receipt-evidence-selection";

describe("receiptEvidenceSelectionIndex", () => {
  it("stable keyから追加・削除後の品目位置を解決する", () => {
    expect(
      receiptEvidenceSelectionIndex(
        { itemKey: "item-c", index: 2, focusEditor: false },
        ["item-a", "item-c"],
        0,
      ),
    ).toBe(1);
  });

  it("選択品目が削除された場合は現在のindexへ戻す", () => {
    expect(
      receiptEvidenceSelectionIndex(
        { itemKey: "item-b", index: 1, focusEditor: false },
        ["item-a", "item-c"],
        1,
      ),
    ).toBe(1);
  });

  it("まだ選択状態がない場合は既存のindexを使う", () => {
    expect(receiptEvidenceSelectionIndex(null, ["item-a", "item-b"], 1)).toBe(1);
  });

  it("選択品目の削除でindexが範囲外になった場合は末尾へ丸める", () => {
    expect(
      receiptEvidenceSelectionIndex(
        { itemKey: "item-b", index: 1, focusEditor: false },
        ["item-a"],
        1,
      ),
    ).toBe(0);
  });
});
