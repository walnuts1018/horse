import { describe, expect, it } from "vitest";

import type { ImportReceiptDraft } from "../../../shared/lib/purchase-storage";
import type { DocumentReview } from "./imports";

import { receiptItemsNeedingReview } from "./receipt-item-review";
import {
  addManualReceiptItem,
  deleteReceiptItemByKey,
  ensureReceiptItemKeys,
  reconcileReceiptConfirmationState,
  reconcileReceiptReviewDraft,
  rekeyReceiptConfirmationAfterStructureChange,
  restoreDeletedReceiptItem,
  restoreReceiptItemDeletion,
} from "./receipt-review-draft";

function item(name: string, printedAmount: number, grossAmount: number, confidence = 0.9) {
  return { name, printedAmount, grossAmount, confidence, verification: "UNRESOLVED" };
}

function hasChangedReceiptItems(result: DocumentReview["result"] | undefined): boolean {
  return Boolean(result && "reviewReceiptItemsChanged" in result && result.reviewReceiptItemsChanged);
}

function draftResult(value: NonNullable<DocumentReview["result"]>) {
  return value as Record<string, unknown>;
}

describe("reconcileReceiptReviewDraft", () => {
  it("AIの更新を取り込みながらユーザーが変更した項目だけを維持する", () => {
    const baseResult = {
      receipt: {
        merchant: "AI店舗",
        items: [item("AI商品", 100, 100, 0.7), item("未編集商品", 200, 200, 0.8)],
      },
      issues: ["旧指摘"],
    };
    const editedResult = {
      ...baseResult,
      receipt: {
        ...baseResult.receipt,
        merchant: "修正店舗",
        items: [{ ...baseResult.receipt.items[0], name: "修正商品" }, baseResult.receipt.items[1]],
      },
    };
    const latest: DocumentReview = {
      id: "document-1",
      status: "SUCCEEDED",
      result: {
        receipt: {
          merchant: "更新店舗",
          items: [
            item("AI商品", 100, 110, 0.99),
            item("更新された未編集商品", 220, 220, 0.99),
            item("新商品", 300, 300),
          ],
        },
        issues: ["新指摘"],
      },
    };
    const draft: ImportReceiptDraft = {
      householdId: "household-1",
      documentId: latest.id,
      baseResult: draftResult(baseResult),
      editedResult: draftResult(editedResult),
    };

    expect(reconcileReceiptReviewDraft(latest, draft).document.result).toEqual({
      receipt: {
        merchant: "修正店舗",
        items: ensureReceiptItemKeys([
          item("修正商品", 100, 110, 0.99),
          item("更新された未編集商品", 220, 220, 0.99),
          item("新商品", 300, 300),
        ]),
      },
      issues: ["新指摘"],
    });
  });

  it("AI再解析で品目順が変わった場合も一意な品目へ修正を戻す", () => {
    const baseResult = {
      receipt: { items: [item("牛乳", 200, 220), item("パン", 100, 110)] },
    };
    const editedResult = {
      receipt: {
        items: [{ ...baseResult.receipt.items[0], name: "低脂肪乳" }, baseResult.receipt.items[1]],
      },
    };
    const latest: DocumentReview = {
      id: "document-1",
      status: "SUCCEEDED",
      result: { receipt: { items: [item("パン", 100, 110), item("牛乳", 200, 220)] } },
    };
    const reconciled = reconcileReceiptReviewDraft(latest, {
      householdId: "household-1",
      documentId: latest.id,
      baseResult: draftResult(baseResult),
      editedResult: draftResult(editedResult),
    });

    expect(reconciled.document.result?.receipt?.items).toEqual(
      ensureReceiptItemKeys([item("パン", 100, 110), item("低脂肪乳", 200, 220)]),
    );
    expect(reconciled.conflicts).toEqual([]);
  });

  it("AI再解析後に一意の品目を特定できない修正を別品目へ適用せず競合にする", () => {
    const baseResult = { receipt: { items: [item("牛乳", 200, 220)] } };
    const editedResult = {
      receipt: { items: [{ ...baseResult.receipt.items[0], name: "低脂肪乳" }] },
    };
    const latest: DocumentReview = {
      id: "document-1",
      status: "SUCCEEDED",
      result: { receipt: { items: [item("ヨーグルト", 201, 220)] } },
    };
    const reconciled = reconcileReceiptReviewDraft(latest, {
      householdId: "household-1",
      documentId: latest.id,
      baseResult: draftResult(baseResult),
      editedResult: draftResult(editedResult),
    });

    expect(reconciled.document.result?.receipt?.items?.[0]?.name).toBe("ヨーグルト");
    expect(reconciled.conflicts).toEqual(["品目1"]);
  });

  it("削除した品目だけを再解析結果から除き、順序が変わっても別品目を残す", () => {
    const baseItems = [item("りんご", 100, 100), item("牛乳", 200, 200)];
    const baseResult = { receipt: { items: baseItems } };
    const editedResult = { receipt: { items: [baseItems[1]] } };
    const latest: DocumentReview = {
      id: "document-1",
      status: "SUCCEEDED",
      result: { receipt: { items: [item("牛乳", 200, 210), item("りんご", 100, 100)] } },
    };

    const reconciled = reconcileReceiptReviewDraft(latest, {
      householdId: "household-1",
      documentId: latest.id,
      baseResult: draftResult(baseResult),
      editedResult: draftResult(editedResult),
    });

    expect(reconciled.document.result?.receipt?.items?.map((entry) => entry.name)).toEqual([
      "牛乳",
    ]);
    expect(reconciled.conflicts).toEqual([]);
  });

  it("追加した品目を維持し、AIの証拠regionを追加品目へコピーしない", () => {
    const original = {
      ...item("パン", 100, 100),
      region: { page: 1, top: 0.1, left: 0.1, bottom: 0.2, right: 0.5 },
    };
    const manual = { ...item("手入力品目", 300, 300), key: "manual:one" };
    const baseResult = { receipt: { items: [original] } };
    const editedResult = { receipt: { items: [original, manual] } };
    const latest: DocumentReview = {
      id: "document-1",
      status: "SUCCEEDED",
      result: { receipt: { items: [original, item("新しいAI品目", 200, 200)] } },
    };

    const reconciled = reconcileReceiptReviewDraft(latest, {
      householdId: "household-1",
      documentId: latest.id,
      baseResult: draftResult(baseResult),
      editedResult: draftResult(editedResult),
    });
    const resultItems = reconciled.document.result?.receipt?.items ?? [];

    expect(resultItems.map((entry) => entry.name)).toEqual(["パン", "新しいAI品目", "手入力品目"]);
    expect(resultItems.at(-1)?.region).toBeUndefined();
  });

  it("同じ印字情報の品目を再解析後に一意に識別できない削除は適用しない", () => {
    const duplicate = item("同じ商品", 100, 100);
    const baseResult = { receipt: { items: [duplicate, duplicate] } };
    const editedResult = { receipt: { items: [duplicate] } };
    const latest: DocumentReview = {
      id: "document-1",
      status: "SUCCEEDED",
      result: { receipt: { items: [duplicate, duplicate] } },
    };

    const reconciled = reconcileReceiptReviewDraft(latest, {
      householdId: "household-1",
      documentId: latest.id,
      baseResult: draftResult(baseResult),
      editedResult: draftResult(editedResult),
    });

    expect(reconciled.document.result?.receipt?.items).toHaveLength(2);
    expect(reconciled.conflicts).toEqual(["品目2"]);
  });
});

describe("receipt item structure changes", () => {
  it("追加品目へ証拠regionを作らず、既存品目のkeyを維持する", () => {
    const original = [
      {
        ...item("パン", 100, 100),
        key: "item-0",
        region: { page: 1, top: 0.1, left: 0.1, bottom: 0.2, right: 0.5 },
      },
    ];
    const added = addManualReceiptItem(original, "manual:new");

    expect(added[0].key).toBe(ensureReceiptItemKeys(original)[0].key);
    expect(added[0].region).toEqual(original[0].region);
    expect(added[1].key).toBe("manual:new");
    expect(added[1].region).toBeUndefined();
  });

  it("品目追加後も既存の人確認状態を保ち、追加品目を未確認にする", () => {
    const original = [item("パン", 100, 100)];
    const keyed = ensureReceiptItemKeys(original);
    const added = addManualReceiptItem(keyed, "manual:new");
    const existingKey = keyed[0].key!;
    const state = rekeyReceiptConfirmationAfterStructureChange(
      { receiptItemReviewStates: { [existingKey]: "CONFIRMED" } },
      original,
      keyed,
      added,
    );

    expect(state.receiptItemReviewStates).toEqual({ [existingKey]: "CONFIRMED" });
    expect(receiptItemsNeedingReview(added, state.receiptItemReviewStates)).toEqual([
      { item: added[1], index: 1, state: "UNREVIEWED" },
    ]);
  });

  it("削除品目の金額・配賦・訂正理由を破棄し、残存keyと決済だけを保つ", () => {
    const original = [item("パン", 100, 100), item("牛乳", 200, 200)];
    const keyed = ensureReceiptItemKeys(original);
    const removedKey = keyed[0].key!;
    const retainedKey = keyed[1].key!;
    const nextItems = deleteReceiptItemByKey(keyed, removedKey);
    const state = rekeyReceiptConfirmationAfterStructureChange(
      {
        selectedCandidate: {
          id: "candidate",
          amounts: [100, 200],
          computedTotal: 300,
          consistent: true,
          issues: [],
        },
        manualAmountOverrides: { [removedKey]: 101, [retainedKey]: 201 },
        manualAmountOverrideReason: "OCRの読み間違い",
        receiptItemReviewStates: {
          [removedKey]: "UNRESOLVED",
          [retainedKey]: "CONFIRMED",
        },
        itemAllocations: {
          [removedKey]: [{ poolId: "pool-1", weight: 100 }],
          [retainedKey]: [{ poolId: "pool-2", weight: 100 }],
        },
        payments: [{ key: "payment", paymentAccountId: "account-1", amountYen: 300 }],
      },
      original,
      keyed,
      nextItems,
    );

    expect(state).toEqual({
      manualAmountOverrides: { [retainedKey]: 201 },
      itemAllocations: { [retainedKey]: [{ poolId: "pool-2", weight: 100 }] },
      receiptItemReviewStates: { [retainedKey]: "CONFIRMED" },
      payments: [{ key: "payment", paymentAccountId: "account-1", amountYen: 300 }],
    });
  });

  it("品目の編集中金額と購入日時を解析更新後も復元する", () => {
    const baseItems = [item("牛乳", 200, 220), item("パン", 100, 110)];
    const keyedItems = ensureReceiptItemKeys(baseItems);
    const baseResult = { receipt: { items: baseItems } };
    const latest: DocumentReview = {
      id: "document-1",
      status: "SUCCEEDED",
      result: { receipt: { items: [item("パン", 100, 110), item("牛乳", 200, 220)] } },
    };
    const result = reconcileReceiptConfirmationState(baseResult, latest.result, {
      amountDrafts: { [keyedItems[0].key!]: "2,2" },
      purchaseDate: "",
      purchaseTime: "09:3",
    });

    expect(result.state).toEqual({
      amountDrafts: {
        [ensureReceiptItemKeys(latest.result!.receipt!.items ?? [])[1].key!]: "2,2",
      },
      purchaseDate: "",
      purchaseTime: "09:3",
    });
    expect(result.conflicts).toEqual([]);
  });

  it("品目削除時に対応する編集中金額だけを下書きから除く", () => {
    const original = [item("パン", 100, 100), item("牛乳", 200, 200)];
    const keyed = ensureReceiptItemKeys(original);
    const nextItems = deleteReceiptItemByKey(keyed, keyed[0].key!);
    const state = rekeyReceiptConfirmationAfterStructureChange(
      {
        amountDrafts: { [keyed[0].key!]: "100x", [keyed[1].key!]: "200x" },
        purchaseDate: "2026-10-14",
      },
      original,
      keyed,
      nextItems,
    );

    expect(state.amountDrafts).toEqual({ [keyed[1].key!]: "200x" });
    expect(state.purchaseDate).toBe("2026-10-14");
  });

  it("品目削除を元に戻し、品目状態を復元して残存品目の変更を保つ", () => {
    const original = [item("パン", 100, 110), item("牛乳", 200, 220)];
    const keyed = ensureReceiptItemKeys(original);
    const deletedKey = keyed[0].key!;
    const retainedKey = keyed[1].key!;
    const confirmationState = {
      selectedCandidate: {
        id: "candidate-1",
        amounts: [110, 220],
        computedTotal: 330,
        consistent: true,
        issues: [],
      },
      manualAmountOverrides: { [deletedKey]: 105, [retainedKey]: 215 },
      manualAmountOverrideReason: "OCRの読み間違い",
      amountDrafts: { [deletedKey]: "1,0" },
      itemAllocations: {
        [deletedKey]: [{ poolId: "pool-1", weight: 2 }],
      },
      receiptItemReviewStates: { [deletedKey]: "CORRECTED" as const },
      payments: [{ key: "payment-1", paymentAccountId: "account-1", amountYen: 320 }],
      purchaseDate: "2026-10-14",
      purchaseTime: "09:30",
    };
    const deletedItems = deleteReceiptItemByKey(keyed, deletedKey);
    const afterDelete = rekeyReceiptConfirmationAfterStructureChange(
      confirmationState,
      original,
      keyed,
      deletedItems,
    );
    const afterAnotherEdit = {
      ...afterDelete,
      manualAmountOverrides: { ...afterDelete.manualAmountOverrides, [retainedKey]: 210 },
    };
    const undo = {
      documentId: "document-1",
      item: keyed[0],
      index: 0,
      reviewTargetIndex: 1,
      remainingItemKeys: deletedItems.map((entry) => entry.key!),
      remainingItems: deletedItems,
      confirmationState,
      balanced: true,
    };
    const document: DocumentReview = {
      id: "document-1",
      status: "SUCCEEDED",
      receiptConfirmationState: afterAnotherEdit,
      result: {
        balanced: false,
        reviewReceiptItemsChanged: true,
        receipt: { items: deletedItems },
        candidates: [confirmationState.selectedCandidate],
      } as NonNullable<DocumentReview["result"]> & { reviewReceiptItemsChanged: boolean },
    };
    const restored = restoreReceiptItemDeletion(document, undo);

    expect(restored?.result?.receipt?.items).toEqual(keyed);
    expect(restored?.result?.balanced).toBe(true);
    expect(hasChangedReceiptItems(restored?.result)).toBe(false);
    expect(restored?.receiptConfirmationState).toEqual({
      ...confirmationState,
      manualAmountOverrides: { [deletedKey]: 105, [retainedKey]: 210 },
    });
  });

  it("Undo前に別品目を編集した場合は編集値を残して税候補を無効にする", () => {
    const original = [item("パン", 100, 110), item("牛乳", 200, 220)];
    const keyed = ensureReceiptItemKeys(original);
    const confirmationState = {
      selectedCandidate: {
        id: "candidate-1",
        amounts: [110, 220],
        computedTotal: 330,
        consistent: true,
        issues: [],
      },
    };
    const remainingItems = deleteReceiptItemByKey(keyed, keyed[0].key!);
    const changedItem = { ...remainingItems[0], name: "低脂肪乳", grossAmount: 225 };
    const restored = restoreReceiptItemDeletion(
      {
        id: "document-1",
        status: "SUCCEEDED",
        result: {
          balanced: false,
          reviewReceiptItemsChanged: true,
          receipt: { items: [changedItem] },
          candidates: [confirmationState.selectedCandidate],
        } as NonNullable<DocumentReview["result"]> & { reviewReceiptItemsChanged: boolean },
      },
      {
        documentId: "document-1",
        item: keyed[0],
        index: 0,
        reviewTargetIndex: 0,
        remainingItemKeys: remainingItems.map((entry) => entry.key!),
        remainingItems,
        confirmationState,
      },
    );
    expect(restored?.result?.receipt?.items?.map((entry) => entry.name)).toEqual([
      "パン",
      "低脂肪乳",
    ]);
    expect(restored?.result?.balanced).toBe(false);
    expect(hasChangedReceiptItems(restored?.result)).toBe(true);
    expect(restored?.receiptConfirmationState?.selectedCandidate).toBeUndefined();
  });

  it("ほかの品目が追加・削除された後は古い削除操作を適用しない", () => {
    const original = [item("パン", 100, 110), item("牛乳", 200, 220)];
    const keyed = ensureReceiptItemKeys(original);
    const deletedItems = deleteReceiptItemByKey(keyed, keyed[0].key!);
    const changedItems = addManualReceiptItem(deletedItems, "manual:new");

    expect(
      restoreDeletedReceiptItem(
        changedItems,
        {},
        {
          documentId: "document-1",
          item: keyed[0],
          index: 0,
          reviewTargetIndex: 0,
          remainingItemKeys: deletedItems.map((entry) => entry.key!),
          remainingItems: deletedItems,
          confirmationState: {},
        },
      ),
    ).toBeUndefined();
  });
});

describe("reconcileReceiptConfirmationState", () => {
  it("再解析で品目順と候補IDが変わっても税候補・金額・配賦を同じ品目へ戻す", () => {
    const baseResult = {
      receipt: { items: [item("牛乳", 100, 110), item("パン", 200, 220)] },
    };
    const latestResult = {
      receipt: { items: [item("パン", 200, 220), item("牛乳", 100, 110)] },
      candidates: [
        {
          id: "candidate-new",
          amounts: [220, 110],
          computedTotal: 330,
          consistent: true,
          issues: [],
        },
      ],
    };

    const reconciled = reconcileReceiptConfirmationState(baseResult, latestResult, {
      selectedCandidate: {
        id: "candidate-old",
        amounts: [110, 220],
        computedTotal: 330,
        consistent: true,
        issues: [],
      },
      manualAmountOverrides: { "item-0": 111 },
      manualAmountOverrideReason: "印字額を確認",
      payments: [{ key: "manual", paymentAccountId: "account-1", amountYen: 331 }],
      itemAllocations: { "item-1": [{ poolId: "pool-1", weight: 100 }] },
      receiptItemReviewStates: { "item-0": "CORRECTED", "item-1": "UNRESOLVED" },
    });

    const keyedLatestItems = ensureReceiptItemKeys(latestResult.receipt.items);
    expect(reconciled.state).toEqual({
      selectedCandidate: latestResult.candidates[0],
      manualAmountOverrides: { [keyedLatestItems[1].key!]: 111 },
      manualAmountOverrideReason: "印字額を確認",
      payments: [{ key: "manual", paymentAccountId: "account-1", amountYen: 331 }],
      itemAllocations: { [keyedLatestItems[0].key!]: [{ poolId: "pool-1", weight: 100 }] },
      receiptItemReviewStates: {
        [keyedLatestItems[1].key!]: "CORRECTED",
        [keyedLatestItems[0].key!]: "UNRESOLVED",
      },
    });
    expect(reconciled.conflicts).toEqual([]);
  });

  it("一意に対応しない品目や候補選択を復元せず競合として返す", () => {
    const baseResult = { receipt: { items: [item("牛乳", 100, 110)] } };
    const latestResult = {
      receipt: { items: [item("別商品", 101, 110)] },
      candidates: [],
    };

    const reconciled = reconcileReceiptConfirmationState(baseResult, latestResult, {
      selectedCandidate: {
        id: "candidate-old",
        amounts: [110],
        computedTotal: 110,
        consistent: false,
        issues: [],
      },
      manualAmountOverrides: { "item-0": 111 },
      itemAllocations: { "item-0": [{ poolId: "pool-1", weight: 100 }] },
    });

    expect(reconciled.state).toEqual({ manualAmountOverrides: {}, itemAllocations: {} });
    expect(reconciled.conflicts).toEqual(["税額候補の選択", "品目1の確認状態", "品目1の確認状態"]);
  });
});
