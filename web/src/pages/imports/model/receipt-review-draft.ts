import type { ImportReceiptDraft } from "../../../shared/lib/purchase-storage";
import type { DocumentReview, ExtractedItem, ReceiptConfirmationState } from "./imports";

import { isRecord } from "../../../shared/lib/guard";

export type ReceiptItemDeletionUndo = {
  documentId: string;
  item: ExtractedItem;
  index: number;
  reviewTargetIndex: number;
  remainingItemKeys: string[];
  remainingItems: ExtractedItem[];
  confirmationState: ReceiptConfirmationState;
  balanced?: boolean;
  reviewReceiptItemsChanged?: boolean;
};

function equal(left: unknown, right: unknown): boolean {
  if (Object.is(left, right)) return true;
  if (Array.isArray(left) && Array.isArray(right)) {
    return left.length === right.length && left.every((value, index) => equal(value, right[index]));
  }
  if (!isRecord(left) || !isRecord(right)) return false;
  const leftKeys = Object.keys(left);
  const rightKeys = Object.keys(right);
  return (
    leftKeys.length === rightKeys.length &&
    leftKeys.every((key) => Object.hasOwn(right, key) && equal(left[key], right[key]))
  );
}

function isReceiptItem(value: unknown): value is Record<string, unknown> & ExtractedItem {
  return (
    isRecord(value) &&
    typeof value.name === "string" &&
    typeof value.printedAmount === "number" &&
    typeof value.grossAmount === "number"
  );
}

function sourceIdentity(item: ExtractedItem): string {
  return JSON.stringify([
    item.name.trim().replace(/\s+/g, " ").toLocaleLowerCase("ja-JP"),
    item.printedAmount,
    item.discount ?? null,
    item.priceBasis ?? null,
    item.taxGroup ?? null,
    item.region
      ? [item.region.page, item.region.top, item.region.left, item.region.bottom, item.region.right]
      : null,
  ]);
}

function sourceRegionIdentity(item: ExtractedItem): string | undefined {
  if (!item.region) return undefined;
  return JSON.stringify([
    item.printedAmount,
    item.region.page,
    item.region.top,
    item.region.left,
    item.region.bottom,
    item.region.right,
  ]);
}

function hasSourceKey(item: ExtractedItem): item is ExtractedItem & { key: string } {
  return typeof item.key === "string" && item.key.length > 0 && !/^item-\d+$/.test(item.key);
}

export function ensureReceiptItemKeys(items: ExtractedItem[]): ExtractedItem[] {
  const occurrences = new Map<string, number>();
  return items.map((item) => {
    if (hasSourceKey(item)) return item;
    const identity = sourceIdentity(item);
    const occurrence = occurrences.get(identity) ?? 0;
    occurrences.set(identity, occurrence + 1);
    return { ...item, key: `source:${encodeURIComponent(identity)}:${occurrence}` };
  });
}

export function createManualReceiptItem(key: string): ExtractedItem {
  return {
    key,
    name: "",
    printedAmount: 0,
    grossAmount: 0,
    confidence: 0,
    verification: "MANUAL",
  };
}

export function addManualReceiptItem(items: ExtractedItem[], key: string): ExtractedItem[] {
  return [...ensureReceiptItemKeys(items), createManualReceiptItem(key)];
}

export function deleteReceiptItemByKey(items: ExtractedItem[], itemKey: string): ExtractedItem[] {
  return ensureReceiptItemKeys(items).filter((item) => item.key !== itemKey);
}

export function restoreDeletedReceiptItem(
  items: ExtractedItem[],
  confirmationState: ReceiptConfirmationState,
  undo: ReceiptItemDeletionUndo,
): { items: ExtractedItem[]; confirmationState: ReceiptConfirmationState } | undefined {
  const keyedItems = ensureReceiptItemKeys(items);
  if (
    undo.item.key === undefined ||
    keyedItems.some((item) => item.key === undo.item.key) ||
    keyedItems.length !== undo.remainingItemKeys.length ||
    keyedItems.some((item, index) => item.key !== undo.remainingItemKeys[index])
  ) {
    return undefined;
  }

  const restoredItems = [...keyedItems];
  restoredItems.splice(Math.min(undo.index, restoredItems.length), 0, undo.item);
  const restoreItemValue = <T>(
    current: Record<string, T> | undefined,
    previous: Record<string, T> | undefined,
  ) => {
    const value = previous?.[undo.item.key!];
    if (value === undefined) return current;
    return { ...current, [undo.item.key!]: value };
  };
  const amountDrafts = restoreItemValue(
    confirmationState.amountDrafts,
    undo.confirmationState.amountDrafts,
  );
  const manualAmountOverrides = restoreItemValue(
    confirmationState.manualAmountOverrides,
    undo.confirmationState.manualAmountOverrides,
  );
  const itemAllocations = restoreItemValue(
    confirmationState.itemAllocations,
    undo.confirmationState.itemAllocations,
  );
  const receiptItemReviewStates = restoreItemValue(
    confirmationState.receiptItemReviewStates,
    undo.confirmationState.receiptItemReviewStates,
  );
  const restoredState: ReceiptConfirmationState = {
    ...confirmationState,
    ...(confirmationState.selectedCandidate || !undo.confirmationState.selectedCandidate
      ? {}
      : { selectedCandidate: undo.confirmationState.selectedCandidate }),
    ...(confirmationState.manualAmountOverrideReason !== undefined ||
    undo.confirmationState.manualAmountOverrideReason === undefined
      ? {}
      : { manualAmountOverrideReason: undo.confirmationState.manualAmountOverrideReason }),
    ...(amountDrafts ? { amountDrafts } : {}),
    ...(manualAmountOverrides ? { manualAmountOverrides } : {}),
    ...(itemAllocations ? { itemAllocations } : {}),
    ...(receiptItemReviewStates ? { receiptItemReviewStates } : {}),
  };
  return { items: restoredItems, confirmationState: restoredState };
}

export function restoreReceiptItemDeletion(
  document: DocumentReview,
  undo: ReceiptItemDeletionUndo,
): DocumentReview | undefined {
  const currentResult = document.result;
  const currentItems = currentResult?.receipt?.items;
  if (document.id !== undo.documentId || !currentResult?.receipt || !currentItems) return undefined;
  const restored = restoreDeletedReceiptItem(
    currentItems,
    document.receiptConfirmationState ?? {},
    undo,
  );
  if (!restored) return undefined;

  const candidate = undo.confirmationState.selectedCandidate;
  const currentCandidate = currentResult.candidates?.find((entry) => entry.id === candidate?.id);
  const changedRemainingItems = undo.remainingItems.some(
    (item, index) =>
      JSON.stringify(item) !== JSON.stringify(ensureReceiptItemKeys(currentItems)[index]),
  );
  const candidateIsCurrent = Boolean(
    candidate &&
    !changedRemainingItems &&
    currentCandidate &&
    currentCandidate.computedTotal === candidate.computedTotal &&
    currentCandidate.consistent === candidate.consistent &&
    currentCandidate.amounts.length === candidate.amounts.length &&
    currentCandidate.amounts.every((amount, index) => amount === candidate.amounts[index]) &&
    currentCandidate.issues.length === candidate.issues.length &&
    currentCandidate.issues.every((issue, index) => issue === candidate.issues[index]),
  );
  const { selectedCandidate: _candidate, ...stateWithoutCandidate } = restored.confirmationState;
  const confirmationState = candidateIsCurrent
    ? { ...stateWithoutCandidate, selectedCandidate: currentCandidate }
    : stateWithoutCandidate;
  const {
    balanced: _balanced,
    reviewReceiptItemsChanged: _reviewReceiptItemsChanged,
    ...resultWithoutPreviousStructureFlags
  } = currentResult as typeof currentResult & { reviewReceiptItemsChanged?: boolean };
  const result = {
    ...resultWithoutPreviousStructureFlags,
    ...(changedRemainingItems
      ? { balanced: false, reviewReceiptItemsChanged: true }
      : {
          ...(undo.balanced !== undefined ? { balanced: undo.balanced } : {}),
          ...(undo.reviewReceiptItemsChanged ? { reviewReceiptItemsChanged: true } : {}),
        }),
    receipt: { ...currentResult.receipt, items: restored.items },
  };
  return {
    ...document,
    result,
    receiptConfirmationState: Object.keys(confirmationState).length ? confirmationState : undefined,
  };
}

export function rekeyReceiptItemValues<T>(
  values: Record<string, T> | undefined,
  originalItems: ExtractedItem[],
  keyedItems: ExtractedItem[],
  survivingKeys: Set<string>,
): Record<string, T> | undefined {
  if (!values) return undefined;
  const originalKeyedItems = ensureReceiptItemKeys(originalItems);
  const keyedByOriginalKey = new Map(
    originalKeyedItems.map((item, index) => [item.key, keyedItems[index]]),
  );
  const rekeyed: Record<string, T> = {};
  for (const [key, value] of Object.entries(values)) {
    const legacy = /^item-(\d+)$/.exec(key);
    const currentItem = legacy
      ? keyedItems[Number(legacy[1])]
      : (keyedByOriginalKey.get(key) ?? keyedItems.find((item) => item.key === key));
    if (currentItem?.key && survivingKeys.has(currentItem.key)) {
      rekeyed[currentItem.key] = value;
    }
  }
  return rekeyed;
}

export function rekeyReceiptConfirmationAfterStructureChange(
  state: ReceiptConfirmationState,
  originalItems: ExtractedItem[],
  keyedItems: ExtractedItem[],
  nextItems: ExtractedItem[],
): ReceiptConfirmationState {
  const survivingKeys = new Set(nextItems.flatMap((item) => (item.key ? [item.key] : [])));
  const manualAmountOverrides = rekeyReceiptItemValues(
    state.manualAmountOverrides,
    originalItems,
    keyedItems,
    survivingKeys,
  );
  const amountDrafts = rekeyReceiptItemValues(
    state.amountDrafts,
    originalItems,
    keyedItems,
    survivingKeys,
  );
  const itemAllocations = rekeyReceiptItemValues(
    state.itemAllocations,
    originalItems,
    keyedItems,
    survivingKeys,
  );
  const receiptItemReviewStates = rekeyReceiptItemValues(
    state.receiptItemReviewStates,
    originalItems,
    keyedItems,
    survivingKeys,
  );
  const {
    selectedCandidate: _selectedCandidate,
    amountDrafts: _amountDrafts,
    manualAmountOverrides: _manualAmountOverrides,
    itemAllocations: _itemAllocations,
    manualAmountOverrideReason: _manualAmountOverrideReason,
    receiptItemReviewStates: _receiptItemReviewStates,
    ...rest
  } = state;
  return {
    ...rest,
    ...(amountDrafts && Object.keys(amountDrafts).length ? { amountDrafts } : {}),
    ...(manualAmountOverrides && Object.keys(manualAmountOverrides).length
      ? { manualAmountOverrides }
      : {}),
    ...(itemAllocations && Object.keys(itemAllocations).length ? { itemAllocations } : {}),
    ...(receiptItemReviewStates && Object.keys(receiptItemReviewStates).length
      ? { receiptItemReviewStates }
      : {}),
  };
}

function sourceKey(key: string | undefined): boolean {
  return Boolean(key?.startsWith("source:"));
}

function uniqueSourceMatch(item: ExtractedItem, values: ExtractedItem[]): number[] {
  const identity = sourceIdentity(item);
  return values.flatMap((candidate, index) =>
    sourceIdentity(candidate) === identity ? [index] : [],
  );
}

function matchItem(
  item: ExtractedItem,
  values: ExtractedItem[],
  allowSourceOccurrence = false,
): number[] {
  if (item.key && !sourceKey(item.key))
    return values.flatMap((candidate, index) => (candidate.key === item.key ? [index] : []));
  if (item.key) {
    const exactKey = values.flatMap((candidate, index) =>
      candidate.key === item.key ? [index] : [],
    );
    if (exactKey.length && allowSourceOccurrence) return exactKey;
  }
  const byIdentity = uniqueSourceMatch(item, values);
  if (byIdentity.length) return byIdentity;
  const regionIdentity = sourceRegionIdentity(item);
  if (!regionIdentity) return [];
  return values.flatMap((candidate, index) =>
    sourceRegionIdentity(candidate) === regionIdentity ? [index] : [],
  );
}

function sameExceptKey(left: ExtractedItem, right: ExtractedItem): boolean {
  const { key: _leftKey, ...leftFields } = left;
  const { key: _rightKey, ...rightFields } = right;
  return equal(leftFields, rightFields);
}

function mergeEdits(
  base: unknown,
  edited: unknown,
  latest: unknown,
  conflicts: string[],
  path = "",
): unknown {
  if (equal(base, edited)) return latest;
  if (Array.isArray(base) && Array.isArray(edited)) {
    const isReceiptItems = path === "receipt.items" || path.endsWith(".receipt.items");
    if (isReceiptItems) {
      return mergeReceiptItems(base, edited, latest, conflicts);
    }
    const merged = Array.isArray(latest) ? [...latest] : [];
    for (let index = 0; index < base.length; index += 1) {
      if (index >= edited.length) {
        merged.splice(index, 1);
        continue;
      }
      merged[index] = mergeEdits(
        base[index],
        edited[index],
        merged[index],
        conflicts,
        `${path}[${index}]`,
      );
    }
    for (let index = base.length; index < edited.length; index += 1) {
      merged[index] = edited[index];
    }
    return merged;
  }
  if (isRecord(base) && isRecord(edited)) {
    const merged: Record<string, unknown> = isRecord(latest) ? { ...latest } : {};
    for (const key of new Set([...Object.keys(base), ...Object.keys(edited)])) {
      if (!Object.hasOwn(edited, key)) {
        delete merged[key];
      } else if (!Object.hasOwn(base, key)) {
        merged[key] = edited[key];
      } else {
        merged[key] = mergeEdits(
          base[key],
          edited[key],
          merged[key],
          conflicts,
          path ? `${path}.${key}` : key,
        );
      }
    }
    return merged;
  }
  return edited;
}

function mergeReceiptItems(
  baseValue: unknown[],
  editedValue: unknown[],
  latestValue: unknown,
  conflicts: string[],
): unknown[] {
  if (!baseValue.every(isReceiptItem) || !editedValue.every(isReceiptItem)) {
    conflicts.push("品目");
    return Array.isArray(latestValue) ? latestValue : [];
  }
  const baseItems = ensureReceiptItemKeys(baseValue);
  const editedItems = ensureReceiptItemKeys(editedValue);
  const latestItems =
    Array.isArray(latestValue) && latestValue.every(isReceiptItem)
      ? ensureReceiptItemKeys(latestValue)
      : [];
  const output: Array<ExtractedItem | undefined> = [...latestItems];
  const usedEdited = new Set<number>();

  for (let baseIndex = 0; baseIndex < baseItems.length; baseIndex += 1) {
    const baseItem = baseItems[baseIndex];
    let editedMatches = matchItem(baseItem, editedItems, true).filter(
      (index) => !usedEdited.has(index),
    );
    if (editedMatches.length === 0) {
      const sameAmountBase = baseItems.filter(
        (item) => item.printedAmount === baseItem.printedAmount,
      );
      const legacyIndexes = new Set(
        editedValue.flatMap((value, index) =>
          isReceiptItem(value) && !hasSourceKey(value) ? [index] : [],
        ),
      );
      const sameAmountEdited = editedItems.filter(
        (item, index) =>
          legacyIndexes.has(index) &&
          !usedEdited.has(index) &&
          item.printedAmount === baseItem.printedAmount,
      );
      if (sameAmountBase.length === 1 && sameAmountEdited.length === 1) {
        editedMatches = [editedItems.indexOf(sameAmountEdited[0])];
      }
    }
    if (editedMatches.length > 1) {
      conflicts.push(`品目${baseIndex + 1}`);
      continue;
    }
    if (editedMatches.length === 0) {
      const latestMatches = matchItem(baseItem, latestItems);
      if (latestMatches.length !== 1) {
        conflicts.push(`品目${baseIndex + 1}`);
        continue;
      }
      const latestIndex = latestMatches[0];
      output[latestIndex] = undefined;
      continue;
    }
    const editedIndex = editedMatches[0];
    usedEdited.add(editedIndex);
    const editedItem = editedItems[editedIndex];
    if (sameExceptKey(baseItem, editedItem)) continue;
    const latestMatches = matchItem(baseItem, latestItems);
    if (latestMatches.length !== 1) {
      conflicts.push(`品目${baseIndex + 1}`);
      continue;
    }
    const latestIndex = latestMatches[0];
    const mergedItem = mergeEdits(
      baseItem,
      editedItem,
      latestItems[latestIndex],
      conflicts,
      `receipt.items[${baseIndex}]`,
    );
    if (!isReceiptItem(mergedItem)) {
      conflicts.push(`品目${baseIndex + 1}`);
      continue;
    }
    output[latestIndex] = mergedItem;
  }

  for (let editedIndex = 0; editedIndex < editedItems.length; editedIndex += 1) {
    if (usedEdited.has(editedIndex)) continue;
    const item = editedItems[editedIndex];
    if (baseItems.some((baseItem) => baseItem.key === item.key)) continue;
    const { region: _region, ...manualItem } = item;
    output.push(manualItem);
  }
  return output.flatMap((item) => (item ? [item] : []));
}

export function reconcileReceiptReviewDraft(
  document: DocumentReview,
  draft: ImportReceiptDraft,
): { document: DocumentReview; conflicts: string[] } {
  if (!document.result) return { document, conflicts: [] };
  const conflicts: string[] = [];
  const result = mergeEdits(draft.baseResult, draft.editedResult, document.result, conflicts);
  if (!isDocumentResult(result)) {
    conflicts.push("解析結果");
    return { document, conflicts };
  }
  return { document: { ...document, result }, conflicts };
}

export function reconcileReceiptConfirmationState(
  baseResult: ImportReceiptDraft["baseResult"],
  latestResult: DocumentReview["result"],
  state: ReceiptConfirmationState | undefined,
): { state: ReceiptConfirmationState | undefined; conflicts: string[] } {
  if (!state) return { state, conflicts: [] };
  const conflicts: string[] = [];
  const baseItems = ensureReceiptItemKeys(receiptItems(baseResult));
  const latestItems = ensureReceiptItemKeys(receiptItems(latestResult));
  const mapItemKeys = <T>(values: Record<string, T> | undefined) => {
    if (!values) return undefined;
    const mapped: Record<string, T> = {};
    for (const [key, value] of Object.entries(values)) {
      if (key.startsWith("manual:")) {
        mapped[key] = value;
        continue;
      }
      const legacyIndex = /^item-(\d+)$/.exec(key);
      const baseItem = legacyIndex
        ? baseItems[Number(legacyIndex[1])]
        : baseItems.find((item) => item.key === key);
      if (!baseItem) continue;
      const matches = matchItem(baseItem, latestItems);
      if (matches.length !== 1) {
        conflicts.push(`品目${baseItems.indexOf(baseItem) + 1}の確認状態`);
        continue;
      }
      const target = latestItems[matches[0]];
      if (target.key) mapped[target.key] = value;
    }
    return mapped;
  };

  let selectedCandidate = state.selectedCandidate;
  if (selectedCandidate) {
    const candidates = latestResult?.candidates ?? [];
    const remappedAmounts =
      selectedCandidate.amounts.length === baseItems.length
        ? selectedCandidate.amounts.map((amount, baseIndex) => {
            const baseItem = baseItems[baseIndex];
            const indexes = matchItem(baseItem, latestItems);
            return indexes.length === 1 ? { index: indexes[0], amount } : undefined;
          })
        : [];
    const amountsInLatestOrder =
      remappedAmounts.length === latestItems.length && remappedAmounts.every(Boolean)
        ? latestItems.map(
            (_, index) => remappedAmounts.find((entry) => entry?.index === index)?.amount,
          )
        : undefined;
    const matches = amountsInLatestOrder
      ? candidates.filter(
          (candidate) =>
            candidate.computedTotal === selectedCandidate?.computedTotal &&
            candidate.amounts.length === amountsInLatestOrder.length &&
            candidate.amounts.every((amount, index) => amount === amountsInLatestOrder[index]),
        )
      : [];
    const candidate = matches.length === 1 ? matches[0] : undefined;
    if (candidate && candidate.amounts.length === latestItems.length) {
      selectedCandidate = { ...candidate };
    } else {
      conflicts.push("税額候補の選択");
      selectedCandidate = undefined;
    }
  }

  return {
    state: {
      ...(state.amountDrafts ? { amountDrafts: mapItemKeys(state.amountDrafts) } : {}),
      ...(state.purchaseDate !== undefined ? { purchaseDate: state.purchaseDate } : {}),
      ...(state.purchaseTime !== undefined ? { purchaseTime: state.purchaseTime } : {}),
      ...(selectedCandidate ? { selectedCandidate } : {}),
      ...(state.manualAmountOverrides
        ? { manualAmountOverrides: mapItemKeys(state.manualAmountOverrides) }
        : {}),
      ...(state.manualAmountOverrideReason !== undefined
        ? { manualAmountOverrideReason: state.manualAmountOverrideReason }
        : {}),
      ...(state.payments ? { payments: state.payments } : {}),
      ...(state.itemAllocations ? { itemAllocations: mapItemKeys(state.itemAllocations) } : {}),
      ...(state.receiptItemReviewStates
        ? { receiptItemReviewStates: mapItemKeys(state.receiptItemReviewStates) }
        : {}),
    },
    conflicts,
  };
}

function receiptItems(value: unknown) {
  return isRecord(value) && isRecord(value.receipt) && Array.isArray(value.receipt.items)
    ? value.receipt.items.filter(isReceiptItem)
    : [];
}

function isDocumentResult(value: unknown): value is NonNullable<DocumentReview["result"]> {
  if (!isRecord(value)) return false;
  if (value.receipt !== undefined && !isRecord(value.receipt)) return false;
  if (
    isRecord(value.receipt) &&
    value.receipt.items !== undefined &&
    (!Array.isArray(value.receipt.items) || !value.receipt.items.every(isReceiptItem))
  )
    return false;
  return true;
}

export function createImportReceiptDraft(
  householdId: string,
  documentId: string,
  baseResult: DocumentReview["result"],
  editedResult: DocumentReview["result"],
  confirmationState?: ReceiptConfirmationState,
): ImportReceiptDraft | undefined {
  if (!baseResult || !editedResult) return undefined;
  return {
    householdId,
    documentId,
    baseResult,
    editedResult,
    ...(confirmationState ? { confirmationState } : {}),
  };
}
