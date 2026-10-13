import { useEffect, useRef, useState } from "react";
import { useRouter } from "@tanstack/react-router";
import { Camera, FileImage, ShieldCheck, Upload } from "lucide-react";
import "./imports-page.css";
import { type Household } from "../../../shared/api";
import { InlineError } from "../../../shared/ui";
import { useEvidencePreview } from "../model/evidence-preview";
import { useDocumentEvidence } from "../model/use-document-evidence";
import { useImportDocuments } from "../model/use-import-documents";
import { CameraCapture, type CameraCaptureHandle } from "./camera-capture";
import { ImportDocumentList } from "./import-document-list";
import { ImportDocumentReview } from "./import-document-review";
import {
  isSupportedFile,
  defaultDocumentFilters,
  parseDocumentFilters,
  serializeDocumentFilters,
  sourceKind,
  type DocumentFilters,
  type DocumentReview,
  type PendingUpload,
  type StatementEntry,
} from "../model/imports";
import { useImportConfirmation } from "../model/use-import-confirmation";

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
  const inputRef = useRef<HTMLInputElement>(null);
  const cameraRef = useRef<CameraCaptureHandle>(null);
  const [files, setFiles] = useState<PendingUpload[]>([]);
  const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false);
  const [csvKinds, setCsvKinds] = useState<Record<string, "card" | "bank">>({});
  const [documentFilters, setDocumentFilters] = useState<DocumentFilters>(() =>
    typeof window === "undefined"
      ? defaultDocumentFilters
      : parseDocumentFilters(new URLSearchParams(window.location.search)),
  );
  const [documentFilterDraft, setDocumentFilterDraft] = useState(documentFilters);
  const documentFilterScope = JSON.stringify(documentFilters);
  const [dragging, setDragging] = useState(false);
  const [message, setMessage] = useState("");
  const {
    documents,
    setDocuments,
    selected,
    setSelected,
    nextDocumentCursor,
    loadingOlderDocuments,
    editedReceiptDocuments,
    refreshDocuments,
    loadOlderDocuments,
  } = useImportDocuments(household.id, documentFilters, setMessage);
  const [pending, setPending] = useState(false);
  const selectedDocument = documents.find((document) => document.id === selected);
  const evidencePreview = useEvidencePreview(selectedDocument);
  const evidence = useDocumentEvidence({
    document: selectedDocument,
    preview: evidencePreview,
    setDocuments,
    editedReceiptDocuments,
    refreshDocuments,
    markUnsaved: () => setHasUnsavedChanges(true),
    onError: setMessage,
  });
  const review = useImportConfirmation({
    household,
    selectedDocument,
    setDocuments,
    editedReceiptDocuments,
    markUnsaved: () => setHasUnsavedChanges(true),
    setHasUnsavedChanges,
    onAdded,
  });
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
    review.setConfirmedStatements(new Set());
    review.setSelectedCandidates({});
  }, [documentFilterScope]);
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
  const confirmReceipt = async (document: DocumentReview) => {
    setPending(true);
    setMessage("");
    try {
      await review.confirm(document);
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const confirmStatement = async (document: DocumentReview, entry: StatementEntry) => {
    setPending(true);
    setMessage("");
    try {
      await review.confirmStatement(document, entry);
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
            <ImportDocumentReview
              document={selectedDocument}
              documents={documents}
              purchases={review.recentPurchases}
              household={household}
              previewState={evidencePreview}
              evidence={evidence}
              review={review}
              receiptEvidenceIndex={evidencePreview.receiptEvidenceIndex}
              onReceiptEvidenceIndexChange={evidencePreview.setReceiptEvidenceIndex}
              pending={pending}
              onPendingChange={setPending}
              onDirty={markUnsaved}
              onConfirmReceipt={() => void confirmReceipt(selectedDocument)}
              onConfirmStatement={(entry) => void confirmStatement(selectedDocument, entry)}
              onError={setMessage}
              onLinked={async () => {
                setHasUnsavedChanges(false);
                setMessage("");
                await refreshDocuments(true);
                onAdded(true);
              }}
              onSelectDocument={setSelected}
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
