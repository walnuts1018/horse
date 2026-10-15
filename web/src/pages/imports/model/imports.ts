import type { ImportReceiptConfirmationState } from "../../../shared/lib/purchase-storage";
import type { Region } from "./evidence-geometry";

export type ReceiptConfirmationState = ImportReceiptConfirmationState & {
  amountDrafts?: Record<string, string>;
  purchaseDate?: string;
  purchaseTime?: string;
};

export type ExtractedItem = {
  key?: string;
  printedName?: string;
  name: string;
  printedAmount: number;
  discount?: number;
  priceBasis?: string;
  taxGroup?: string;
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
  region?: Region;
};
export type StatementSourceKind = "card" | "bank";
export type UploadSourceKind = "receipt" | "order" | "email" | StatementSourceKind;
export const importModels = [
  { value: "gemini-3.8-flash", label: "Gemini 3.8 Flash", tier: "free" },
  { value: "gemini-3.5-flash-lite", label: "Gemini 3.5 Flash-Lite", tier: "free" },
  { value: "gemini-3.1-flash-lite", label: "Gemini 3.1 Flash-Lite", tier: "free" },
  { value: "gemini-3.1-pro-preview", label: "Gemini 3.1 Pro Preview", tier: "paid" },
] as const;

export type ImportModel = (typeof importModels)[number]["value"];
export type ImportModelChoice = { model: ImportModel; quotaDocumentScope: string };

export const defaultImportModel: ImportModel = "gemini-3.8-flash";
export const proImportModel: ImportModel = "gemini-3.1-pro-preview";

export function importModelSelectionForQuota(
  quotaLimitedDocumentIDs: readonly string[],
  choice?: ImportModelChoice,
) {
  const quotaDocumentScope = JSON.stringify(quotaLimitedDocumentIDs);
  if (choice?.quotaDocumentScope === quotaDocumentScope) {
    return { model: choice.model, quotaDocumentScope, autoSelectedForQuota: false };
  }
  return {
    model: quotaLimitedDocumentIDs.length > 0 ? proImportModel : defaultImportModel,
    quotaDocumentScope,
    autoSelectedForQuota: quotaLimitedDocumentIDs.length > 0,
  };
}

const quotaLimitedExtractionErrorCodes = new Set(["rate_limited", "quota_reservation_deferred"]);

export function isImportModel(value: string): value is ImportModel {
  return importModels.some((model) => model.value === value);
}

export function receiptDateInputValue(purchasedAt?: string): string {
  if (!purchasedAt) return "";
  if (/^\d{4}-\d{2}-\d{2}$/.test(purchasedAt)) return purchasedAt;
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

export function receiptTimeInputValue(
  purchasedAt?: string,
  datePrecision?: "DATE_TIME" | "DATE_ONLY",
): string {
  if (datePrecision === "DATE_ONLY" || !purchasedAt || !/[T ]\d{2}:\d{2}/.test(purchasedAt))
    return "";
  const date = new Date(purchasedAt);
  if (Number.isNaN(date.getTime())) return "";
  const parts = Object.fromEntries(
    new Intl.DateTimeFormat("en", {
      timeZone: "Asia/Tokyo",
      hour: "2-digit",
      hourCycle: "h23",
      minute: "2-digit",
    })
      .formatToParts(date)
      .map((part) => [part.type, part.value]),
  );
  return `${parts.hour}:${parts.minute}`;
}

export function receiptOccurredAt(
  purchasedAt?: string,
  manualDate?: string,
  manualTime?: string,
  datePrecision?: "DATE_TIME" | "DATE_ONLY",
): string | null {
  if (manualDate === undefined && (manualTime !== undefined || datePrecision === "DATE_ONLY")) {
    return receiptOccurredAt(
      purchasedAt,
      receiptDateInputValue(purchasedAt),
      manualTime,
      datePrecision,
    );
  }
  if (manualDate !== undefined) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(manualDate)) return null;
    const calendarDate = new Date(`${manualDate}T00:00:00.000Z`);
    if (
      Number.isNaN(calendarDate.getTime()) ||
      calendarDate.toISOString().slice(0, 10) !== manualDate
    ) {
      return null;
    }
    if (manualTime !== undefined) {
      if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(manualTime)) return null;
      return new Date(`${manualDate}T${manualTime}:00+09:00`).toISOString();
    }
    if (datePrecision === "DATE_ONLY" || !purchasedAt || !/[T ]\d{2}:\d{2}/.test(purchasedAt)) {
      return new Date(`${manualDate}T12:00:00+09:00`).toISOString();
    }
    const extractedDate =
      purchasedAt && /[T ]\d{2}:\d{2}/.test(purchasedAt) ? new Date(purchasedAt) : null;
    if (extractedDate && !Number.isNaN(extractedDate.getTime())) {
      const timeParts = Object.fromEntries(
        new Intl.DateTimeFormat("en", {
          timeZone: "Asia/Tokyo",
          hour: "2-digit",
          hourCycle: "h23",
          minute: "2-digit",
          second: "2-digit",
        })
          .formatToParts(extractedDate)
          .map((part) => [part.type, part.value]),
      );
      const milliseconds = String(extractedDate.getUTCMilliseconds()).padStart(3, "0");
      return new Date(
        `${manualDate}T${timeParts.hour}:${timeParts.minute}:${timeParts.second}.${milliseconds}+09:00`,
      ).toISOString();
    }
    return null;
  }
  if (!purchasedAt || !/[T ]\d{2}:\d{2}/.test(purchasedAt)) return null;
  const date = new Date(purchasedAt);
  return Number.isNaN(date.getTime()) ? null : date.toISOString();
}

export function statementConfirmationIssue(entry: StatementEntry): string | undefined {
  if (entry.kind === "ACCOUNT_TRANSFER") {
    if (!Number.isSafeInteger(entry.amount) || entry.amount === 0) {
      return "振替金額を確認してください";
    }
    if (!entry.occurredAt || Number.isNaN(new Date(entry.occurredAt).getTime())) {
      return "振替日を確認してください";
    }
    return undefined;
  }
  if (entry.kind === "CARD_BILL_PAYMENT") {
    if (!Number.isSafeInteger(entry.amount) || entry.amount >= 0) {
      return "カード引落額を確認してください";
    }
    if (!entry.occurredAt || Number.isNaN(new Date(entry.occurredAt).getTime())) {
      return "カード引落日を確認してください";
    }
    return undefined;
  }
  if (entry.kind === "EXTERNAL_INCOME") {
    if (!Number.isSafeInteger(entry.amount) || entry.amount <= 0) {
      return "入金額を確認してください";
    }
    if (!entry.occurredAt || Number.isNaN(new Date(entry.occurredAt).getTime())) {
      return "入金日を確認してください";
    }
    return undefined;
  }
  if (entry.kind === "REFUND") {
    if (!Number.isSafeInteger(entry.amount) || entry.amount >= 0) {
      return "返金額を確認してください";
    }
    if (!entry.occurredAt || Number.isNaN(new Date(entry.occurredAt).getTime())) {
      return "返品日を確認してください";
    }
    return undefined;
  }
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
  itemInterpretations?: Array<{ index: number; priceBasis: string; taxGroup: string }>;
  groupInterpretations?: Array<{ id: string; basis: string; rounding: string }>;
  computedTotal: number;
  consistent: boolean;
  issues: string[];
};
export type DocumentReview = {
  id: string;
  jobId?: string;
  pendingInteraction?: {
    jobId: string;
    status: "queued" | "running" | "failed";
    message: string;
    replyPreview?: string;
    model?: string;
    baseJobId?: string;
  };
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
  model?: string;
  errorCode?: string;
  sourceKind?: string;
  mimeType?: string;
  createdAt?: string;
  sha256?: string;
  duplicateOf?: string;
  hasEvidence?: boolean;
  deletionPending?: boolean;
  receiptConfirmationState?: ReceiptConfirmationState;
  confirmations?: Array<{
    sourceReference: string;
    purchaseId: string;
    correctionId?: string;
    accountMovementId?: string;
    accountIncomeId?: string;
    transferId?: string;
  }>;
  result?: {
    conversationId?: string;
    conversation?: Array<{ role: "user" | "assistant"; text: string }>;
    receipt?: {
      merchant?: string;
      purchasedAt?: string;
      datePrecision?: "DATE_TIME" | "DATE_ONLY";
      total?: number;
      items?: ExtractedItem[];
      taxGroups?: Array<{
        id: string;
        numerator: number;
        denominator: number;
        basis?: string;
        rounding?: string;
        printedSubtotal?: number;
        printedTax?: number;
      }>;
      payments?: ReceiptPayment[];
    };
    computedTotal?: number;
    balanced?: boolean;
    candidates?: VerificationCandidate[];
    issues?: string[];
    transform?: number[];
    transformPage?: number;
    ocrLines?: Array<{ key?: string; text: string; confidence: number; region?: Region }>;
    statements?: StatementEntry[];
  };
};

export type DocumentExtractionVersion = {
  id: string;
  version: number;
  model: string;
  createdAt: string;
  conversationTurns: number;
};

export type DocumentExtraction = DocumentExtractionVersion & {
  result: NonNullable<DocumentReview["result"]>;
};

export function isQuotaLimitedExtraction(document: DocumentReview): boolean {
  const status = document.status.toUpperCase();
  return (
    (status === "QUEUED" || status === "FAILED") &&
    document.errorCode !== undefined &&
    quotaLimitedExtractionErrorCodes.has(document.errorCode)
  );
}

export function purchaseDatePrecisionFor(document: DocumentReview): "DATE_TIME" | "DATE_ONLY" {
  if (document.receiptConfirmationState?.purchaseTime !== undefined) {
    return document.receiptConfirmationState.purchaseTime ? "DATE_TIME" : "DATE_ONLY";
  }
  return (
    document.result?.receipt?.datePrecision ??
    (/[T ]\d{2}:\d{2}/.test(document.result?.receipt?.purchasedAt ?? "")
      ? "DATE_TIME"
      : "DATE_ONLY")
  );
}

export function purchaseOccurredAtFor(document: DocumentReview) {
  const purchaseTime = document.receiptConfirmationState?.purchaseTime;
  return receiptOccurredAt(
    document.result?.receipt?.purchasedAt,
    document.receiptConfirmationState?.purchaseDate,
    purchaseTime || undefined,
    purchaseDatePrecisionFor(document),
  );
}

// APIから届いた証拠品目とOCR行に一覧の再利用に耐える安定キーを付与する。編集時は既存品目を展開し直すためキーは保持される。
export function withStableEvidenceKeys(document: DocumentReview): DocumentReview {
  const result = document.result;
  if (!result) return document;
  const receipt = result.receipt;
  const items = receipt?.items;
  const ocrLines = result.ocrLines;
  if (!items?.length && !ocrLines?.length) return document;
  return {
    ...document,
    result: {
      ...result,
      receipt:
        receipt && items?.length
          ? {
              ...receipt,
              items: items.map((item, index) => Object.assign({}, item, { key: `item-${index}` })),
            }
          : receipt,
      ocrLines: ocrLines?.length
        ? ocrLines.map((line, index) => Object.assign({}, line, { key: `ocr-${index}` }))
        : ocrLines,
    },
  };
}

export function canReclassifyAttachment(
  document: DocumentReview,
  targetKind?: StatementSourceKind,
) {
  return (
    !!document.attachmentParent &&
    !document.hasEvidence &&
    !document.confirmations?.length &&
    document.status.toUpperCase() !== "RUNNING" &&
    (!targetKind || targetKind !== document.sourceKind)
  );
}

export function canDeleteDocument(document: DocumentReview) {
  if (document.deletionPending) return true;

  return (
    (document.status.toUpperCase() === "SUCCEEDED" || document.status.toUpperCase() === "FAILED") &&
    !document.hasEvidence &&
    !document.confirmations?.length &&
    !document.attachmentParent &&
    !document.attachments?.length
  );
}

export type PendingUpload = {
  id: string;
  file: File;
  analysisFile?: File;
  analysisTransform?: [number, number, number, number, number, number, number, number, number];
  sourceKind: UploadSourceKind;
  model: ImportModel;
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

export function parseDocumentFilters(search: Record<string, unknown>): DocumentFilters {
  const sourceKind = typeof search.sourceKind === "string" ? search.sourceKind : "";
  const confirmed = typeof search.confirmed === "string" ? search.confirmed : "";
  return {
    store: typeof search.store === "string" ? search.store : "",
    from: typeof search.from === "string" ? search.from : "",
    to: typeof search.to === "string" ? search.to : "",
    sourceKind: ["receipt", "order", "email", "card", "bank"].includes(sourceKind)
      ? sourceKind
      : "",
    confirmed: confirmed === "true" || confirmed === "false" ? confirmed : "",
  };
}

export function serializeDocumentFilters(filters: DocumentFilters): Partial<DocumentFilters> {
  return {
    ...(filters.store ? { store: filters.store } : {}),
    ...(filters.from ? { from: filters.from } : {}),
    ...(filters.to ? { to: filters.to } : {}),
    ...(filters.sourceKind ? { sourceKind: filters.sourceKind } : {}),
    ...(filters.confirmed ? { confirmed: filters.confirmed } : {}),
  };
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
export const isEmailFile = (file: File) =>
  /\.eml$/i.test(file.name) || file.type === "message/rfc822";

export const uploadSourceKinds = (file: File): readonly UploadSourceKind[] =>
  isEmailFile(file)
    ? []
    : isCsvFile(file)
      ? ["card", "bank"]
      : ["receipt", "order", "card", "bank"];

export const defaultUploadSourceKind = (file: File): UploadSourceKind =>
  isCsvFile(file) ? "card" : isEmailFile(file) ? "email" : "receipt";

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
      "DELETION-PENDING": "削除待ち",
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

/** 不明な値がオブジェクトである場合にのみレコード型として扱い、それ以外は undefined を返す。 */
export function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null) return undefined;
  return { ...value };
}
