import { useCallback, useEffect, useRef, useState } from "react";
import {
  AlertCircle,
  Camera,
  Check,
  ChevronRight,
  FileImage,
  Link2,
  ShieldCheck,
  Upload,
  X,
} from "lucide-react";
import { readGraphql, type Household, type ProductSuggestion } from "../../../shared/api";
import { useCreateAndPostPurchase } from "../../../entities/purchase";
import { InlineError } from "../../../shared/ui";
import { useHouseholdPurchases } from "../../../entities/purchase";
import { productSuggestionsQuery } from "../../../entities/product";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const isCsvFile = (file: File) => /\.csv$/i.test(file.name) || file.type === "text/csv";
const sourceKind = (file: File, csvKind: "card" | "bank") =>
  isCsvFile(file) ? csvKind : /\.eml$/i.test(file.name) ? "email" : "receipt";
const isSupportedFile = (file: File) =>
  /\.(jpe?g|png|webp|pdf|csv|eml)$/i.test(file.name) ||
  [
    "image/jpeg",
    "image/png",
    "image/webp",
    "application/pdf",
    "text/csv",
    "message/rfc822",
  ].includes(file.type);
const importIdempotencyKey = async (documentId: string, sourceReference: string) => {
  const input = new TextEncoder().encode(`${documentId}\u0000${sourceReference}`);
  const digest = await crypto.subtle.digest("SHA-256", input);
  const hash = Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
  return `import:${hash}`;
};

type ExtractedItem = {
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
type Region = { page: number; top: number; left: number; bottom: number; right: number };
type PreviewTransform = [number, number, number, number, number, number, number, number, number];
type MatchCandidate = {
  documentId: string;
  sourceReference: string;
  state: string;
  reasons: string[];
};
type ObservationCandidates = { sourceReference: string; matches: MatchCandidate[] };
type StatementEntry = {
  row: number;
  kind: string;
  occurredAt: string;
  merchant: string;
  amount: number;
  transactionId?: string;
  orderId?: string;
  cardLast4?: string;
};
type ReceiptPayment = { method: string; cardLast4?: string; amount: number };
type VerificationCandidate = {
  id: string;
  amounts: number[];
  computedTotal: number;
  consistent: boolean;
  issues: string[];
};
type DocumentReview = {
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
type PendingUpload = {
  id: string;
  file: File;
  status: "queued" | "uploading" | "failed";
  error?: string;
};
type DocumentFilters = {
  store: string;
  from: string;
  to: string;
  sourceKind: string;
  confirmed: string;
};
type ReceiptPaymentDraft = {
  key: string;
  method?: string;
  cardLast4?: string;
  paymentAccountId: string;
  amountYen: number;
};
type ReviewTarget = { key: string; label: string; elementId: string };
const documentListURL = (householdId: string, filters: DocumentFilters, after?: string) => {
  const query = new URLSearchParams({ householdId, first: "50" });
  if (filters.store.trim()) query.set("store", filters.store.trim());
  if (filters.from) query.set("from", filters.from);
  if (filters.to) query.set("to", filters.to);
  if (filters.sourceKind) query.set("sourceKind", filters.sourceKind);
  if (filters.confirmed) query.set("confirmed", filters.confirmed);
  if (after) query.set("after", after);
  return `/documents?${query.toString()}`;
};
const identityTransform: PreviewTransform = [1, 0, 0, 0, 1, 0, 0, 0, 1];
const invertTransform = (matrix: PreviewTransform): PreviewTransform | null => {
  const [a, b, c, d, e, f, g, h, i] = matrix;
  const A = e * i - f * h;
  const B = f * g - d * i;
  const C = d * h - e * g;
  const determinant = a * A + b * B + c * C;
  if (!Number.isFinite(determinant) || Math.abs(determinant) < 1e-12) return null;
  return [
    A / determinant,
    (c * h - b * i) / determinant,
    (b * f - c * e) / determinant,
    B / determinant,
    (a * i - c * g) / determinant,
    (c * d - a * f) / determinant,
    C / determinant,
    (b * g - a * h) / determinant,
    (a * e - b * d) / determinant,
  ];
};
const transformPoint = (matrix: PreviewTransform, x: number, y: number) => {
  const divisor = matrix[6] * x + matrix[7] * y + matrix[8];
  if (!Number.isFinite(divisor) || Math.abs(divisor) < 1e-12) return [x, y] as const;
  return [
    (matrix[0] * x + matrix[1] * y + matrix[2]) / divisor,
    (matrix[3] * x + matrix[4] * y + matrix[5]) / divisor,
  ] as const;
};
const regionPolygon = (region: Region, previewToOriginal: PreviewTransform) => {
  const originalToPreview = invertTransform(previewToOriginal) ?? identityTransform;
  return [
    transformPoint(originalToPreview, region.left, region.top),
    transformPoint(originalToPreview, region.right, region.top),
    transformPoint(originalToPreview, region.right, region.bottom),
    transformPoint(originalToPreview, region.left, region.bottom),
  ]
    .map(([x, y]) => `${Math.max(0, Math.min(1, x)) * 100}% ${Math.max(0, Math.min(1, y)) * 100}%`)
    .join(", ");
};
const terminalStatuses = new Set([
  "READY",
  "REVIEW",
  "COMPLETED",
  "SUCCEEDED",
  "FAILED",
  "DUPLICATE",
  "DONE",
]);
const statusLabel = (status: string) =>
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
  })[status.toUpperCase()] ?? "状態を確認中";
const sourceKindLabel = (sourceKind?: string) =>
  ({
    receipt: "レシート",
    order: "注文履歴",
    email: "メール",
    card: "カード明細",
    bank: "銀行明細",
  })[sourceKind ?? ""] ?? "証拠";
const isProcessing = (status: string) => !terminalStatuses.has(status.toUpperCase());

function ProductVariantSelect({
  householdId,
  itemName,
  merchant,
  value,
  onChange,
}: {
  householdId: string;
  itemName: string;
  merchant: string;
  value: string;
  onChange: (variantId: string) => void;
}) {
  const [suggestions, setSuggestions] = useState<ProductSuggestion[]>([]);
  const [suggestionsQuery, setSuggestionsQuery] = useState("");
  const [selectedSuggestion, setSelectedSuggestion] = useState<{
    itemName: string;
    suggestion: ProductSuggestion;
  } | null>(null);
  const [searchEnabled, setSearchEnabled] = useState(false);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    const query = itemName.trim();
    if (!searchEnabled || query.length < 2) {
      setSuggestions([]);
      setLoading(false);
      return;
    }
    let active = true;
    const timer = window.setTimeout(() => {
      setLoading(true);
      void readGraphql<{ productSuggestions: ProductSuggestion[] }>(productSuggestionsQuery, {
        householdId,
        query,
        merchant: merchant.trim() || null,
        first: 8,
      })
        .then((result) => {
          if (!active) return;
          setSuggestions(result.productSuggestions);
          setSuggestionsQuery(query);
        })
        .catch(() => active && setSuggestions([]))
        .finally(() => active && setLoading(false));
    }, 220);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [householdId, itemName, merchant, searchEnabled]);

  const currentSuggestions = suggestionsQuery === itemName.trim() ? suggestions : [];
  const currentSelectedSuggestion =
    selectedSuggestion?.itemName === itemName ? selectedSuggestion.suggestion : null;
  const options = currentSuggestions.some((suggestion) => suggestion.variant.id === value)
    ? currentSuggestions
    : currentSelectedSuggestion?.variant.id === value
      ? [currentSelectedSuggestion, ...currentSuggestions]
      : currentSuggestions;

  return (
    <label>
      商品
      <select
        aria-label={`${itemName || "品目"}の商品variant`}
        value={value}
        onFocus={() => setSearchEnabled(true)}
        onChange={(event) => {
          const variantId = event.target.value;
          const suggestion =
            currentSuggestions.find((entry) => entry.variant.id === variantId) ?? null;
          setSelectedSuggestion(suggestion ? { itemName, suggestion } : null);
          onChange(variantId);
        }}
      >
        <option value="">商品を選択</option>
        {options.map((suggestion) => (
          <option value={suggestion.variant.id} key={suggestion.variant.id}>
            {[suggestion.concept.name, suggestion.variant.name, suggestion.variant.brand]
              .filter(Boolean)
              .join(" · ")}
            {suggestion.variant.packageSize ? ` (${suggestion.variant.packageSize})` : ""}
          </option>
        ))}
      </select>
      {loading && <small>商品候補を検索中</small>}
      {searchEnabled &&
        !loading &&
        itemName.trim().length >= 2 &&
        currentSuggestions.length === 0 && <small>商品候補がありません</small>}
    </label>
  );
}

export function ImportsPage({
  household,
  onAdded,
  onUnsavedChanges,
}: {
  household: Household;
  onAdded: (keepOpen?: boolean) => void;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  const createAndPostPurchase = useCreateAndPostPurchase();
  const inputRef = useRef<HTMLInputElement>(null);
  const cameraInputRef = useRef<HTMLInputElement>(null);
  const [files, setFiles] = useState<PendingUpload[]>([]);
  const editedReceiptDocuments = useRef(new Set<string>());
  const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false);
  const [csvKinds, setCsvKinds] = useState<Record<string, "card" | "bank">>({});
  const [documents, setDocuments] = useState<DocumentReview[]>([]);
  const [documentFilters, setDocumentFilters] = useState<DocumentFilters>({
    store: "",
    from: "",
    to: "",
    sourceKind: "",
    confirmed: "",
  });
  const [documentFilterDraft, setDocumentFilterDraft] = useState(documentFilters);
  const documentFilterScope = JSON.stringify(documentFilters);
  const documentFilterScopeRef = useRef(documentFilterScope);
  const [nextDocumentCursor, setNextDocumentCursor] = useState<string | null>(null);
  const [loadingOlderDocuments, setLoadingOlderDocuments] = useState(false);
  const loadedOlderPages = useRef(false);
  const [dragging, setDragging] = useState(false);
  const [message, setMessage] = useState("");
  const [pending, setPending] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [confirmedStatements, setConfirmedStatements] = useState<Set<string>>(() => new Set());
  const [selectedCandidates, setSelectedCandidates] = useState<Record<string, string>>({});
  const [reviewTargetIndex, setReviewTargetIndex] = useState(0);
  const [manualAmountOverrides, setManualAmountOverrides] = useState<Record<string, boolean>>({});
  const [manualAmountOverrideReasons, setManualAmountOverrideReasons] = useState<
    Record<string, string>
  >({});
  const paymentAccountId = household.accounts[0]?.id ?? "";
  const [receiptPayments, setReceiptPayments] = useState<Record<string, ReceiptPaymentDraft[]>>({});
  const [cropRegion, setCropRegion] = useState<Region | null>(null);
  const [cropPending, setCropPending] = useState(false);
  const [previewPage, setPreviewPage] = useState(0);
  const [previewPageDraft, setPreviewPageDraft] = useState("1");
  const [preview, setPreview] = useState<{
    documentId: string;
    page: number;
    src: string;
    transform: PreviewTransform;
  } | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewError, setPreviewError] = useState("");
  const [observations, setObservations] = useState<ObservationCandidates[]>([]);
  const [matchesTruncated, setMatchesTruncated] = useState(false);
  const [selectedMatches, setSelectedMatches] = useState<Set<string>>(() => new Set());
  const [targetPurchaseId, setTargetPurchaseId] = useState("");
  const [targetItemId, setTargetItemId] = useState("");
  const [activeObservationRef, setActiveObservationRef] = useState("");
  const [receiptEvidenceIndex, setReceiptEvidenceIndex] = useState("0");
  const { purchases: recentPurchases, refresh: refreshPurchases } = useHouseholdPurchases(
    household.id,
    100,
  );
  const selectedDocument = documents.find((document) => document.id === selected);
  const targetPurchase = recentPurchases.find((purchase) => purchase.id === targetPurchaseId);
  const normalizeName = (name: string) =>
    name.trim().replace(/\s+/g, " ").toLocaleLowerCase("ja-JP");
  const suggestedCategoryId = (item: ExtractedItem) => {
    const categories = household.categories.filter((category) => !category.archived);
    const hinted = categories.filter(
      (category) => normalizeName(category.name) === normalizeName(item.categoryHint ?? ""),
    );
    if (hinted.length === 1) return hinted[0].id;
    const matchingCategoryIds = new Set(
      recentPurchases
        .filter((purchase) => purchase.status !== "DRAFT")
        .flatMap((purchase) => purchase.items)
        .filter((purchaseItem) => normalizeName(purchaseItem.name) === normalizeName(item.name))
        .map((purchaseItem) => purchaseItem.categoryId)
        .filter((categoryId): categoryId is string => Boolean(categoryId)),
    );
    return matchingCategoryIds.size === 1 ? [...matchingCategoryIds][0] : "";
  };
  const receiptItemAmount = (document: DocumentReview, item: ExtractedItem, index: number) => {
    const candidateID = selectedCandidates[document.id] ?? "";
    const candidate = document.result?.candidates?.find((entry) => entry.id === candidateID);
    return manualAmountOverrides[document.id]
      ? (item.grossAmount ?? item.printedAmount)
      : (candidate?.amounts[index] ?? item.grossAmount ?? item.printedAmount);
  };
  const receiptTotal = (document: DocumentReview) =>
    (document.result?.receipt?.items ?? []).reduce(
      (total, item, index) => total + receiptItemAmount(document, item, index),
      0,
    );
  const reviewTargets: ReviewTarget[] = selectedDocument?.result?.receipt?.items
    ? [
        ...selectedDocument.result.receipt.items.flatMap((item, index) =>
          item.verification.toUpperCase() === "UNRESOLVED" || item.confidence < 0.8
            ? [
                {
                  key: `item:${index}`,
                  label: `品目 ${index + 1}`,
                  elementId: `receipt-review-item-${index}`,
                },
              ]
            : [],
        ),
        ...(() => {
          const selectedCandidate = selectedDocument.result?.candidates?.find(
            (candidate) => candidate.id === selectedCandidates[selectedDocument.id],
          );
          const needsCandidateSelection =
            (selectedDocument.result?.candidates?.length ?? 0) > 0 && !selectedCandidate;
          const paymentsTotal = paymentDraftFor(selectedDocument).reduce(
            (total, payment) => total + payment.amountYen,
            0,
          );
          const needsAmountReview =
            manualAmountOverrides[selectedDocument.id] ||
            needsCandidateSelection ||
            (selectedCandidate
              ? !selectedCandidate.consistent
              : selectedDocument.result?.balanced === false);
          const needsPaymentReview =
            household.accounts.length > 0 && paymentsTotal !== receiptTotal(selectedDocument);
          return [
            ...(needsAmountReview
              ? [
                  {
                    key: "amount",
                    label: "金額差異",
                    elementId: needsCandidateSelection
                      ? "receipt-review-candidates"
                      : "receipt-review-amount-reason",
                  },
                ]
              : []),
            ...(needsPaymentReview
              ? [
                  {
                    key: "payments",
                    label: "決済合計",
                    elementId: "receipt-review-payments",
                  },
                ]
              : []),
          ];
        })(),
      ]
    : [];
  const activeReviewIndex = Math.min(reviewTargetIndex, Math.max(0, reviewTargets.length - 1));
  const activeReviewTarget = reviewTargets[activeReviewIndex];
  useEffect(() => {
    setReviewTargetIndex(0);
  }, [selectedDocument?.id]);
  useEffect(() => {
    if (!activeReviewTarget) return;
    const element = document.getElementById(activeReviewTarget.elementId);
    element?.scrollIntoView({ block: "center", behavior: "smooth" });
    if (element instanceof HTMLElement) element.focus({ preventScroll: true });
  }, [activeReviewTarget?.key, activeReviewTarget?.elementId]);
  function paymentDraftFor(document: DocumentReview): ReceiptPaymentDraft[] {
    if (receiptPayments[document.id]) return receiptPayments[document.id];
    const recognizedPayments = document.result?.receipt?.payments ?? [];
    if (recognizedPayments.length) {
      return recognizedPayments.map((payment, index) => {
        const method = normalizeName(payment.method);
        const matchingAccounts = household.accounts.filter(
          (account) =>
            normalizeName(account.name) === method || normalizeName(account.kind) === method,
        );
        return {
          key: `recognized:${index}`,
          method: payment.method,
          cardLast4: payment.cardLast4,
          paymentAccountId: matchingAccounts.length === 1 ? matchingAccounts[0].id : "",
          amountYen: payment.amount,
        };
      });
    }
    return [
      {
        key: "default",
        paymentAccountId: household.accounts[0]?.id ?? "",
        amountYen: receiptTotal(document),
      },
    ];
  }
  const markUnsaved = () => setHasUnsavedChanges(true);
  useEffect(() => {
    onUnsavedChanges(hasUnsavedChanges);
    return () => onUnsavedChanges(false);
  }, [hasUnsavedChanges, onUnsavedChanges]);
  useEffect(() => {
    setPreviewPage(0);
    setPreviewPageDraft("1");
    setCropRegion(null);
  }, [selectedDocument?.id]);
  useEffect(() => {
    if (
      !selectedDocument ||
      !selectedDocument.mimeType ||
      (!selectedDocument.mimeType.startsWith("image/") &&
        selectedDocument.mimeType !== "application/pdf")
    ) {
      setPreview(null);
      setPreviewError("");
      setPreviewLoading(false);
      return;
    }
    const controller = new AbortController();
    let active = true;
    let objectUrl = "";
    setPreview(null);
    setPreviewLoading(true);
    setPreviewError("");
    void fetch(
      `/documents/${encodeURIComponent(selectedDocument.id)}/preview?page=${previewPage}`,
      { credentials: "include", signal: controller.signal },
    )
      .then(async (response) => {
        if (!response.ok) throw new Error(`原本ページを表示できませんでした (${response.status})`);
        const sourcePage = Number(response.headers.get("X-Source-Page"));
        if (sourcePage !== previewPage) throw new Error("原本ページが一致しません");
        const encodedTransform = response.headers.get("X-Coordinate-Transform");
        let transform = identityTransform;
        if (encodedTransform) {
          const parsed = JSON.parse(encodedTransform) as number[];
          if (parsed.length === 9 && parsed.every(Number.isFinite)) {
            transform = parsed as PreviewTransform;
          }
        }
        if (!invertTransform(transform)) throw new Error("原本の座標を読み取れません");
        const blob = await response.blob();
        if (blob.type !== "image/png") throw new Error("原本ページを表示できませんでした");
        if (!active) return;
        objectUrl = URL.createObjectURL(blob);
        setPreview({
          documentId: selectedDocument.id,
          page: previewPage,
          src: objectUrl,
          transform,
        });
      })
      .catch((reason) => {
        if (active && (reason as Error).name !== "AbortError") {
          setPreviewError((reason as Error).message);
        }
      })
      .finally(() => active && setPreviewLoading(false));
    return () => {
      active = false;
      controller.abort();
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [selectedDocument?.id, selectedDocument?.mimeType, previewPage]);
  const refreshDocuments = useCallback(
    async (silent = false) => {
      try {
        const requestScope = documentFilterScope;
        const response = await fetch(documentListURL(household.id, documentFilters), {
          credentials: "include",
        });
        if (!response.ok) throw new Error(`証拠一覧を取得できませんでした (${response.status})`);
        const payload = (await response.json()) as {
          documents: DocumentReview[];
          nextCursor: string | null;
        };
        if (requestScope !== documentFilterScopeRef.current) return;
        setDocuments((current) => [
          ...payload.documents.map((document) => {
            const existing = current.find((entry) => entry.id === document.id);
            return {
              ...document,
              ...(existing && editedReceiptDocuments.current.has(document.id)
                ? { result: existing.result }
                : {}),
              name:
                existing?.name ??
                document.name ??
                `${sourceKindLabel(document.sourceKind)} · ${
                  document.createdAt
                    ? new Date(document.createdAt).toLocaleString("ja-JP", {
                        month: "numeric",
                        day: "numeric",
                        hour: "2-digit",
                        minute: "2-digit",
                      })
                    : document.id.slice(0, 8)
                }`,
            };
          }),
          ...current.filter(
            (entry) => !payload.documents.some((document) => document.id === entry.id),
          ),
        ]);
        if (!loadedOlderPages.current) setNextDocumentCursor(payload.nextCursor);
        setSelected((current) => current ?? payload.documents[0]?.id ?? null);
      } catch (reason) {
        if (!silent) setMessage((reason as Error).message);
      }
    },
    [documentFilterScope, documentFilters, household.id],
  );
  useEffect(() => {
    documentFilterScopeRef.current = documentFilterScope;
    setDocuments([]);
    setNextDocumentCursor(null);
    loadedOlderPages.current = false;
    editedReceiptDocuments.current.clear();
    setSelected(null);
    setConfirmedStatements(new Set());
    setSelectedCandidates({});
    void refreshDocuments();
  }, [documentFilterScope, refreshDocuments]);
  const loadOlderDocuments = useCallback(async () => {
    if (!nextDocumentCursor || loadingOlderDocuments) return;
    setLoadingOlderDocuments(true);
    const requestScope = documentFilterScope;
    try {
      const response = await fetch(
        documentListURL(household.id, documentFilters, nextDocumentCursor),
        { credentials: "include" },
      );
      if (!response.ok) throw new Error(`証拠一覧を取得できませんでした (${response.status})`);
      const payload = (await response.json()) as {
        documents: DocumentReview[];
        nextCursor: string | null;
      };
      if (requestScope !== documentFilterScopeRef.current) return;
      setDocuments((current) => {
        const ids = new Set(current.map((document) => document.id));
        return [
          ...current,
          ...payload.documents
            .filter((document) => !ids.has(document.id))
            .map((document) => ({
              ...document,
              name:
                document.name ??
                `${sourceKindLabel(document.sourceKind)} · ${
                  document.createdAt
                    ? new Date(document.createdAt).toLocaleString("ja-JP", {
                        month: "numeric",
                        day: "numeric",
                        hour: "2-digit",
                        minute: "2-digit",
                      })
                    : document.id.slice(0, 8)
                }`,
            })),
        ];
      });
      setNextDocumentCursor(payload.nextCursor);
      loadedOlderPages.current = true;
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setLoadingOlderDocuments(false);
    }
  }, [
    documentFilterScope,
    documentFilters,
    household.id,
    loadingOlderDocuments,
    nextDocumentCursor,
  ]);
  const hasProcessingDocuments = documents.some((document) => isProcessing(document.status));
  useEffect(() => {
    if (!hasProcessingDocuments) return;
    const interval = window.setInterval(() => void refreshDocuments(true), 1200);
    return () => window.clearInterval(interval);
  }, [hasProcessingDocuments, refreshDocuments]);
  useEffect(() => {
    if (!selectedDocument || !selectedDocument.result) {
      setObservations([]);
      setMatchesTruncated(false);
      setSelectedMatches(new Set());
      return;
    }
    let active = true;
    setObservations([]);
    setMatchesTruncated(false);
    setSelectedMatches(new Set());
    void fetch(`/documents/${encodeURIComponent(selectedDocument.id)}/matches`, {
      credentials: "include",
    })
      .then(async (response) => {
        if (!response.ok) throw new Error(`照合候補を取得できませんでした (${response.status})`);
        return (await response.json()) as {
          observations: ObservationCandidates[];
          truncated: boolean;
        };
      })
      .then((payload) => {
        if (!active) return;
        setObservations(payload.observations);
        setMatchesTruncated(payload.truncated);
        if (!selectedDocument.result?.receipt?.items?.length) {
          setActiveObservationRef(payload.observations[0]?.sourceReference ?? "");
        } else {
          setActiveObservationRef("");
        }
      })
      .catch((reason) => active && setMessage((reason as Error).message));
    return () => {
      active = false;
    };
  }, [selectedDocument?.id, selectedDocument?.jobId, selectedDocument?.status]);
  const addFiles = (selected: FileList | null) => {
    if (!selected) return;
    const candidates = Array.from(selected);
    const supported = candidates.filter(isSupportedFile);
    const unsupported = candidates.filter((file) => !isSupportedFile(file));
    if (supported.length) markUnsaved();
    setFiles((current) => [
      ...current,
      ...supported.map((file) => ({ id: crypto.randomUUID(), file, status: "queued" as const })),
    ]);
    setMessage(
      unsupported.length
        ? `対応していない形式: ${unsupported.map((file) => file.name).join("、")}`
        : "",
    );
  };
  const uploadFiles = async () => {
    if (!files.length) return;
    setPending(true);
    setMessage("");
    const queued = files;
    const queuedIds = new Set(queued.map((entry) => entry.id));
    let nextIndex = 0;
    const failedUploads = new Set<string>();
    const uploadOne = async (entry: PendingUpload) => {
      setFiles((current) =>
        current.map((upload) =>
          upload.id === entry.id ? { ...upload, status: "uploading", error: undefined } : upload,
        ),
      );
      const { file } = entry;
      try {
        const body = new FormData();
        body.set("file", file);
        body.set("householdId", household.id);
        body.set("sourceKind", sourceKind(file, csvKinds[file.name] ?? "card"));
        const response = await fetch("/documents", {
          method: "POST",
          credentials: "include",
          body,
        });
        if (!response.ok) throw new Error(`証拠を送信できませんでした (${response.status})`);
        const accepted = (await response.json()) as {
          id: string;
          name?: string;
          sha256: string;
          status: string;
          duplicateOf?: string;
          duplicate?: boolean;
          jobId?: string;
        };
        const documentReview: DocumentReview = {
          id: accepted.id,
          jobId: accepted.jobId,
          name: accepted.name ?? file.name,
          status: accepted.status,
          sourceKind: sourceKind(file, csvKinds[file.name] ?? "card"),
          duplicateOf: accepted.duplicateOf,
          sha256: accepted.sha256,
        };
        setDocuments((current) => [
          documentReview,
          ...current.filter((document) => document.id !== documentReview.id),
        ]);
        setSelected((current) => current ?? accepted.id);
        setFiles((current) => current.filter((upload) => upload.id !== entry.id));
      } catch (reason) {
        failedUploads.add(entry.id);
        setFiles((current) =>
          current.map((upload) =>
            upload.id === entry.id
              ? { ...upload, status: "failed", error: (reason as Error).message }
              : upload,
          ),
        );
        setMessage((current) => (current ? current : `${file.name}: ${(reason as Error).message}`));
      }
    };
    const worker = async () => {
      while (nextIndex < queued.length) {
        const entry = queued[nextIndex++];
        await uploadOne(entry);
      }
    };
    try {
      await Promise.all(Array.from({ length: Math.min(3, queued.length) }, worker));
      setHasUnsavedChanges(
        failedUploads.size > 0 || files.some((upload) => !queuedIds.has(upload.id)),
      );
      void refreshDocuments(true);
    } finally {
      setPending(false);
    }
  };
  const retryDocument = async (document: DocumentReview) => {
    setPending(true);
    setMessage("");
    try {
      const response = await fetch(`/documents/${encodeURIComponent(document.id)}/retry`, {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({}),
      });
      if (!response.ok) throw new Error(`再抽出を開始できませんでした (${response.status})`);
      editedReceiptDocuments.current.delete(document.id);
      await refreshDocuments(true);
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const retryDocumentCrop = async (document: DocumentReview) => {
    if (!cropRegion) return;
    setCropPending(true);
    setMessage("");
    try {
      const response = await fetch(`/documents/${encodeURIComponent(document.id)}/retry`, {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ crop: cropRegion }),
      });
      if (!response.ok) throw new Error(`範囲を再抽出できませんでした (${response.status})`);
      editedReceiptDocuments.current.delete(document.id);
      const accepted = (await response.json()) as { jobId: string; status: string };
      setDocuments((current) =>
        current.map((entry) =>
          entry.id === document.id
            ? { ...entry, jobId: accepted.jobId, status: accepted.status, result: undefined }
            : entry,
        ),
      );
      setCropRegion(null);
      await refreshDocuments(true);
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setCropPending(false);
    }
  };
  const selectCropRegion = (region: Region) => {
    markUnsaved();
    setCropRegion(region);
    setPreviewPage(region.page);
    setPreviewPageDraft(String(region.page + 1));
  };
  const showPreviewPage = () => {
    const page = Number(previewPageDraft);
    if (!Number.isInteger(page) || page < 1) {
      setPreviewError("ページ番号を入力してください");
      return;
    }
    setPreviewPage(page - 1);
  };
  const confirmEvidenceGroup = async () => {
    if (
      !selectedDocument ||
      !targetPurchaseId ||
      (!activeObservationRef && !selectedDocument.result?.receipt?.items?.length)
    )
      return;
    const selectedCandidates = observations.flatMap((observation) =>
      observation.matches
        .filter((candidate) =>
          selectedMatches.has(
            `${observation.sourceReference}\u0000${candidate.documentId}\u0000${candidate.sourceReference}`,
          ),
        )
        .map((candidate) => ({
          documentId: candidate.documentId,
          sourceReference: candidate.sourceReference,
          ...(targetItemId ? { itemId: targetItemId } : {}),
        })),
    );
    const isReceiptObservation =
      Boolean(selectedDocument.result?.receipt?.items?.length) && !activeObservationRef;
    const receiptIndex = Number(receiptEvidenceIndex);
    const sources = [
      {
        documentId: selectedDocument.id,
        sourceReference: activeObservationRef,
        ...(targetItemId ? { itemId: targetItemId } : {}),
        ...(isReceiptObservation && selectedDocument.jobId && targetItemId
          ? { observationJobId: selectedDocument.jobId, sourceItemIndex: receiptIndex }
          : {}),
      },
      ...selectedCandidates,
    ];
    setPending(true);
    setMessage("");
    try {
      const response = await fetch(`/purchases/${encodeURIComponent(targetPurchaseId)}/evidence`, {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ sources }),
      });
      if (!response.ok) throw new Error(`証拠を購入に紐付けできませんでした (${response.status})`);
      setSelectedMatches(new Set());
      setHasUnsavedChanges(false);
      await refreshDocuments(true);
      onAdded(true);
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const editItem = (documentId: string, index: number, patch: Partial<ExtractedItem>) => {
    editedReceiptDocuments.current.add(documentId);
    markUnsaved();
    setDocuments((current) =>
      current.map((document) =>
        document.id !== documentId || !document.result?.receipt?.items
          ? document
          : {
              ...document,
              result: {
                ...document.result,
                receipt: {
                  ...document.result.receipt,
                  items: document.result.receipt.items.map((item, itemIndex) => {
                    if (itemIndex !== index) return item;
                    const nameChanged = patch.name !== undefined && patch.name !== item.name;
                    return {
                      ...item,
                      ...patch,
                      ...(nameChanged ? { productVariantId: null } : {}),
                    };
                  }),
                },
              },
            },
      ),
    );
  };
  const confirm = async (document: DocumentReview) => {
    const receipt = document.result?.receipt;
    const items = receipt?.items ?? [];
    const candidateID = selectedCandidates[document.id] ?? "";
    const candidate = document.result?.candidates?.find((item) => item.id === candidateID);
    const overrideReason = manualAmountOverrideReasons[document.id]?.trim() ?? "";
    const requiresOverrideReason =
      manualAmountOverrides[document.id] ||
      (candidate ? !candidate.consistent : !document.result?.balanced);
    const paymentDraft = paymentDraftFor(document);
    const totalYen = items.reduce(
      (total, item, index) => total + receiptItemAmount(document, item, index),
      0,
    );
    if (!items.length) {
      setMessage("確定できる品目がありません");
      return;
    }
    if (document.result?.candidates?.length && !selectedCandidates[document.id]) {
      setMessage("税計算候補を選択してください");
      return;
    }
    if (requiresOverrideReason && overrideReason.length < 3) {
      setMessage("金額の訂正理由を入力してください");
      return;
    }
    if (
      household.accounts.length > 0 &&
      (paymentDraft.some(
        (payment) =>
          !payment.paymentAccountId ||
          !Number.isSafeInteger(payment.amountYen) ||
          payment.amountYen < 0,
      ) ||
        paymentDraft.reduce((total, payment) => total + payment.amountYen, 0) !== totalYen)
    ) {
      setMessage("決済内訳と購入額を一致させてください");
      return;
    }
    setPending(true);
    setMessage("");
    try {
      const purchaseItems = items.map((item, index) => ({
        name: item.name,
        amountYen: receiptItemAmount(document, item, index),
        quantityNumerator: item.quantityNumerator,
        quantityDenominator: item.quantityDenominator,
        quantityUnit: item.quantityUnit,
        printedUnitPriceYen: item.printedUnitPriceYen,
        printedUnitPriceQuantityNumerator: item.printedUnitPriceQuantityNumerator,
        printedUnitPriceQuantityDenominator: item.printedUnitPriceQuantityDenominator,
        printedUnitPriceUnit: item.printedUnitPriceUnit,
        categoryId:
          item.categoryId != null ? item.categoryId || null : suggestedCategoryId(item) || null,
        productVariantId: item.productVariantId || null,
        allocations: null,
      }));
      const created = await createAndPostPurchase({
        idempotencyKey: await importIdempotencyKey(document.id, "receipt"),
        householdId: household.id,
        origin: "AI",
        sourceDocumentId: document.id,
        occurredAt: receipt?.purchasedAt
          ? new Date(receipt.purchasedAt).toISOString()
          : new Date().toISOString(),
        merchant: receipt?.merchant ?? "",
        paymentAllocations: paymentDraft
          .filter((payment) => payment.paymentAccountId && payment.amountYen > 0)
          .map((payment) => ({
            paymentAccountId: payment.paymentAccountId,
            amountYen: payment.amountYen,
          })),
        receiptCandidateId: candidateID || null,
        manualAmountOverrideReason: overrideReason || null,
        items: purchaseItems,
      });
      await refreshPurchases();
      const evidenceResponse = await fetch(
        `/purchases/${encodeURIComponent(created.createPurchase.id)}/evidence`,
        {
          method: "POST",
          credentials: "include",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            sources: items.map((_, index) => ({
              documentId: document.id,
              sourceReference: "",
              ...(created.createPurchase.items[index]?.id
                ? { itemId: created.createPurchase.items[index].id }
                : {}),
              ...(document.jobId
                ? { observationJobId: document.jobId, sourceItemIndex: index }
                : {}),
            })),
          }),
        },
      );
      if (!evidenceResponse.ok) {
        setTargetPurchaseId(created.createPurchase.id);
        onAdded(true);
        setMessage("購入は登録済みです。証拠の紐付けを再試行してください");
        return;
      }
      editedReceiptDocuments.current.delete(document.id);
      setHasUnsavedChanges(false);
      onAdded();
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const confirmStatement = async (document: DocumentReview, entry: StatementEntry) => {
    if (!document.result || entry.kind !== "CARD_PURCHASE") return;
    const reference = entry.transactionId
      ? `transaction:${entry.transactionId}`
      : `row:${entry.row}`;
    const key = `${document.id}:${reference}`;
    if (confirmedStatements.has(key)) return;
    setPending(true);
    setMessage("");
    try {
      const created = await createAndPostPurchase({
        idempotencyKey: await importIdempotencyKey(document.id, reference),
        householdId: household.id,
        origin: "AI",
        sourceDocumentId: document.id,
        sourceReference: reference,
        occurredAt: new Date(entry.occurredAt).toISOString(),
        merchant: entry.merchant,
        paymentAllocations: paymentAccountId
          ? [{ paymentAccountId, amountYen: Math.abs(entry.amount) }]
          : [],
        items: [
          {
            name: entry.merchant || "カード利用",
            amountYen: Math.abs(entry.amount),
            allocations: null,
          },
        ],
      });
      await refreshPurchases();
      const evidenceResponse = await fetch(
        `/purchases/${encodeURIComponent(created.createPurchase.id)}/evidence`,
        {
          method: "POST",
          credentials: "include",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            sources: [{ documentId: document.id, sourceReference: reference }],
          }),
        },
      );
      setConfirmedStatements((current) => new Set(current).add(key));
      setHasUnsavedChanges(false);
      if (!evidenceResponse.ok) {
        setTargetPurchaseId(created.createPurchase.id);
        setMessage("購入は登録済みです。証拠の紐付けを再試行してください");
        onAdded(true);
        return;
      }
      onAdded(true);
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">証拠と照合</span>
          <h1>取り込み</h1>
          <p>レシート、注文履歴、明細を追加</p>
        </div>
        <div className="import-actions page-import-actions">
          <button
            type="button"
            className="button button-soft"
            onClick={() => cameraInputRef.current?.click()}
          >
            <Camera size={16} />
            撮影
          </button>
          <button
            type="button"
            className="button button-soft"
            onClick={() => inputRef.current?.click()}
          >
            <Upload size={16} />
            ファイルを選ぶ
          </button>
        </div>
      </div>
      <input
        ref={inputRef}
        type="file"
        accept="image/jpeg,image/png,image/webp,application/pdf,.jpg,.jpeg,.png,.webp,.pdf,.csv,.eml,text/csv,message/rfc822"
        multiple
        hidden
        onChange={(event) => {
          addFiles(event.currentTarget.files);
          event.currentTarget.value = "";
        }}
      />
      <input
        ref={cameraInputRef}
        type="file"
        accept="image/*"
        capture="environment"
        hidden
        onChange={(event) => {
          addFiles(event.currentTarget.files);
          event.currentTarget.value = "";
        }}
      />
      <div className="import-layout">
        <section
          className={`dropzone ${dragging ? "dropzone-active" : ""}`}
          onDragOver={(event) => {
            event.preventDefault();
            setDragging(true);
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={(event) => {
            event.preventDefault();
            setDragging(false);
            addFiles(event.dataTransfer.files);
          }}
        >
          <div className="drop-icon">
            <FileImage size={28} />
          </div>
          <h2>購入記録を追加</h2>
          <p>JPEG、PNG、WebP、PDF、CSV、EML</p>
          <div className="import-actions dropzone-import-actions">
            <button
              type="button"
              className="button button-primary"
              onClick={() => cameraInputRef.current?.click()}
            >
              <Camera size={16} />
              撮影
            </button>
            <button
              type="button"
              className="button button-soft"
              onClick={() => inputRef.current?.click()}
            >
              <Upload size={16} />
              ファイルを選ぶ
            </button>
          </div>
        </section>
        <section className="panel import-review">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">確認待ち</span>
              <h2>証拠レビュー</h2>
            </div>
            <span className="count-badge">{documents.length + files.length}</span>
          </div>
          <form
            className="ledger-filters"
            onSubmit={(event) => {
              event.preventDefault();
              setDocumentFilters(documentFilterDraft);
            }}
          >
            <label className="select-filter">
              店舗
              <input
                type="search"
                value={documentFilterDraft.store}
                onChange={(event) =>
                  setDocumentFilterDraft((current) => ({ ...current, store: event.target.value }))
                }
              />
            </label>
            <label className="select-filter">
              取り込み開始日
              <input
                type="date"
                value={documentFilterDraft.from}
                onChange={(event) =>
                  setDocumentFilterDraft((current) => ({ ...current, from: event.target.value }))
                }
              />
            </label>
            <label className="select-filter">
              取り込み終了日
              <input
                type="date"
                value={documentFilterDraft.to}
                onChange={(event) =>
                  setDocumentFilterDraft((current) => ({ ...current, to: event.target.value }))
                }
              />
            </label>
            <label className="select-filter">
              原本種別
              <select
                value={documentFilterDraft.sourceKind}
                onChange={(event) =>
                  setDocumentFilterDraft((current) => ({
                    ...current,
                    sourceKind: event.target.value,
                  }))
                }
              >
                <option value="">すべて</option>
                <option value="receipt">レシート</option>
                <option value="order">注文履歴</option>
                <option value="email">メール</option>
                <option value="card">カード明細</option>
                <option value="bank">銀行明細</option>
              </select>
            </label>
            <label className="select-filter">
              確定状態
              <select
                value={documentFilterDraft.confirmed}
                onChange={(event) =>
                  setDocumentFilterDraft((current) => ({
                    ...current,
                    confirmed: event.target.value,
                  }))
                }
              >
                <option value="">すべて</option>
                <option value="true">確定済み</option>
                <option value="false">未確定</option>
              </select>
            </label>
            <button type="submit" className="button button-soft">
              検索
            </button>
          </form>
          {files.map((upload) => (
            <div className="file-review-row" key={upload.id}>
              <span className="file-thumb">
                <FileImage />
              </span>
              <div>
                <b>{upload.file.name}</b>
                <small>
                  {(upload.file.size / 1024).toFixed(0)} KB ·{" "}
                  {upload.status === "uploading"
                    ? "送信中"
                    : upload.status === "failed"
                      ? `送信失敗 · ${upload.error}`
                      : "送信待ち"}
                </small>
              </div>
              {upload.status === "failed" && <AlertCircle size={16} aria-label="送信失敗" />}
              {isCsvFile(upload.file) && (
                <select
                  aria-label={`${upload.file.name}の明細種別`}
                  value={csvKinds[upload.file.name] ?? "card"}
                  onChange={(event) => {
                    markUnsaved();
                    setCsvKinds((current) => ({
                      ...current,
                      [upload.file.name]: event.target.value as "card" | "bank",
                    }));
                  }}
                >
                  <option value="card">カード明細</option>
                  <option value="bank">銀行明細</option>
                </select>
              )}
              <button
                className="icon-button"
                aria-label="削除"
                disabled={pending}
                onClick={() => {
                  markUnsaved();
                  setFiles((current) => current.filter((entry) => entry.id !== upload.id));
                }}
              >
                <X size={16} />
              </button>
            </div>
          ))}
          {documents.map((document) => (
            <div className="persisted-document-row" key={document.id}>
              <button
                type="button"
                className={`file-review-row review-select ${selected === document.id ? "review-selected" : ""}`}
                onClick={() => setSelected(document.id)}
              >
                <span className="file-thumb">
                  <FileImage />
                </span>
                <div>
                  <b>
                    {document.name ??
                      `${sourceKindLabel(document.sourceKind)} · ${document.id.slice(0, 8)}`}
                  </b>
                  <small>
                    {statusLabel(document.status)}
                    {document.duplicateOf ? " · 重複候補" : ""}
                  </small>
                </div>
                <ChevronRight size={15} />
              </button>
              {document.status.toUpperCase() === "FAILED" && (
                <button
                  type="button"
                  className="text-button"
                  disabled={pending}
                  onClick={() => void retryDocument(document)}
                >
                  再抽出
                </button>
              )}
            </div>
          ))}
          {nextDocumentCursor && (
            <button
              className="button button-soft button-wide"
              type="button"
              disabled={loadingOlderDocuments}
              onClick={() => void loadOlderDocuments()}
            >
              {loadingOlderDocuments ? "読み込み中" : "古い履歴を読み込む"}
            </button>
          )}
          {files.length > 0 && (
            <button
              className="button button-primary button-wide"
              disabled={pending}
              onClick={() => void uploadFiles()}
            >
              <Upload size={15} />
              {pending ? "送信しています" : "証拠を送信"}
            </button>
          )}
          {selectedDocument && (
            <div className="evidence-review-layout">
              <details className="evidence-source-column" open>
                <summary>原本</summary>
                <div className="evidence-preview">
                  <a
                    className="review-image"
                    href={`/documents/${encodeURIComponent(selectedDocument.id)}/content`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    <FileImage size={23} />
                    <span>原本ファイルを開く</span>
                  </a>
                  {selectedDocument.mimeType?.startsWith("image/") ||
                  selectedDocument.mimeType === "application/pdf" ? (
                    <>
                      {selectedDocument.mimeType === "application/pdf" && (
                        <div className="evidence-preview-page-controls">
                          <label>
                            PDFページ
                            <input
                              aria-label="PDFページ"
                              type="number"
                              min="1"
                              step="1"
                              value={previewPageDraft}
                              onChange={(event) => setPreviewPageDraft(event.target.value)}
                              onKeyDown={(event) => {
                                if (event.key === "Enter") showPreviewPage();
                              }}
                            />
                          </label>
                          <button className="text-button" type="button" onClick={showPreviewPage}>
                            ページを表示
                          </button>
                        </div>
                      )}
                      {previewLoading && <p>ページを読み込んでいます</p>}
                      {previewError && <InlineError message={previewError} />}
                      {preview?.documentId === selectedDocument.id &&
                        preview.page === previewPage && (
                          <div className="evidence-image-stage">
                            <img src={preview.src} alt={`${previewPage + 1}ページ目の原本`} />
                            {selectedDocument.result?.receipt?.items?.map((item, index) =>
                              item.region?.page === previewPage ? (
                                <button
                                  key={`item-${index}`}
                                  type="button"
                                  className={`evidence-region ${Number(receiptEvidenceIndex) === index ? "evidence-region-selected" : ""}`}
                                  style={{
                                    clipPath: `polygon(${regionPolygon(item.region, preview.transform)})`,
                                  }}
                                  aria-label={`${item.name}の原本位置`}
                                  onClick={() => {
                                    setReceiptEvidenceIndex(String(index));
                                    selectCropRegion(item.region!);
                                  }}
                                />
                              ) : null,
                            )}
                            {selectedDocument.result?.ocrLines?.map((line, index) =>
                              line.region?.page === previewPage ? (
                                <button
                                  key={`line-${index}`}
                                  type="button"
                                  className="evidence-region evidence-region-ocr"
                                  style={{
                                    clipPath: `polygon(${regionPolygon(line.region, preview.transform)})`,
                                  }}
                                  aria-label={`${line.text}の原本位置`}
                                  onClick={() => selectCropRegion(line.region!)}
                                />
                              ) : null,
                            )}
                          </div>
                        )}
                    </>
                  ) : null}
                </div>
                {(selectedDocument.result?.receipt?.items?.length ||
                  selectedDocument.result?.ocrLines?.some((line) => line.region)) && (
                  <section className="panel evidence-crop-panel">
                    <div className="panel-heading">
                      <div>
                        <span className="eyebrow">原本座標</span>
                        <h3>範囲の再抽出</h3>
                      </div>
                      <span>{cropRegion ? `ページ ${cropRegion.page + 1}` : "範囲未選択"}</span>
                    </div>
                    <div className="evidence-source-items">
                      {selectedDocument?.result?.receipt?.items?.map((item, index) => (
                        <button
                          type="button"
                          key={index}
                          className={`text-button ${Number(receiptEvidenceIndex) === index ? "evidence-item-selected" : ""}`}
                          aria-pressed={Number(receiptEvidenceIndex) === index}
                          disabled={!item.region}
                          onClick={() => {
                            setReceiptEvidenceIndex(String(index));
                            if (item.region) selectCropRegion(item.region);
                          }}
                        >
                          {item.name} · {yen(item.grossAmount ?? item.printedAmount)}
                        </button>
                      ))}
                    </div>
                    {!!selectedDocument?.result?.ocrLines?.length && (
                      <div className="evidence-ocr-lines">
                        {selectedDocument.result.ocrLines
                          .filter((line) => line.region)
                          .slice(0, 80)
                          .map((line, index) => (
                            <button
                              type="button"
                              className="text-button"
                              key={`${line.text}-${index}`}
                              onClick={() => selectCropRegion(line.region!)}
                            >
                              {line.text} · ページ {line.region!.page + 1}
                            </button>
                          ))}
                      </div>
                    )}
                    {cropRegion && (
                      <div className="crop-coordinate-fields">
                        {(["top", "left", "bottom", "right"] as const).map((key) => (
                          <label key={key}>
                            {{ top: "上", left: "左", bottom: "下", right: "右" }[key]}
                            <input
                              type="number"
                              min="0"
                              max="1"
                              step="0.01"
                              value={cropRegion[key]}
                              onChange={(event) => {
                                markUnsaved();
                                setCropRegion((current) =>
                                  current
                                    ? { ...current, [key]: Number(event.target.value) }
                                    : current,
                                );
                              }}
                            />
                          </label>
                        ))}
                        <label>
                          ページ
                          <input
                            type="number"
                            min="1"
                            step="1"
                            value={cropRegion.page + 1}
                            onChange={(event) => {
                              markUnsaved();
                              const page = Math.max(0, Math.trunc(Number(event.target.value) - 1));
                              setCropRegion((current) =>
                                current ? { ...current, page } : current,
                              );
                              setPreviewPage(page);
                              setPreviewPageDraft(String(page + 1));
                            }}
                          />
                        </label>
                        <button
                          className="button button-soft"
                          type="button"
                          disabled={
                            pending ||
                            cropPending ||
                            cropRegion.bottom <= cropRegion.top ||
                            cropRegion.right <= cropRegion.left
                          }
                          onClick={() => void retryDocumentCrop(selectedDocument!)}
                        >
                          {cropPending ? "抽出中" : "この範囲で再抽出"}
                        </button>
                      </div>
                    )}
                  </section>
                )}
              </details>
              <div className="evidence-edit-column">
                {!!selectedDocument.result?.receipt?.items?.length && (
                  <div className="receipt-review">
                    {reviewTargets.length > 0 && (
                      <section className="panel-heading" aria-label="確認順">
                        <div aria-live="polite">
                          <span className="eyebrow">
                            確認 {activeReviewIndex + 1}/{reviewTargets.length}
                          </span>
                          <h3>{activeReviewTarget?.label}</h3>
                        </div>
                        <div style={{ display: "flex", gap: 8 }}>
                          <button
                            type="button"
                            className="button button-soft"
                            disabled={activeReviewIndex === 0}
                            onClick={() => setReviewTargetIndex((index) => index - 1)}
                          >
                            前へ
                          </button>
                          <button
                            type="button"
                            className="button button-soft"
                            disabled={activeReviewIndex >= reviewTargets.length - 1}
                            onClick={() => setReviewTargetIndex((index) => index + 1)}
                          >
                            次へ
                          </button>
                        </div>
                      </section>
                    )}
                    <div className="review-fields">
                      <div className="review-merchant">
                        <label>
                          店名
                          <input
                            value={selectedDocument.result.receipt.merchant ?? ""}
                            onChange={(event) => {
                              editedReceiptDocuments.current.add(selectedDocument.id);
                              markUnsaved();
                              setDocuments((current) =>
                                current.map((document) =>
                                  document.id === selectedDocument.id && document.result
                                    ? {
                                        ...document,
                                        result: {
                                          ...document.result,
                                          receipt: {
                                            ...document.result.receipt,
                                            merchant: event.target.value,
                                          },
                                        },
                                      }
                                    : document,
                                ),
                              );
                            }}
                          />
                        </label>
                      </div>
                      <div className="field-group" id="receipt-review-payments" tabIndex={-1}>
                        <label>決済内訳</label>
                        {paymentDraftFor(selectedDocument).map((payment, index) => (
                          <div className="review-merchant" key={payment.key}>
                            {(payment.method || payment.cardLast4) && (
                              <small>
                                {payment.method}
                                {payment.cardLast4 ? ` · •••• ${payment.cardLast4}` : ""}
                              </small>
                            )}
                            <select
                              aria-label={`決済${index + 1}の口座`}
                              value={payment.paymentAccountId}
                              onChange={(event) => {
                                markUnsaved();
                                setReceiptPayments((current) => ({
                                  ...current,
                                  [selectedDocument.id]: paymentDraftFor(selectedDocument).map(
                                    (entry) =>
                                      entry.key === payment.key
                                        ? { ...entry, paymentAccountId: event.target.value }
                                        : entry,
                                  ),
                                }));
                              }}
                            >
                              <option value="">口座を選択</option>
                              {household.accounts.map((account) => (
                                <option value={account.id} key={account.id}>
                                  {account.name}
                                </option>
                              ))}
                            </select>
                            <input
                              aria-label={`決済${index + 1}の金額`}
                              inputMode="numeric"
                              type="number"
                              min="0"
                              step="1"
                              value={payment.amountYen}
                              onChange={(event) => {
                                markUnsaved();
                                setReceiptPayments((current) => ({
                                  ...current,
                                  [selectedDocument.id]: paymentDraftFor(selectedDocument).map(
                                    (entry) =>
                                      entry.key === payment.key
                                        ? { ...entry, amountYen: Number(event.target.value) || 0 }
                                        : entry,
                                  ),
                                }));
                              }}
                            />
                            <button
                              className="text-button"
                              type="button"
                              aria-label={`決済${index + 1}を削除`}
                              disabled={paymentDraftFor(selectedDocument).length === 1}
                              onClick={() => {
                                markUnsaved();
                                setReceiptPayments((current) => ({
                                  ...current,
                                  [selectedDocument.id]: paymentDraftFor(selectedDocument).filter(
                                    (entry) => entry.key !== payment.key,
                                  ),
                                }));
                              }}
                            >
                              <X size={16} />
                            </button>
                          </div>
                        ))}
                        <button
                          className="text-button"
                          type="button"
                          disabled={household.accounts.length < 2}
                          onClick={() => {
                            markUnsaved();
                            const current = paymentDraftFor(selectedDocument);
                            const total = receiptTotal(selectedDocument);
                            const rows =
                              current.length === 1 && current[0].amountYen === total
                                ? [
                                    { ...current[0], amountYen: Math.floor(total / 2) },
                                    {
                                      key: crypto.randomUUID(),
                                      paymentAccountId: "",
                                      amountYen: total - Math.floor(total / 2),
                                    },
                                  ]
                                : [
                                    ...current,
                                    {
                                      key: crypto.randomUUID(),
                                      paymentAccountId: "",
                                      amountYen: 0,
                                    },
                                  ];
                            setReceiptPayments((previous) => ({
                              ...previous,
                              [selectedDocument.id]: rows,
                            }));
                          }}
                        >
                          <Check size={14} />
                          {paymentDraftFor(selectedDocument).length === 1
                            ? "決済を分割"
                            : "決済を追加"}
                        </button>
                        <small>
                          決済合計{" "}
                          {yen(
                            paymentDraftFor(selectedDocument).reduce(
                              (total, payment) => total + payment.amountYen,
                              0,
                            ),
                          )}{" "}
                          購入額 {yen(receiptTotal(selectedDocument))}
                        </small>
                      </div>
                      {selectedDocument.result.receipt.items.map((item, index) => (
                        <div className="receipt-item-review" key={index}>
                          <div>
                            <input
                              id={`receipt-review-item-${index}`}
                              aria-label="品名"
                              value={item.name}
                              onChange={(event) =>
                                editItem(selectedDocument.id, index, { name: event.target.value })
                              }
                            />
                            <small>
                              {item.verification} · {Math.round(item.confidence * 100)}%
                            </small>
                            <select
                              aria-label={`${item.name}のカテゴリ`}
                              value={
                                item.categoryId != null
                                  ? item.categoryId
                                  : suggestedCategoryId(item)
                              }
                              onChange={(event) =>
                                editItem(selectedDocument.id, index, {
                                  categoryId: event.target.value,
                                })
                              }
                            >
                              <option value="">カテゴリを確認</option>
                              {household.categories
                                .filter((category) => !category.archived)
                                .map((category) => (
                                  <option value={category.id} key={category.id}>
                                    {category.name}
                                  </option>
                                ))}
                            </select>
                            <ProductVariantSelect
                              householdId={household.id}
                              itemName={item.name}
                              merchant={selectedDocument.result?.receipt?.merchant ?? ""}
                              value={item.productVariantId ?? ""}
                              onChange={(productVariantId) =>
                                editItem(selectedDocument.id, index, {
                                  productVariantId: productVariantId || null,
                                })
                              }
                            />
                          </div>
                          <label>
                            ¥
                            <input
                              inputMode="numeric"
                              value={String(item.grossAmount ?? item.printedAmount)}
                              onChange={(event) => {
                                markUnsaved();
                                setManualAmountOverrides((current) => ({
                                  ...current,
                                  [selectedDocument.id]: true,
                                }));
                                editItem(selectedDocument.id, index, {
                                  grossAmount: Number(event.target.value.replace(/\D/g, "")),
                                });
                              }}
                            />
                          </label>
                          <details className="receipt-measure-details">
                            <summary>数量・印字単価</summary>
                            <label>
                              数量の分子
                              <input
                                inputMode="numeric"
                                value={item.quantityNumerator ?? ""}
                                onChange={(event) =>
                                  editItem(selectedDocument.id, index, {
                                    quantityNumerator: event.target.value || null,
                                  })
                                }
                              />
                            </label>
                            <label>
                              数量の分母
                              <input
                                inputMode="numeric"
                                value={item.quantityDenominator ?? ""}
                                onChange={(event) =>
                                  editItem(selectedDocument.id, index, {
                                    quantityDenominator: event.target.value || null,
                                  })
                                }
                              />
                            </label>
                            <label>
                              数量単位
                              <input
                                value={item.quantityUnit ?? ""}
                                onChange={(event) =>
                                  editItem(selectedDocument.id, index, {
                                    quantityUnit: event.target.value || null,
                                  })
                                }
                              />
                            </label>
                            <label>
                              印字単価
                              <input
                                inputMode="numeric"
                                value={item.printedUnitPriceYen ?? ""}
                                onChange={(event) =>
                                  editItem(selectedDocument.id, index, {
                                    printedUnitPriceYen: event.target.value
                                      ? Number(event.target.value.replace(/\D/g, ""))
                                      : null,
                                  })
                                }
                              />
                            </label>
                            <label>
                              基準量の分子
                              <input
                                inputMode="numeric"
                                value={item.printedUnitPriceQuantityNumerator ?? ""}
                                onChange={(event) =>
                                  editItem(selectedDocument.id, index, {
                                    printedUnitPriceQuantityNumerator: event.target.value || null,
                                  })
                                }
                              />
                            </label>
                            <label>
                              基準量の分母
                              <input
                                inputMode="numeric"
                                value={item.printedUnitPriceQuantityDenominator ?? ""}
                                onChange={(event) =>
                                  editItem(selectedDocument.id, index, {
                                    printedUnitPriceQuantityDenominator: event.target.value || null,
                                  })
                                }
                              />
                            </label>
                            <label>
                              印字単位
                              <input
                                value={item.printedUnitPriceUnit ?? ""}
                                onChange={(event) =>
                                  editItem(selectedDocument.id, index, {
                                    printedUnitPriceUnit: event.target.value || null,
                                  })
                                }
                              />
                            </label>
                          </details>
                        </div>
                      ))}
                      {!!selectedDocument.result.candidates?.length && (
                        <div
                          className="candidate-review"
                          id="receipt-review-candidates"
                          tabIndex={-1}
                        >
                          <div className="panel-heading">
                            <div>
                              <span className="eyebrow">金額の解釈</span>
                              <h3>計算候補</h3>
                            </div>
                          </div>
                          {selectedDocument.result.candidates.map((candidate) => (
                            <button
                              type="button"
                              className={`candidate-option ${selectedCandidates[selectedDocument.id] === candidate.id ? "candidate-selected" : ""}`}
                              key={candidate.id}
                              onClick={() => {
                                markUnsaved();
                                setSelectedCandidates((current) => ({
                                  ...current,
                                  [selectedDocument.id]: candidate.id,
                                }));
                              }}
                            >
                              <span>
                                <b>{candidate.id || "候補"}</b>
                                <small>
                                  {candidate.consistent ? "合計一致" : "合計差異"}
                                  {candidate.issues.length
                                    ? ` · ${candidate.issues.join("、")}`
                                    : ""}
                                </small>
                              </span>
                              <strong>{yen(candidate.computedTotal)}</strong>
                            </button>
                          ))}
                          <small>品目の印字額は保持されます。選んだ計算額を帳簿に使います。</small>
                        </div>
                      )}
                      {(manualAmountOverrides[selectedDocument.id] ||
                        (selectedCandidates[selectedDocument.id]
                          ? !selectedDocument.result.candidates?.find(
                              (candidate) =>
                                candidate.id === selectedCandidates[selectedDocument.id],
                            )?.consistent
                          : !selectedDocument.result.balanced)) && (
                        <label
                          className="field-group"
                          id="receipt-review-amount-reason"
                          tabIndex={-1}
                        >
                          金額の訂正理由
                          <input
                            maxLength={500}
                            value={manualAmountOverrideReasons[selectedDocument.id] ?? ""}
                            onChange={(event) => {
                              markUnsaved();
                              setManualAmountOverrideReasons((current) => ({
                                ...current,
                                [selectedDocument.id]: event.target.value,
                              }));
                            }}
                          />
                        </label>
                      )}
                      <div className="review-total">
                        <span>認識した合計</span>
                        <b>
                          {yen(
                            selectedDocument.result.candidates?.find(
                              (candidate) =>
                                candidate.id === selectedCandidates[selectedDocument.id],
                            )?.computedTotal ?? selectedDocument.result.computedTotal,
                          )}
                        </b>
                      </div>
                      <button
                        className="button button-primary button-wide"
                        disabled={pending}
                        onClick={() => void confirm(selectedDocument)}
                      >
                        <Check size={16} />
                        帳簿に追加
                      </button>
                    </div>
                  </div>
                )}
              </div>
            </div>
          )}
          {selectedDocument?.result?.statements && (
            <div className="statement-review">
              <div className="panel-heading">
                <div>
                  <span className="eyebrow">明細行</span>
                  <h3>{selectedDocument.result.statements.length}件</h3>
                </div>
              </div>
              {selectedDocument.result.statements.map((entry) => (
                <div
                  className="statement-row"
                  key={`${entry.row}-${entry.transactionId ?? entry.occurredAt}`}
                >
                  <div>
                    <b>{entry.merchant || "利用先未入力"}</b>
                    <small>
                      {entry.occurredAt.slice(0, 10)} ·{" "}
                      {entry.kind === "BANK_MOVEMENT" ? "入出金" : "カード利用"}
                      {entry.cardLast4 ? ` · •••• ${entry.cardLast4}` : ""}
                    </small>
                  </div>
                  <div className="statement-row-action">
                    <strong>{yen(entry.amount)}</strong>
                    {entry.kind === "CARD_PURCHASE" && (
                      <button
                        className="text-button"
                        disabled={
                          pending ||
                          confirmedStatements.has(
                            `${selectedDocument.id}:${entry.transactionId ? `transaction:${entry.transactionId}` : `row:${entry.row}`}`,
                          )
                        }
                        onClick={() => void confirmStatement(selectedDocument, entry)}
                      >
                        {confirmedStatements.has(
                          `${selectedDocument.id}:${entry.transactionId ? `transaction:${entry.transactionId}` : `row:${entry.row}`}`,
                        )
                          ? "追加済み"
                          : "購入に追加"}
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
          {selectedDocument?.result && observations.length > 0 && (
            <section className="panel evidence-group-panel">
              <div className="panel-heading">
                <div>
                  <span className="eyebrow">照合候補</span>
                  <h3>購入へまとめる証拠</h3>
                </div>
                <Link2 size={18} />
              </div>
              {matchesTruncated && (
                <small role="status">
                  <AlertCircle aria-hidden size={14} /> 候補を直近100件に絞っています
                </small>
              )}
              <label className="select-filter">
                <span>照合元</span>
                <select
                  value={activeObservationRef}
                  onChange={(event) => {
                    markUnsaved();
                    setActiveObservationRef(event.target.value);
                  }}
                >
                  {selectedDocument.result.receipt?.items?.length ? (
                    <option value="">レシート全体</option>
                  ) : null}
                  {observations.map((observation) => {
                    const statement = selectedDocument.result?.statements?.find(
                      (entry) =>
                        (entry.transactionId
                          ? `transaction:${entry.transactionId}`
                          : `row:${entry.row}`) === observation.sourceReference,
                    );
                    return (
                      <option key={observation.sourceReference} value={observation.sourceReference}>
                        {statement?.merchant || observation.sourceReference || "レシート"}
                      </option>
                    );
                  })}
                </select>
              </label>
              <div className="evidence-match-list">
                {observations.map((observation) => (
                  <div className="evidence-match-group" key={observation.sourceReference}>
                    <small>{observation.sourceReference || "レシート全体"}</small>
                    {observation.matches.map((candidate) => {
                      const key = `${observation.sourceReference}\u0000${candidate.documentId}\u0000${candidate.sourceReference}`;
                      const sourceDocument = documents.find(
                        (document) => document.id === candidate.documentId,
                      );
                      return (
                        <label className="evidence-match-row" key={key}>
                          <input
                            type="checkbox"
                            checked={selectedMatches.has(key)}
                            onChange={(event) => {
                              markUnsaved();
                              setSelectedMatches((current) => {
                                const next = new Set(current);
                                if (event.target.checked) next.add(key);
                                else next.delete(key);
                                return next;
                              });
                            }}
                          />
                          <span>
                            <b>
                              {sourceKindLabel(sourceDocument?.sourceKind)} ·{" "}
                              {candidate.sourceReference ||
                                sourceDocument?.result?.receipt?.merchant ||
                                "レシート"}
                            </b>
                            <small>
                              {candidate.state} · {candidate.reasons.join("、")}
                            </small>
                          </span>
                        </label>
                      );
                    })}
                  </div>
                ))}
              </div>
              <label className="select-filter">
                <span>購入先</span>
                <select
                  value={targetPurchaseId}
                  onChange={(event) => {
                    markUnsaved();
                    setTargetPurchaseId(event.target.value);
                    setTargetItemId("");
                  }}
                >
                  <option value="">購入を選択</option>
                  {recentPurchases.map((purchase) => (
                    <option key={purchase.id} value={purchase.id}>
                      {purchase.occurredAt.slice(0, 10)} · {purchase.merchant} ·{" "}
                      {yen(purchase.totalYen)}
                    </option>
                  ))}
                </select>
              </label>
              {targetPurchase && (
                <label className="select-filter">
                  <span>紐付ける品目</span>
                  <select
                    value={targetItemId}
                    onChange={(event) => {
                      markUnsaved();
                      setTargetItemId(event.target.value);
                    }}
                  >
                    <option value="">購入全体</option>
                    {targetPurchase.items.map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.name} · {yen(item.amountYen)}
                      </option>
                    ))}
                  </select>
                </label>
              )}
              {selectedDocument.result.receipt?.items?.length ? (
                <label className="select-filter">
                  <span>照合するレシート品目</span>
                  <select
                    value={receiptEvidenceIndex}
                    onChange={(event) => {
                      markUnsaved();
                      setReceiptEvidenceIndex(event.target.value);
                    }}
                  >
                    {selectedDocument.result.receipt.items.map((item, index) => (
                      <option key={index} value={index}>
                        {item.name}
                      </option>
                    ))}
                  </select>
                </label>
              ) : null}
              <button
                className="button button-primary button-wide"
                type="button"
                disabled={pending || !targetPurchaseId}
                onClick={() => void confirmEvidenceGroup()}
              >
                <Link2 size={15} />
                選択した証拠を紐付け
              </button>
            </section>
          )}
          {message && <InlineError message={message} />}
        </section>
      </div>
      {message && <InlineError message={message} />}
      <div className="import-note">
        <ShieldCheck size={17} />
        <span>原本は帳簿の品目と紐づけて確認できます</span>
      </div>
    </>
  );
}
