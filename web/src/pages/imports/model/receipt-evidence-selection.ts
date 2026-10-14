export type ReceiptEvidenceSelection = {
  itemKey: string;
  index: number;
  focusEditor: boolean;
};

const selections = new Map<string, ReceiptEvidenceSelection>();
const listeners = new Map<string, Set<() => void>>();

export function getReceiptEvidenceSelection(documentId: string) {
  return selections.get(documentId) ?? null;
}

export function subscribeReceiptEvidenceSelection(documentId: string, listener: () => void) {
  const documentListeners = listeners.get(documentId) ?? new Set<() => void>();
  documentListeners.add(listener);
  listeners.set(documentId, documentListeners);
  return () => {
    documentListeners.delete(listener);
    if (documentListeners.size === 0) listeners.delete(documentId);
  };
}

export function selectReceiptEvidenceItem(documentId: string, selection: ReceiptEvidenceSelection) {
  const current = selections.get(documentId);
  if (
    current?.itemKey === selection.itemKey &&
    current.index === selection.index &&
    current.focusEditor === selection.focusEditor
  )
    return;
  selections.set(documentId, selection);
  listeners.get(documentId)?.forEach((listener) => listener());
}

export function receiptEvidenceSelectionIndex(
  selection: ReceiptEvidenceSelection | null,
  itemKeys: string[],
  fallbackIndex: number,
) {
  if (selection) {
    const matchingIndex = itemKeys.indexOf(selection.itemKey);
    if (matchingIndex >= 0) return matchingIndex;
  }
  if (itemKeys.length === 0) return 0;
  const requestedIndex = selection?.index ?? fallbackIndex;
  return Math.max(0, Math.min(itemKeys.length - 1, Math.trunc(requestedIndex)));
}
