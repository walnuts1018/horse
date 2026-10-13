import type { Region } from "./evidence-geometry";

export type ExtractedItem = {
  name: string;
  printedAmount: number;
  grossAmount: number;
  categoryId?: string | null;
  quantityNumerator?: string | null;
  quantityDenominator?: string | null;
  quantityUnit?: string | null;
  printedUnitPriceYen?: number | null;
  printedUnitPriceQuantityNumerator?: string | null;
  printedUnitPriceQuantityDenominator?: string | null;
  printedUnitPriceUnit?: string | null;
  productVariantId?: string | null;
  confidence: number;
  verification: string;
  categoryHint?: string;
  region?: Region;
};

export type MatchCandidate = {
  documentId: string;
  sourceReference: string;
  state: string;
  reasons: string[];
};
export type ObservationCandidates = { sourceReference: string; matches: MatchCandidate[] };
export type StatementEntry = {
  row: number;
  sourceReference?: string;
  kind: string;
  occurredAt: string;
  merchant: string;
  amount: number;
  transactionId?: string;
  orderId?: string;
  cardLast4?: string;
};

export function receiptDateInputValue(purchasedAt?: string): string {
  if (!purchasedAt) return "";
  const date = new Date(purchasedAt);
  if (Number.isNaN(date.getTime())) return "";
  const parts = new Intl.DateTimeFormat("en", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(date);
  const value = Object.fromEntries(parts.map((part) => [part.type, part.value]));
  return `${value.year}-${value.month}-${value.day}`;
}

export function receiptOccurredAt(purchasedAt?: string, manualDate?: string): string | null {
  if (manualDate !== undefined) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(manualDate)) return null;
    const calendarDate = new Date(`${manualDate}T00:00:00.000Z`);
    if (
      Number.isNaN(calendarDate.getTime()) ||
      calendarDate.toISOString().slice(0, 10) !== manualDate
    ) {
      return null;
    }
    return new Date(`${manualDate}T12:00:00+09:00`).toISOString();
  }
  if (!purchasedAt) return null;
  const date = new Date(purchasedAt);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

export function statementConfirmationIssue(entry: StatementEntry): string | undefined {
  if (entry.kind !== "CARD_PURCHASE") return undefined;
  if (!Number.isSafeInteger(entry.amount) || entry.amount <= 0) {
    return entry.amount < 0 ? "返品・返金は確定できません" : "明細金額を確認してください";
  }
  if (!entry.occurredAt || Number.isNaN(new Date(entry.occurredAt).getTime())) {
    return "購入日を確認してください";
  }
  return undefined;
}

export function statementSourceReference(entry: StatementEntry): string {
  return entry.sourceReference || `row:${entry.row}`;
}

export function persistedStatementConfirmationKeys(document?: DocumentReview): string[] {
  if (!document) return [];
  return (document.confirmations ?? []).map(
    (confirmation) => `${document.id}:${confirmation.sourceReference}`,
  );
}

export type ReceiptPayment = { method: string; cardLast4?: string; amount: number };
export type VerificationCandidate = {
  id: string;
  amounts: number[];
  computedTotal: number;
  consistent: boolean;
  issues: string[];
};
export type DocumentReview = {
  id: string;
  jobId?: string;
  name?: string;
  attachments?: Array<{
    id: string;
    name: string;
    filename: string;
    mimeType: string;
    status?: string;
    jobId?: string;
  }>;
  attachmentParent?: {
    id: string;
    name: string;
    filename: string;
    index: number;
  };
  status: string;
  sourceKind?: string;
  mimeType?: string;
  createdAt?: string;
  sha256?: string;
  duplicateOf?: string;
  confirmations?: Array<{ sourceReference: string; purchaseId: string }>;
  result?: {
    receipt?: {
      merchant?: string;
      purchasedAt?: string;
      items?: ExtractedItem[];
      payments?: ReceiptPayment[];
    };
    computedTotal?: number;
    balanced?: boolean;
    candidates?: VerificationCandidate[];
    issues?: string[];
    transform?: number[];
    transformPage?: number;
    ocrLines?: Array<{ text: string; confidence: number; region?: Region }>;
    statements?: StatementEntry[];
  };
};
export type PendingUpload = {
  id: string;
  file: File;
  status: "queued" | "uploading" | "failed";
  error?: string;
};
export type DocumentFilters = {
  store: string;
  from: string;
  to: string;
  sourceKind: string;
  confirmed: string;
};

export const defaultDocumentFilters: DocumentFilters = {
  store: "",
  from: "",
  to: "",
  sourceKind: "",
  confirmed: "",
};

export function parseDocumentFilters(search: URLSearchParams): DocumentFilters {
  const sourceKind = search.get("sourceKind") ?? "";
  const confirmed = search.get("confirmed") ?? "";
  return {
    store: search.get("store") ?? "",
    from: search.get("from") ?? "",
    to: search.get("to") ?? "",
    sourceKind: ["receipt", "order", "email", "card", "bank"].includes(sourceKind)
      ? sourceKind
      : "",
    confirmed: confirmed === "true" || confirmed === "false" ? confirmed : "",
  };
}

export function serializeDocumentFilters(filters: DocumentFilters, search: URLSearchParams) {
  const query = new URLSearchParams(search);
  for (const [key, value] of Object.entries(filters)) {
    if (value) query.set(key, value);
    else query.delete(key);
  }
  return query;
}
export type ReceiptPaymentDraft = {
  key: string;
  method?: string;
  cardLast4?: string;
  paymentAccountId: string;
  amountYen: number;
};
export type ReviewTarget = { key: string; label: string; elementId: string };

const terminalStatuses = new Set([
  "READY",
  "REVIEW",
  "COMPLETED",
  "SUCCEEDED",
  "FAILED",
  "DUPLICATE",
  "DONE",
]);

export const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

export const isCsvFile = (file: File) => /\.csv$/i.test(file.name) || file.type === "text/csv";

export const sourceKind = (file: File, csvKind: "card" | "bank") =>
  isCsvFile(file) ? csvKind : /\.eml$/i.test(file.name) ? "email" : "receipt";

export const isSupportedFile = (file: File) =>
  /\.(jpe?g|png|webp|pdf|csv|eml)$/i.test(file.name) ||
  [
    "image/jpeg",
    "image/png",
    "image/webp",
    "application/pdf",
    "text/csv",
    "message/rfc822",
  ].includes(file.type);

export const statusLabel = (status: string) =>
  (
    ({
      QUEUED: "待機中",
      RUNNING: "抽出中",
      READY: "確認待ち",
      REVIEW: "確認待ち",
      COMPLETED: "抽出済み",
      SUCCEEDED: "抽出済み",
      FAILED: "失敗",
      DUPLICATE: "重複",
      DONE: "抽出済み",
    }) as Record<string, string>
  )[status.toUpperCase()] ?? "状態を確認中";

export const sourceKindLabel = (kind?: string) =>
  (
    ({
      receipt: "レシート",
      order: "注文履歴",
      email: "メール",
      card: "カード明細",
      bank: "銀行明細",
    }) as Record<string, string>
  )[kind ?? ""] ?? "証拠";

export const isProcessing = (status: string) => !terminalStatuses.has(status.toUpperCase());
