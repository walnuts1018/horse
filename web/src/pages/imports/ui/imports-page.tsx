import { useCallback, useEffect, useRef, useState } from "react";
import { Check, ChevronRight, FileImage, Link2, ShieldCheck, Upload, X } from "lucide-react";
import {
  createPurchaseMutation,
  makeIdempotencyKey,
  readGraphql,
  type Household,
  type Purchase,
} from "../../../shared/api";
import { InlineError } from "../../../shared/ui";
import { useHouseholdPurchases } from "../../../shared/api";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const sourceKind = (file: File, csvKind: "card" | "bank") =>
  /\.csv$/i.test(file.name) ? csvKind : /\.eml$/i.test(file.name) ? "email" : "receipt";

type ExtractedItem = {
  name: string;
  printedAmount: number;
  grossAmount: number;
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
    receipt?: { merchant?: string; purchasedAt?: string; items?: ExtractedItem[] };
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

export function ImportsPage({
  household,
  onAdded,
}: {
  household: Household;
  onAdded: (keepOpen?: boolean) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [files, setFiles] = useState<File[]>([]);
  const [csvKinds, setCsvKinds] = useState<Record<string, "card" | "bank">>({});
  const [documents, setDocuments] = useState<DocumentReview[]>([]);
  const [dragging, setDragging] = useState(false);
  const [message, setMessage] = useState("");
  const [pending, setPending] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [confirmedStatements, setConfirmedStatements] = useState<Set<string>>(() => new Set());
  const [selectedCandidates, setSelectedCandidates] = useState<Record<string, string>>({});
  const [paymentAccountId, setPaymentAccountId] = useState(household.accounts[0]?.id ?? "");
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
        const response = await fetch(
          `/documents?householdId=${encodeURIComponent(household.id)}&first=50`,
          { credentials: "include" },
        );
        if (!response.ok) throw new Error(`証拠一覧を取得できませんでした (${response.status})`);
        const payload = (await response.json()) as { documents: DocumentReview[] };
        setDocuments((current) =>
          payload.documents.map((document) => ({
            ...document,
            name:
              current.find((existing) => existing.id === document.id)?.name ??
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
        );
        setSelected((current) => current ?? payload.documents[0]?.id ?? null);
      } catch (reason) {
        if (!silent) setMessage((reason as Error).message);
      }
    },
    [household.id],
  );
  useEffect(() => {
    setDocuments([]);
    setSelected(null);
    setConfirmedStatements(new Set());
    setSelectedCandidates({});
    void refreshDocuments();
  }, [refreshDocuments]);
  const hasProcessingDocuments = documents.some((document) => isProcessing(document.status));
  useEffect(() => {
    if (!hasProcessingDocuments) return;
    const interval = window.setInterval(() => void refreshDocuments(true), 1200);
    return () => window.clearInterval(interval);
  }, [hasProcessingDocuments, refreshDocuments]);
  useEffect(() => {
    if (!selectedDocument || !selectedDocument.result) {
      setObservations([]);
      setSelectedMatches(new Set());
      return;
    }
    let active = true;
    setObservations([]);
    setSelectedMatches(new Set());
    void fetch(`/documents/${encodeURIComponent(selectedDocument.id)}/matches`, {
      credentials: "include",
    })
      .then(async (response) => {
        if (!response.ok) throw new Error(`照合候補を取得できませんでした (${response.status})`);
        return (await response.json()) as { observations: ObservationCandidates[] };
      })
      .then((payload) => {
        if (!active) return;
        setObservations(payload.observations);
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
    setFiles((current) => [
      ...current,
      ...Array.from(selected).filter(
        (file) =>
          file.type.startsWith("image/") ||
          file.type === "application/pdf" ||
          file.type === "text/csv" ||
          file.type === "application/vnd.ms-excel" ||
          file.type === "message/rfc822" ||
          /\.(csv|eml)$/i.test(file.name),
      ),
    ]);
  };
  const uploadFiles = async () => {
    if (!files.length) return;
    setPending(true);
    setMessage("");
    const uploaded: DocumentReview[] = [];
    try {
      for (const file of files) {
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
          sha256: string;
          status: string;
          duplicateOf?: string;
          duplicate?: boolean;
          jobId?: string;
        };
        let documentReview: DocumentReview = {
          id: accepted.id,
          name: file.name,
          status: accepted.status,
          sourceKind: sourceKind(file, csvKinds[file.name] ?? "card"),
          duplicateOf: accepted.duplicateOf,
          sha256: accepted.sha256,
        };
        for (let attempt = 0; attempt < 24 && isProcessing(documentReview.status); attempt++) {
          await new Promise((resolve) => window.setTimeout(resolve, 1000));
          const resultResponse = await fetch(`/documents/${encodeURIComponent(accepted.id)}`, {
            credentials: "include",
          });
          if (!resultResponse.ok)
            throw new Error(`認識結果を取得できませんでした (${resultResponse.status})`);
          documentReview = {
            ...documentReview,
            ...((await resultResponse.json()) as Partial<DocumentReview>),
            name: file.name,
          };
        }
        uploaded.push(documentReview);
      }
      setDocuments((current) => [...uploaded, ...current]);
      setFiles([]);
      setSelected(uploaded.find((document) => document.result)?.id ?? uploaded[0]?.id ?? null);
      void refreshDocuments(true);
    } catch (reason) {
      setMessage((reason as Error).message);
      setDocuments((current) => [...uploaded, ...current]);
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
        body: JSON.stringify({ hints: [] }),
      });
      if (!response.ok) throw new Error(`再抽出を開始できませんでした (${response.status})`);
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
        body: JSON.stringify({ hints: [], crop: cropRegion }),
      });
      if (!response.ok) throw new Error(`範囲を再抽出できませんでした (${response.status})`);
      const accepted = (await response.json()) as { jobId: string; status: string };
      setDocuments((current) =>
        current.map((entry) =>
          entry.id === document.id
            ? { ...entry, jobId: accepted.jobId, status: accepted.status, result: undefined }
            : entry,
        ),
      );
      await refreshDocuments(true);
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setCropPending(false);
    }
  };
  const selectCropRegion = (region: Region) => {
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
      await refreshDocuments(true);
      onAdded(true);
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const editItem = (documentId: string, index: number, patch: Partial<ExtractedItem>) =>
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
                  items: document.result.receipt.items.map((item, itemIndex) =>
                    itemIndex === index ? { ...item, ...patch } : item,
                  ),
                },
              },
            },
      ),
    );
  const confirm = async (document: DocumentReview) => {
    const receipt = document.result?.receipt;
    const items = receipt?.items ?? [];
    if (!items.length) {
      setMessage("確定できる品目がありません");
      return;
    }
    if (document.result?.candidates?.length && !selectedCandidates[document.id]) {
      setMessage("税計算候補を選択してください");
      return;
    }
    setPending(true);
    setMessage("");
    try {
      const created = await readGraphql<{ createPurchase: Purchase }>(createPurchaseMutation, {
        input: {
          idempotencyKey: makeIdempotencyKey(),
          householdId: household.id,
          sourceDocumentId: document.id,
          occurredAt: receipt?.purchasedAt
            ? new Date(receipt.purchasedAt).toISOString()
            : new Date().toISOString(),
          merchant: receipt?.merchant ?? "",
          paymentAccountId: paymentAccountId || null,
          items: items.map((item, index) => ({
            name: item.name,
            amountYen:
              document.result?.candidates?.find(
                (candidate) => candidate.id === selectedCandidates[document.id],
              )?.amounts[index] ??
              (item.grossAmount || item.printedAmount),
            allocations: null,
          })),
        },
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
      const created = await readGraphql<{ createPurchase: Purchase }>(createPurchaseMutation, {
        input: {
          idempotencyKey: makeIdempotencyKey(),
          householdId: household.id,
          sourceDocumentId: document.id,
          sourceReference: reference,
          occurredAt: new Date(entry.occurredAt).toISOString(),
          merchant: entry.merchant,
          paymentAccountId: paymentAccountId || null,
          items: [
            {
              name: entry.merchant || "カード利用",
              amountYen: Math.abs(entry.amount),
              allocations: null,
            },
          ],
        },
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
        <button className="button button-soft" onClick={() => inputRef.current?.click()}>
          <Upload size={16} />
          ファイルを選ぶ
        </button>
      </div>
      <input
        ref={inputRef}
        type="file"
        accept="image/*,application/pdf,.csv,.eml,text/csv,message/rfc822"
        capture="environment"
        multiple
        hidden
        onChange={(event) => addFiles(event.currentTarget.files)}
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
          <p>画像、PDF、CSV、メール</p>
          <button className="button button-primary" onClick={() => inputRef.current?.click()}>
            <Upload size={16} />
            ファイルを選ぶ
          </button>
          <span className="drop-caption">スマートフォンではカメラを使えます</span>
        </section>
        <section className="panel import-review">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">確認待ち</span>
              <h2>証拠レビュー</h2>
            </div>
            <span className="count-badge">{documents.length + files.length}</span>
          </div>
          {files.map((file, index) => (
            <div className="file-review-row" key={`${file.name}-${index}`}>
              <span className="file-thumb">
                <FileImage />
              </span>
              <div>
                <b>{file.name}</b>
                <small>{(file.size / 1024).toFixed(0)} KB · 送信待ち</small>
              </div>
              {/\.csv$/i.test(file.name) && (
                <select
                  aria-label={`${file.name}の明細種別`}
                  value={csvKinds[file.name] ?? "card"}
                  onChange={(event) =>
                    setCsvKinds((current) => ({
                      ...current,
                      [file.name]: event.target.value as "card" | "bank",
                    }))
                  }
                >
                  <option value="card">カード明細</option>
                  <option value="bank">銀行明細</option>
                </select>
              )}
              <button
                className="icon-button"
                aria-label="削除"
                onClick={() =>
                  setFiles((current) => current.filter((_, fileIndex) => fileIndex !== index))
                }
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
          {files.length > 0 && (
            <button
              className="button button-primary button-wide"
              disabled={pending}
              onClick={() => void uploadFiles()}
            >
              <Upload size={15} />
              {pending ? "認識しています" : "証拠を送信"}
            </button>
          )}
          {selectedDocument && (
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
                  {preview?.documentId === selectedDocument.id && preview.page === previewPage && (
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
          )}
          {(selectedDocument?.result?.receipt?.items?.length ||
            selectedDocument?.result?.ocrLines?.some((line) => line.region)) && (
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
                    {item.name} · {yen(item.grossAmount || item.printedAmount)}
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
                        onChange={(event) =>
                          setCropRegion((current) =>
                            current ? { ...current, [key]: Number(event.target.value) } : current,
                          )
                        }
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
                        const page = Math.max(0, Math.trunc(Number(event.target.value) - 1));
                        setCropRegion((current) => (current ? { ...current, page } : current));
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
          {!!selectedDocument?.result?.receipt?.items?.length && (
            <div className="receipt-review">
              <div className="review-fields">
                <div className="review-merchant">
                  <label>
                    店名
                    <input
                      value={selectedDocument.result.receipt.merchant ?? ""}
                      onChange={(event) =>
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
                        )
                      }
                    />
                  </label>
                  <label>
                    決済元
                    <select
                      value={paymentAccountId}
                      onChange={(event) => setPaymentAccountId(event.target.value)}
                    >
                      <option value="">未設定</option>
                      {household.accounts.map((account) => (
                        <option value={account.id} key={account.id}>
                          {account.name}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>
                {selectedDocument.result.receipt.items.map((item, index) => (
                  <div className="receipt-item-review" key={index}>
                    <div>
                      <input
                        aria-label="品名"
                        value={item.name}
                        onChange={(event) =>
                          editItem(selectedDocument.id, index, { name: event.target.value })
                        }
                      />
                      <small>
                        {item.verification} · {Math.round(item.confidence * 100)}%
                      </small>
                    </div>
                    <label>
                      ¥
                      <input
                        inputMode="numeric"
                        value={String(item.grossAmount || item.printedAmount)}
                        onChange={(event) =>
                          editItem(selectedDocument.id, index, {
                            grossAmount: Number(event.target.value.replace(/\D/g, "")),
                          })
                        }
                      />
                    </label>
                  </div>
                ))}
                {!!selectedDocument.result.candidates?.length && (
                  <div className="candidate-review">
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
                        onClick={() =>
                          setSelectedCandidates((current) => ({
                            ...current,
                            [selectedDocument.id]: candidate.id,
                          }))
                        }
                      >
                        <span>
                          <b>{candidate.id || "候補"}</b>
                          <small>
                            {candidate.consistent ? "合計一致" : "合計差異"}
                            {candidate.issues.length ? ` · ${candidate.issues.join("、")}` : ""}
                          </small>
                        </span>
                        <strong>{yen(candidate.computedTotal)}</strong>
                      </button>
                    ))}
                    <small>品目の印字額は保持されます。選んだ計算額を帳簿に使います。</small>
                  </div>
                )}
                <div className="review-total">
                  <span>認識した合計</span>
                  <b>
                    {yen(
                      selectedDocument.result.candidates?.find(
                        (candidate) => candidate.id === selectedCandidates[selectedDocument.id],
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
              <label className="select-filter">
                <span>照合元</span>
                <select
                  value={activeObservationRef}
                  onChange={(event) => setActiveObservationRef(event.target.value)}
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
                            onChange={(event) =>
                              setSelectedMatches((current) => {
                                const next = new Set(current);
                                if (event.target.checked) next.add(key);
                                else next.delete(key);
                                return next;
                              })
                            }
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
                    onChange={(event) => setTargetItemId(event.target.value)}
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
                    onChange={(event) => setReceiptEvidenceIndex(event.target.value)}
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
