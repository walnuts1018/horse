import { describe, expect, it } from "vitest";

import type { LedgerItemDraft } from "./purchase-item-draft";

import {
  ledgerItemFieldPatch,
  rebaseLedgerItemDraft,
  reverseLedgerItemFieldPatch,
} from "./ledger-item-autosave";

const draft = (overrides: Partial<LedgerItemDraft> = {}): LedgerItemDraft => ({
  name: "りんご",
  amountYen: "300",
  quantity: "1",
  quantityUnit: "個",
  printedUnitPriceYen: "",
  printedBasisQuantity: "",
  printedUnitPriceUnit: "",
  categoryId: "fruit",
  productVariantId: "",
  allocations: [{ poolId: "pool", weight: 1 }],
  productLearning: "ITEM_ONLY",
  variantName: "",
  ...overrides,
});

describe("ledger item autosave", () => {
  it("品名とカテゴリ以外を保存patchへ含めない", () => {
    expect(ledgerItemFieldPatch(draft(), draft({ name: "  青りんご ", categoryId: "" }))).toEqual({
      name: "青りんご",
      categoryId: null,
    });
  });

  it("Undo patchは直前に保存した品名とカテゴリだけを戻す", () => {
    expect(reverseLedgerItemFieldPatch({ name: "青りんご", categoryId: null }, draft())).toEqual({
      name: "りんご",
      categoryId: "fruit",
    });
  });

  it("保存中に編集した値を残し、ほかの項目は最新サーバー値へ再baseする", () => {
    const submitted = draft({ name: "青りんご" });
    const current = draft({ name: "青りんご 特大", amountYen: "350" });
    const server = draft({ name: "青りんご", amountYen: "320", categoryId: "snack" });

    expect(rebaseLedgerItemDraft(current, submitted, draft(), server)).toEqual(
      draft({ name: "青りんご 特大", amountYen: "350", categoryId: "snack" }),
    );
  });

  it("保存前から未保存の項目も入力値を保持する", () => {
    const previousBaseline = draft();
    const submitted = draft({ name: "青りんご", amountYen: "350" });
    const server = draft({ name: "青りんご", amountYen: "320" });

    expect(rebaseLedgerItemDraft(submitted, submitted, previousBaseline, server)).toEqual(
      draft({ name: "青りんご", amountYen: "350" }),
    );
  });
});
