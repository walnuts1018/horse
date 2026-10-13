import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "@tanstack/react-router";
import { Camera, FileImage, ShieldCheck, Upload } from "lucide-react";
import styles from "./imports-page.module.css";
import { type Household } from "../../../shared/api";
import { useCreateAndPostPurchase } from "../../../entities/purchase";
import { InlineError } from "../../../shared/ui";
import { useHouseholdPurchases } from "../../../entities/purchase";
import type { Region } from "../model/evidence-geometry";
import { useEvidencePreview } from "../model/evidence-preview";
import { CameraCapture, type CameraCaptureHandle } from "./camera-capture";
import { ReceiptReview } from "./receipt-review";
import { ImportDocumentList } from "./import-document-list";
import { StatementReview } from "./statement-review";
import { EvidenceMatching } from "./evidence-matching";
import { EvidenceSourcePreview } from "./evidence-source-preview";
import { documentListURL, importIdempotencyKey } from "../api/documents";
import {
  isProcessing,
  isSupportedFile,
  defaultDocumentFilters,
  parseDocumentFilters,
  serializeDocumentFilters,
  sourceKind,
  sourceKindLabel,
  type DocumentFilters,
  type DocumentReview,
  type ExtractedItem,
  type PendingUpload,
  type ReceiptPaymentDraft,
  type ReviewTarget,
  type StatementEntry,
} from "../model/imports";

export function ImportsPage({
  household,
  onAdded,
  onUnsavedChanges,
}: {
  household: Household;
  onAdded: (keepOpen?: boolean) => void;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  const router = useRouter();
  const createAndPostPurchase = useCreateAndPostPurchase();
  const inputRef = useRef<HTMLInputElement>(null);
  const cameraRef = useRef<CameraCaptureHandle>(null);
  const [files, setFiles] = useState<PendingUpload[]>([]);
  const editedReceiptDocuments = useRef(new Set<string>());
  const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false);
  const [csvKinds, setCsvKinds] = useState<Record<string, "card" | "bank">>({});
  const [documents, setDocuments] = useState<DocumentReview[]>([]);
  const [documentFilters, setDocumentFilters] = useState<DocumentFilters>(() =>
    typeof window === "undefined"
      ? defaultDocumentFilters
      : parseDocumentFilters(new URLSearchParams(window.location.search)),
  );
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
  const { purchases: recentPurchases, refresh: refreshPurchases } = useHouseholdPurchases(
    household.id,
    100,
  );
  const selectedDocument = documents.find((document) => document.id === selected);
  const evidencePreview = useEvidencePreview(selectedDocument);
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
  const applyDocumentFilters = (filters: DocumentFilters) => {
    setDocumentFilters(filters);
    setDocumentFilterDraft(filters);
    const url = new URL(window.location.href);
    url.search = serializeDocumentFilters(filters, url.searchParams).toString();
    router.history.replace(
      `${url.pathname}${url.search}${url.hash}`,
      router.history.location.state,
      { ignoreBlocker: true },
    );
  };
  useEffect(() => {
    onUnsavedChanges(hasUnsavedChanges);
    return () => onUnsavedChanges(false);
  }, [hasUnsavedChanges, onUnsavedChanges]);
  useEffect(() => {
    const restoreFilters = () => {
      const filters = parseDocumentFilters(new URLSearchParams(window.location.search));
      setDocumentFilters(filters);
      setDocumentFilterDraft(filters);
    };
    window.addEventListener("popstate", restoreFilters);
    return () => window.removeEventListener("popstate", restoreFilters);
  }, []);
  useEffect(() => {
    setCropRegion(null);
  }, [selectedDocument?.id]);
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
                document.name ??
                existing?.name ??
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
    evidencePreview.selectPage(region.page);
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
      await createAndPostPurchase({
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
      editedReceiptDocuments.current.delete(document.id);
      setHasUnsavedChanges(false);
      await refreshPurchases();
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
      await createAndPostPurchase({
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
      setConfirmedStatements((current) => new Set(current).add(key));
      setHasUnsavedChanges(false);
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
            onClick={() => cameraRef.current?.open()}
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
      <CameraCapture
        ref={cameraRef}
        onError={setMessage}
        onAdd={(file) => {
          markUnsaved();
          setMessage("");
          setFiles((current) => [...current, { id: crypto.randomUUID(), file, status: "queued" }]);
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
              onClick={() => cameraRef.current?.open()}
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
          <ImportDocumentList
            files={files}
            documents={documents}
            csvKinds={csvKinds}
            selected={selected}
            filters={documentFilterDraft}
            onFiltersChange={setDocumentFilterDraft}
            onSearch={() => applyDocumentFilters(documentFilterDraft)}
            onCsvKindChange={(filename, kind) => {
              markUnsaved();
              setCsvKinds((current) => ({ ...current, [filename]: kind }));
            }}
            onRemoveFile={(id) => {
              markUnsaved();
              setFiles((current) => current.filter((entry) => entry.id !== id));
            }}
            onSelectDocument={setSelected}
            onRetryDocument={(document) => void retryDocument(document)}
            nextCursor={nextDocumentCursor}
            loadingOlder={loadingOlderDocuments}
            onLoadOlder={() => void loadOlderDocuments()}
            pending={pending}
            onUpload={() => void uploadFiles()}
          />
          {selectedDocument && (
            <div className={styles.evidenceReviewLayout}>
              <EvidenceSourcePreview
                document={selectedDocument}
                previewState={evidencePreview}
                cropRegion={cropRegion}
                cropPending={cropPending}
                pending={pending}
                onCropRegionChange={setCropRegion}
                onSelectCropRegion={selectCropRegion}
                onDirty={markUnsaved}
                onRetryCrop={() => void retryDocumentCrop(selectedDocument)}
              />
              <div className={styles.evidenceEditColumn}>
                {!!selectedDocument.result?.receipt?.items?.length && (
                  <ReceiptReview
                    household={household}
                    selectedDocument={selectedDocument}
                    reviewTargets={reviewTargets}
                    activeReviewIndex={activeReviewIndex}
                    activeReviewTarget={activeReviewTarget}
                    paymentDraftFor={paymentDraftFor}
                    receiptTotal={receiptTotal}
                    markUnsaved={markUnsaved}
                    setReviewTargetIndex={setReviewTargetIndex}
                    setReceiptPayments={setReceiptPayments}
                    editItem={editItem}
                    onMerchantChange={(merchant) => {
                      editedReceiptDocuments.current.add(selectedDocument.id);
                      markUnsaved();
                      setDocuments((current) =>
                        current.map((document) =>
                          document.id === selectedDocument.id && document.result
                            ? {
                                ...document,
                                result: {
                                  ...document.result,
                                  receipt: { ...document.result.receipt, merchant },
                                },
                              }
                            : document,
                        ),
                      );
                    }}
                    suggestedCategoryId={suggestedCategoryId}
                    selectedCandidates={selectedCandidates}
                    setSelectedCandidates={setSelectedCandidates}
                    manualAmountOverrides={manualAmountOverrides}
                    setManualAmountOverrides={setManualAmountOverrides}
                    manualAmountOverrideReasons={manualAmountOverrideReasons}
                    setManualAmountOverrideReasons={setManualAmountOverrideReasons}
                    pending={pending}
                    onConfirm={() => void confirm(selectedDocument)}
                  />
                )}
              </div>
            </div>
          )}
          {selectedDocument && (
            <StatementReview
              document={selectedDocument}
              confirmedStatements={confirmedStatements}
              pending={pending}
              onConfirm={(entry) => void confirmStatement(selectedDocument, entry)}
            />
          )}
          {selectedDocument?.result && (
            <EvidenceMatching
              document={selectedDocument}
              documents={documents}
              purchases={recentPurchases}
              receiptEvidenceIndex={evidencePreview.receiptEvidenceIndex}
              onReceiptEvidenceIndexChange={evidencePreview.setReceiptEvidenceIndex}
              pending={pending}
              onPendingChange={setPending}
              onDirty={markUnsaved}
              onError={setMessage}
              onLinked={async () => {
                setHasUnsavedChanges(false);
                setMessage("");
                await refreshDocuments(true);
                onAdded(true);
              }}
            />
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
