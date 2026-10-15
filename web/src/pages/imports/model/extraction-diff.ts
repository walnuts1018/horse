import type { DocumentReview } from "./imports";

export type ReceiptExtractionSnapshot = {
  jobId: string;
  nextJobId: string;
  receipt: NonNullable<DocumentReview["result"]>["receipt"];
};

export type ExtractionFieldChange = {
  label: string;
  before: string;
  after: string;
};

export type ExtractionDiff = {
  receiptChanges: ExtractionFieldChange[];
  itemChanges: Array<{ label: string; changes: ExtractionFieldChange[] }>;
  taxChanges: Array<{ label: string; changes: ExtractionFieldChange[] }>;
  paymentChanges: Array<{ label: string; changes: ExtractionFieldChange[] }>;
};

function value(raw: unknown): string {
  if (typeof raw === "string" || typeof raw === "number") return String(raw);
  return "未設定";
}

function yen(amount: unknown): string {
  return typeof amount !== "number" ? "未設定" : `${amount.toLocaleString("ja-JP")}円`;
}

function fields<T extends Record<string, unknown>>(
  before: T | undefined,
  after: T | undefined,
  definitions: Array<{ key: keyof T; label: string; format?: (value: unknown) => string }>,
): ExtractionFieldChange[] {
  return definitions.flatMap(({ key, label, format }) => {
    const beforeValue = before?.[key];
    const afterValue = after?.[key];
    const beforeText = format ? format(beforeValue) : value(beforeValue);
    const afterText = format ? format(afterValue) : value(afterValue);
    return beforeText === afterText ? [] : [{ label, before: beforeText, after: afterText }];
  });
}

function changedRows<T extends Record<string, unknown>>(
  before: T[] | undefined,
  after: T[] | undefined,
  label: (entry: T, index: number) => string,
  definitions: Array<{ key: keyof T; label: string; format?: (value: unknown) => string }>,
) {
  const rows = [];
  for (let index = 0; index < Math.max(before?.length ?? 0, after?.length ?? 0); index += 1) {
    const previous = before?.[index];
    const next = after?.[index];
    const changes = fields(previous, next, definitions);
    if (changes.length) rows.push({ label: label(next ?? previous!, index), changes });
  }
  return rows;
}

const yenField = (amount: unknown) => yen(amount);

export function compareReceiptExtractions(
  before: ReceiptExtractionSnapshot["receipt"],
  after: ReceiptExtractionSnapshot["receipt"],
): ExtractionDiff {
  if (!before || !after) {
    return { receiptChanges: [], itemChanges: [], taxChanges: [], paymentChanges: [] };
  }
  const itemsBefore = before.items ?? [];
  const itemsAfter = after.items ?? [];
  const taxesBefore = before.taxGroups ?? [];
  const taxesAfter = after.taxGroups ?? [];
  const paymentsBefore = before.payments ?? [];
  const paymentsAfter = after.payments ?? [];

  return {
    receiptChanges: fields({ total: before.total }, { total: after.total }, [
      { key: "total", label: "合計", format: yenField },
    ]),
    itemChanges: changedRows(
      itemsBefore,
      itemsAfter,
      (item, index) => `明細${index + 1}・${item.name || item.printedName || "品目"}`,
      [
        { key: "name", label: "品目名" },
        { key: "grossAmount", label: "金額", format: yenField },
        {
          key: "categoryHint",
          label: "カテゴリ",
        },
        { key: "taxGroup", label: "税区分" },
      ],
    ),
    taxChanges: changedRows(
      taxesBefore,
      taxesAfter,
      (tax, index) => `税${index + 1}・${tax.id || "区分"}`,
      [
        { key: "numerator", label: "税率分子" },
        { key: "denominator", label: "税率分母" },
        { key: "basis", label: "計算対象" },
        { key: "rounding", label: "端数処理" },
        { key: "printedSubtotal", label: "印字小計", format: yenField },
        { key: "printedTax", label: "印字税額", format: yenField },
      ],
    ),
    paymentChanges: changedRows(
      paymentsBefore,
      paymentsAfter,
      (payment, index) => `支払${index + 1}・${payment.method || "方法"}`,
      [
        { key: "method", label: "支払方法" },
        { key: "cardLast4", label: "カード番号下4桁" },
        { key: "amount", label: "支払額", format: yenField },
      ],
    ),
  };
}

export function readReceiptExtractionSnapshot(
  documentId: string,
): ReceiptExtractionSnapshot | null {
  try {
    const stored: unknown = JSON.parse(
      sessionStorage.getItem(`document-extraction:${documentId}`) ?? "null",
    );
    return isReceiptExtractionSnapshot(stored) ? stored : null;
  } catch {
    return null;
  }
}

function isReceiptExtractionSnapshot(raw: unknown): raw is ReceiptExtractionSnapshot {
  if (typeof raw !== "object" || raw === null) return false;
  if (!("jobId" in raw) || typeof raw.jobId !== "string") return false;
  if (!("nextJobId" in raw) || typeof raw.nextJobId !== "string") return false;
  return "receipt" in raw && typeof raw.receipt === "object" && raw.receipt !== null;
}

export function storeReceiptExtractionSnapshot(
  documentId: string,
  snapshot: ReceiptExtractionSnapshot,
) {
  try {
    sessionStorage.setItem(`document-extraction:${documentId}`, JSON.stringify(snapshot));
  } catch {
    return;
  }
}
