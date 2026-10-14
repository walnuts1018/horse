import type {
  ImportReceiptConfirmationState,
  ImportReceiptDraft,
} from "../../../shared/lib/purchase-storage";
import type { DocumentReview } from "./imports";

import { isRecord } from "../../../shared/lib/guard";

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

function isReceiptItem(value: unknown): value is Record<string, unknown> {
  return (
    isRecord(value) &&
    typeof value.name === "string" &&
    typeof value.printedAmount === "number" &&
    typeof value.grossAmount === "number"
  );
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
    const merged = Array.isArray(latest) ? [...latest] : [];
    const baseItems = base.filter(isReceiptItem);
    const editedItems = edited.filter(isReceiptItem);
    const latestItems = Array.isArray(latest) ? latest.filter(isReceiptItem) : [];
    if (
      (path === "receipt.items" || path.endsWith(".receipt.items")) &&
      baseItems.length === base.length &&
      editedItems.length === edited.length &&
      Array.isArray(latest) &&
      latestItems.length === latest.length
    ) {
      for (let index = 0; index < baseItems.length; index += 1) {
        if (equal(baseItems[index], editedItems[index])) continue;
        const original = baseItems[index];
        const matches = latestItems.flatMap((item, latestIndex) =>
          item.printedAmount === original.printedAmount ? [latestIndex] : [],
        );
        if (matches.length !== 1) {
          conflicts.push(`品目${index + 1}`);
          continue;
        }
        const latestIndex = matches[0];
        merged[latestIndex] = mergeEdits(
          original,
          editedItems[index],
          latestItems[latestIndex],
          conflicts,
          `${path}[${index}]`,
        );
      }
      return merged;
    }
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
  state: ImportReceiptConfirmationState | undefined,
): { state: ImportReceiptConfirmationState | undefined; conflicts: string[] } {
  if (!state) return { state, conflicts: [] };
  const conflicts: string[] = [];
  const baseItems = receiptItems(baseResult);
  const latestItems = receiptItems(latestResult);
  const mapItemKeys = <T>(values: Record<string, T> | undefined) => {
    if (!values) return undefined;
    const mapped: Record<string, T> = {};
    for (const [key, value] of Object.entries(values)) {
      const match = /^item-(\d+)$/.exec(key);
      const baseItem = match ? baseItems[Number(match[1])] : undefined;
      if (!baseItem) continue;
      const matches = latestItems.flatMap((item, index) =>
        item.printedAmount === baseItem.printedAmount ? [index] : [],
      );
      const matchedIndex = matches.at(0);
      if (matches.length !== 1 || matchedIndex === undefined) {
        conflicts.push(`品目${Number(match?.[1] ?? 0) + 1}の確認状態`);
        continue;
      }
      const targetItem = latestItems[matchedIndex];
      const targetKey =
        typeof targetItem.key === "string" ? targetItem.key : `item-${matchedIndex}`;
      mapped[targetKey] = value;
    }
    return mapped;
  };
  let selectedCandidate = state.selectedCandidate;
  if (selectedCandidate) {
    const candidates = latestResult?.candidates ?? [];
    const remappedAmounts = selectedCandidate.amounts.map((amount, baseIndex) => {
      const baseItem = baseItems[baseIndex];
      if (!baseItem) return undefined;
      const indexes = latestItems.flatMap((item, index) =>
        item.printedAmount === baseItem.printedAmount ? [index] : [],
      );
      return indexes.length === 1 ? { index: indexes[0], amount } : undefined;
    });
    const amountsInLatestOrder =
      remappedAmounts.length === latestItems.length && remappedAmounts.every(Boolean)
        ? latestItems.map(
            (_, index) => remappedAmounts.find((entry) => entry?.index === index)?.amount,
          )
        : selectedCandidate.amounts;
    const matches = candidates.filter(
      (candidate) =>
        candidate.computedTotal === selectedCandidate?.computedTotal &&
        candidate.amounts.length === amountsInLatestOrder.length &&
        candidate.amounts.every((amount, index) => amount === amountsInLatestOrder[index]),
    );
    const sameID = candidates.filter((candidate) => candidate.id === selectedCandidate?.id);
    const candidate =
      matches.length === 1 ? matches[0] : sameID.length === 1 ? sameID[0] : undefined;
    if (candidate) selectedCandidate = { ...candidate };
    else {
      conflicts.push("税額候補の選択");
      selectedCandidate = undefined;
    }
  }
  return {
    state: {
      ...(selectedCandidate ? { selectedCandidate } : {}),
      ...(state.manualAmountOverrides
        ? { manualAmountOverrides: mapItemKeys(state.manualAmountOverrides) }
        : {}),
      ...(state.manualAmountOverrideReason !== undefined
        ? { manualAmountOverrideReason: state.manualAmountOverrideReason }
        : {}),
      ...(state.payments ? { payments: state.payments } : {}),
      ...(state.itemAllocations ? { itemAllocations: mapItemKeys(state.itemAllocations) } : {}),
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
  confirmationState?: ImportReceiptConfirmationState,
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
