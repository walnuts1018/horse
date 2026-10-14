import type { ImportReceiptDraft } from "../../../shared/lib/purchase-storage";
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
): ImportReceiptDraft | undefined {
  if (!baseResult || !editedResult) return undefined;
  return {
    householdId,
    documentId,
    baseResult,
    editedResult,
  };
}
