import { describe, expect, it } from "vitest";

import {
  emptyPurchaseEditHistory,
  recordPurchaseEdit,
  redoPurchaseEdit,
  undoPurchaseEdit,
} from "./purchase-edit-history";

const item = (key: string) => ({
  key,
  name: key,
  amount: "100",
  categoryId: "",
  allocations: [],
});

describe("purchase edit history", () => {
  it("複数の追加・削除・負担変更を順にUndo/Redoし、新しい操作でRedo履歴を破棄する", () => {
    let history = emptyPurchaseEditHistory();
    history = recordPurchaseEdit(history, { items: [] });
    history = recordPurchaseEdit(history, { items: [item("first")] });

    const undoneDelete = undoPurchaseEdit(history, { items: [] });
    expect(undoneDelete?.snapshot.items).toEqual([item("first")]);
    const undoneAdd = undoPurchaseEdit(undoneDelete!.history, { items: [item("first")] });
    expect(undoneAdd?.snapshot.items).toEqual([]);

    const redoneAdd = redoPurchaseEdit(undoneAdd!.history, { items: [] });
    expect(redoneAdd?.snapshot.items).toEqual([item("first")]);
    const redoneDelete = redoPurchaseEdit(redoneAdd!.history, { items: [item("first")] });
    expect(redoneDelete?.snapshot.items).toEqual([]);

    const afterNewEdit = recordPurchaseEdit(redoneDelete!.history, { items: [] });
    expect(redoPurchaseEdit(afterNewEdit, { items: [] })).toBeNull();
  });

  it("履歴コピーのUndo/Redoでは店名と品目を同じ操作で復元する", () => {
    const history = recordPurchaseEdit(emptyPurchaseEditHistory(), {
      items: [],
      categoryId: "category-before",
      merchant: "前の店",
    });
    const copiedItems = [item("copied")];
    const undone = undoPurchaseEdit(history, {
      items: copiedItems,
      categoryId: "",
      merchant: "コピー元",
    });

    expect(undone?.snapshot).toEqual({
      items: [],
      categoryId: "category-before",
      merchant: "前の店",
    });
    const redone = redoPurchaseEdit(undone!.history, {
      items: [],
      categoryId: "category-before",
      merchant: "前の店",
    });
    expect(redone?.snapshot).toEqual({
      items: copiedItems,
      categoryId: "",
      merchant: "コピー元",
    });
  });
});
