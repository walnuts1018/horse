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
  kind: string;
  occurredAt: string;
  merchant: string;
  amount: number;
  transactionId?: string;
  orderId?: string;
  cardLast4?: string;
};
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
