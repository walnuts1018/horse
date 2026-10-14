import { useQuery } from "@tanstack/react-query";
import { Camera, FileImage, ShieldCheck, Upload } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import "./imports-page.css";
import { type Household } from "../../../shared/api";
import { errorMessage } from "../../../shared/lib/errors";
import { InlineError } from "../../../shared/ui";
import { deleteDocument, readDocument } from "../api/documents";
import { useEvidencePreview } from "../model/evidence-preview";
import {
  asRecord,
  defaultUploadSourceKind,
  isSupportedFile,
  withStableEvidenceKeys,
  type DocumentFilters,
  type DocumentReview,
  type PendingUpload,
  type StatementEntry,
  type StatementSourceKind,
} from "../model/imports";
import { useDocumentEvidence } from "../model/use-document-evidence";
import { useImportConfirmation } from "../model/use-import-confirmation";
import { useImportDocuments } from "../model/use-import-documents";
import { CameraCapture, type CameraCaptureHandle } from "./camera-capture";
import { ImportDocumentList } from "./import-document-list";
import { ImportDocumentReview } from "./import-document-review";

export function ImportsPage({
  household,
  documentFilters,
  filterLocation,
  onApplyDocumentFilters,
  onAdded,
  onUnsavedChanges,
}: {
  household: Household;
  documentFilters: DocumentFilters;
  filterLocation: object;
  onApplyDocumentFilters: (filters: DocumentFilters) => void;
  onAdded: (keepOpen?: boolean) => void;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const cameraRef = useRef<CameraCaptureHandle>(null);
  const [files, setFiles] = useState<PendingUpload[]>([]);
  // 親への未保存通知は effect で状態を push せず、変化したイベントから直接行う
  const onUnsavedChangesRef = useRef(onUnsavedChanges);
  const reportedUnsavedChanges = useRef(false);
  useEffect(() => {
    onUnsavedChangesRef.current = onUnsavedChanges;
  }, [onUnsavedChanges]);
  useEffect(() => {
    return () => {
      onUnsavedChangesRef.current(false);
    };
  }, []);
  const setHasUnsavedChanges = useCallback((unsaved: boolean) => {
    if (reportedUnsavedChanges.current === unsaved) return;
    reportedUnsavedChanges.current = unsaved;
    onUnsavedChangesRef.current(unsaved);
  }, []);
  const [documentFilterDraftState, setDocumentFilterDraftState] = useState({
    location: filterLocation,
    filters: documentFilters,
  });
  const documentFilterDraft =
    documentFilterDraftState.location === filterLocation
      ? documentFilterDraftState.filters
      : documentFilters;
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
    clearReceiptDraft,
    refreshDocuments,
    loadOlderDocuments,
  } = useImportDocuments(household.id, documentFilters, setMessage);
  const [pending, setPending] = useState(false);
  const listedDocument = documents.find((document) => document.id === selected);
  const selectedDocumentQuery = useQuery({
    queryKey: ["import-document-detail", household.id, selected],
    queryFn: ({ signal }) => readDocument(selected ?? "", signal),
    enabled: Boolean(selected && !listedDocument),
  });
  const selectedDocument = useMemo(
    () =>
      listedDocument ??
      (selectedDocumentQuery.data ? withStableEvidenceKeys(selectedDocumentQuery.data) : undefined),
    [listedDocument, selectedDocumentQuery.data],
  );
  useEffect(() => {
    if (!listedDocument && selectedDocumentQuery.error) {
      setMessage(errorMessage(selectedDocumentQuery.error));
    }
  }, [listedDocument, selectedDocumentQuery.error]);
  const evidencePreview = useEvidencePreview(selectedDocument);
  const evidence = useDocumentEvidence({
    document: selectedDocument,
    preview: evidencePreview,
    setDocuments,
    refreshDocuments,
    markUnsaved: () => setHasUnsavedChanges(true),
    onError: setMessage,
  });
  const review = useImportConfirmation({
    household,
    selectedDocument,
    setDocuments,
    editedReceiptDocuments,
    clearReceiptDraft,
    markUnsaved: () => setHasUnsavedChanges(true),
    setHasUnsavedChanges,
    onAdded,
  });
  const markUnsaved = () => setHasUnsavedChanges(true);
  const setDocumentFilterDraft = (filters: DocumentFilters) =>
    setDocumentFilterDraftState({ location: filterLocation, filters });
  // フィルタスコープが変わったときだけ確認状態をリセットする
  const confirmationScope = useRef(documentFilterScope);
  useEffect(() => {
    if (confirmationScope.current === documentFilterScope) return;
    confirmationScope.current = documentFilterScope;
    review.resetConfirmation();
  }, [documentFilterScope, review]);
  const addFiles = (fileList: FileList | null) => {
    if (!fileList) return;
    const candidates = Array.from(fileList);
    const supported = candidates.filter(isSupportedFile);
    const unsupported = candidates.filter((file) => !isSupportedFile(file));
    if (supported.length) markUnsaved();
    setFiles((current) => [
      ...current,
      ...supported.map((file) => ({
        id: crypto.randomUUID(),
        file,
        sourceKind: defaultUploadSourceKind(file),
        status: "queued" as const,
      })),
    ]);
    setMessage(
      unsupported.length
        ? `対応していない形式: ${unsupported.map((file) => file.name).join("、")}`
        : "",
    );
  };
  const uploadFiles = async (queued: PendingUpload[] = files) => {
    if (!queued.length) return;
    setPending(true);
    setMessage("");
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
        if (entry.analysisFile && entry.analysisTransform) {
          body.set("analysisFile", entry.analysisFile);
          body.set("analysisTransform", JSON.stringify(entry.analysisTransform));
        }
        body.set("householdId", household.id);
        body.set("sourceKind", entry.sourceKind);
        const response = await fetch("/documents", {
          method: "POST",
          credentials: "include",
          body,
        });
        if (!response.ok) throw new Error(`証拠を送信できませんでした (${response.status})`);
        const accepted = asRecord(await response.json()) ?? {};
        const documentReview: DocumentReview = {
          id: typeof accepted.id === "string" ? accepted.id : "",
          jobId: typeof accepted.jobId === "string" ? accepted.jobId : undefined,
          name: typeof accepted.name === "string" ? accepted.name : file.name,
          status: typeof accepted.status === "string" ? accepted.status : "QUEUED",
          sourceKind: entry.sourceKind,
          duplicateOf: typeof accepted.duplicateOf === "string" ? accepted.duplicateOf : undefined,
          sha256: typeof accepted.sha256 === "string" ? accepted.sha256 : "",
        };
        setDocuments((current) => [
          documentReview,
          ...current.filter((document) => document.id !== documentReview.id),
        ]);
        setSelected((current) => current ?? (typeof accepted.id === "string" ? accepted.id : null));
        setFiles((current) => current.filter((upload) => upload.id !== entry.id));
      } catch (reason) {
        failedUploads.add(entry.id);
        setFiles((current) =>
          current.map((upload) =>
            upload.id === entry.id
              ? { ...upload, status: "failed", error: errorMessage(reason) }
              : upload,
          ),
        );
        setMessage((current) => (current ? current : `${file.name}: ${errorMessage(reason)}`));
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
      setMessage(errorMessage(reason));
    } finally {
      setPending(false);
    }
  };
  const removeDocument = async (document: DocumentReview) => {
    if (
      !window.confirm(`${document.name ?? "この原本"}を削除しますか？この操作は取り消せません。`)
    ) {
      return;
    }
    setPending(true);
    setMessage("");
    try {
      await deleteDocument(document.id);
      setDocuments((current) => current.filter((entry) => entry.id !== document.id));
      if (selected === document.id) setSelected(null);
      await refreshDocuments(true);
    } catch (reason) {
      setMessage(errorMessage(reason));
      if (selected === document.id) setSelected(null);
      await refreshDocuments(true);
    } finally {
      setPending(false);
    }
  };
  const reclassifyAttachment = async (document: DocumentReview, kind: StatementSourceKind) => {
    setPending(true);
    setMessage("");
    try {
      const response = await fetch(`/documents/${encodeURIComponent(document.id)}/retry`, {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ sourceKind: kind }),
      });
      if (!response.ok) throw new Error(`明細種別を変更できませんでした (${response.status})`);
      editedReceiptDocuments.current.delete(document.id);
      await refreshDocuments(true);
    } catch (reason) {
      setMessage(errorMessage(reason));
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
      setMessage(errorMessage(reason));
    } finally {
      setPending(false);
    }
  };
  const confirmStatement = async (
    document: DocumentReview,
    entry: StatementEntry,
    paymentAccountId: string,
  ) => {
    setPending(true);
    setMessage("");
    try {
      await review.confirmStatement(document, entry, paymentAccountId);
    } catch (reason) {
      setMessage(errorMessage(reason));
    } finally {
      setPending(false);
    }
  };
  const confirmStatementRefund = async (
    document: DocumentReview,
    entry: StatementEntry,
    purchaseId: string,
    itemId?: string,
  ) => {
    setPending(true);
    setMessage("");
    try {
      await review.confirmStatementRefund(document, entry, purchaseId, itemId);
    } catch (reason) {
      setMessage(errorMessage(reason));
    } finally {
      setPending(false);
    }
  };
  const linkStatementRefundCorrection = async (
    document: DocumentReview,
    entry: StatementEntry,
    correctionId: string,
  ) => {
    setPending(true);
    setMessage("");
    try {
      await review.linkStatementRefundCorrection(document, entry, correctionId);
    } catch (reason) {
      setMessage(errorMessage(reason));
    } finally {
      setPending(false);
    }
  };
  const confirmStatementTransfer = async (
    document: DocumentReview,
    entry: StatementEntry,
    statementAccountId: string,
    otherAccountId: string,
  ) => {
    setPending(true);
    setMessage("");
    try {
      await review.confirmStatementTransfer(document, entry, statementAccountId, otherAccountId);
    } catch (reason) {
      setMessage(errorMessage(reason));
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
        onAdd={({ originalFile, analysisFile, analysisTransform }) => {
          markUnsaved();
          setMessage("");
          const upload: PendingUpload = {
            id: crypto.randomUUID(),
            file: originalFile,
            analysisFile,
            analysisTransform,
            sourceKind: defaultUploadSourceKind(originalFile),
            status: "queued",
          };
          setFiles((current) => [...current, upload]);
          void uploadFiles([upload]);
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
            selected={selected}
            filters={documentFilterDraft}
            onFiltersChange={setDocumentFilterDraft}
            onSearch={() => onApplyDocumentFilters(documentFilterDraft)}
            onSourceKindChange={(id, kind) => {
              markUnsaved();
              setFiles((current) =>
                current.map((entry) => (entry.id === id ? { ...entry, sourceKind: kind } : entry)),
              );
            }}
            onRemoveFile={(id) => {
              markUnsaved();
              setFiles((current) => current.filter((entry) => entry.id !== id));
            }}
            onSelectDocument={setSelected}
            onRetryDocument={(document) => void retryDocument(document)}
            onDeleteDocument={(document) => void removeDocument(document)}
            onReclassifyAttachment={(document, kind) => void reclassifyAttachment(document, kind)}
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
              onConfirmStatement={(entry, paymentAccountId) =>
                void confirmStatement(selectedDocument, entry, paymentAccountId)
              }
              onConfirmRefund={(entry, purchaseId, itemId) =>
                void confirmStatementRefund(selectedDocument, entry, purchaseId, itemId)
              }
              onLinkRefund={(entry, correctionId) =>
                void linkStatementRefundCorrection(selectedDocument, entry, correctionId)
              }
              onConfirmTransfer={(entry, statementAccountId, otherAccountId) =>
                void confirmStatementTransfer(
                  selectedDocument,
                  entry,
                  statementAccountId,
                  otherAccountId,
                )
              }
              onConfirmLoan={(entry, accountId, fromMemberId, toMemberId, loanId) =>
                void review.confirmStatementLoan(
                  selectedDocument,
                  entry,
                  accountId,
                  fromMemberId,
                  toMemberId,
                  loanId,
                )
              }
              onLinkExistingTransfer={(entry, accountId, transferId) =>
                void review.linkStatementTransfer(selectedDocument, entry, accountId, transferId)
              }
              onLinkExistingMovement={(entry, accountId, movementId) =>
                void review.linkStatementAccountMovement(
                  selectedDocument,
                  entry,
                  accountId,
                  movementId,
                )
              }
              onLinkExistingIncome={(entry, accountId, incomeId) =>
                void review.linkStatementAccountIncome(selectedDocument, entry, accountId, incomeId)
              }
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
      <div className="import-note">
        <ShieldCheck size={17} />
        <span>原本は帳簿の品目と紐づけて確認できます</span>
      </div>
    </>
  );
}
