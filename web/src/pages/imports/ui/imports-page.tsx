import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { AlertCircle, Camera, FileImage, PenLine, ShieldCheck, Upload } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import "./imports-page.css";
import "./import-model-selector.css";
import { type Household } from "../../../shared/api";
import { errorMessage } from "../../../shared/lib/errors";
import { InlineError } from "../../../shared/ui";
import { deleteDocument, readDocument } from "../api/documents";
import { useEvidencePreview } from "../model/evidence-preview";
import {
  asRecord,
  defaultUploadSourceKind,
  defaultImportModel,
  isQuotaLimitedExtraction,
  importModels,
  isImportModel,
  isSupportedFile,
  proImportModel,
  withStableEvidenceKeys,
  type DocumentFilters,
  type DocumentReview,
  type ImportModel,
  type PendingUpload,
  type StatementEntry,
  type StatementSourceKind,
} from "../model/imports";
import {
  nextReceiptToReview,
  receiptReviewProgress,
  reviewableReceipts,
} from "../model/receipt-sequential-review";
import { useDocumentEvidence } from "../model/use-document-evidence";
import { useImportConfirmation } from "../model/use-import-confirmation";
import { useImportDocuments } from "../model/use-import-documents";
import { CameraCapture, type CameraCaptureHandle } from "./camera-capture";
import { ImportDocumentList } from "./import-document-list";
import { ImportDocumentReview } from "./import-document-review";

const noCompletedReceipts = new Set<string>();

export function ImportsPage({
  household,
  captureRequested,
  onCaptureHandled,
  documentFilters,
  filterLocation,
  onApplyDocumentFilters,
  onAdded,
  onUnsavedChanges,
}: {
  household: Household;
  captureRequested: boolean;
  onCaptureHandled: () => void;
  documentFilters: DocumentFilters;
  filterLocation: object;
  onApplyDocumentFilters: (filters: DocumentFilters) => void;
  onAdded: (keepOpen?: boolean) => void;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const cameraRef = useRef<CameraCaptureHandle>(null);
  useEffect(() => {
    if (!captureRequested) return;
    const frame = window.requestAnimationFrame(() => {
      cameraRef.current?.open();
      onCaptureHandled();
    });
    return () => window.cancelAnimationFrame(frame);
  }, [captureRequested, onCaptureHandled]);
  const [files, setFiles] = useState<PendingUpload[]>([]);
  const [importModelChoice, setImportModelChoice] = useState<{
    model: ImportModel;
    quotaDocumentScope: string;
  }>();
  // 親への未保存通知は effect で状態を push せず、変化したイベントから直接行う
  const onUnsavedChangesRef = useRef(onUnsavedChanges);
  const reportedUnsavedChanges = useRef(false);
  const hasUnsavedChanges = useRef(false);
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
    hasUnsavedChanges.current = unsaved;
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
  const [completedReceiptState, setCompletedReceiptState] = useState<{
    scope: string;
    ids: Set<string>;
  }>(() => ({ scope: documentFilterScope, ids: new Set() }));
  const completedReceiptIDs =
    completedReceiptState.scope === documentFilterScope
      ? completedReceiptState.ids
      : noCompletedReceipts;
  const [dragging, setDragging] = useState(false);
  const [message, setMessage] = useState("");
  const {
    documents,
    setDocuments,
    selected,
    setSelected,
    nextDocumentCursor,
    loadingDocuments,
    documentLoadFailed,
    loadingOlderDocuments,
    editedReceiptDocuments,
    clearReceiptDraft,
    refreshDocuments,
    loadOlderDocuments,
  } = useImportDocuments(household.id, documentFilters, setMessage);
  const [pending, setPending] = useState(false);
  const quotaLimitedDocuments = documents.filter(isQuotaLimitedExtraction);
  const quotaLimitedDocumentIDs = JSON.stringify(
    quotaLimitedDocuments.map((document) => document.id),
  );
  const importModelForUpload =
    importModelChoice?.quotaDocumentScope === quotaLimitedDocumentIDs
      ? importModelChoice.model
      : quotaLimitedDocuments.length > 0
        ? proImportModel
        : defaultImportModel;
  const selectedImportModel = importModels.find((model) => model.value === importModelForUpload);
  const importModelWasAutoSelectedForQuota =
    quotaLimitedDocuments.length > 0 &&
    importModelChoice?.quotaDocumentScope !== quotaLimitedDocumentIDs;
  const selectedDocumentID = useRef(selected);
  useEffect(() => {
    selectedDocumentID.current = selected;
  }, [selected]);
  const handleSelectDocument = (documentID: string) => {
    if (documentID === selected) return;
    if (
      hasUnsavedChanges.current &&
      !window.confirm("未確定の確認内容があります。別のレシートへ移動しますか？")
    ) {
      return;
    }
    if (hasUnsavedChanges.current) setHasUnsavedChanges(false);
    selectedDocumentID.current = documentID;
    setSelected(documentID);
  };
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
  const displayedMessage =
    !listedDocument && selectedDocumentQuery.error
      ? errorMessage(selectedDocumentQuery.error)
      : message;
  const receiptProgress = useMemo(
    () => receiptReviewProgress(documents, completedReceiptIDs),
    [completedReceiptIDs, documents],
  );
  const selectedReceipt = documents.find((document) => document.id === selected);
  const evidencePreview = useEvidencePreview(selectedDocument);
  const evidence = useDocumentEvidence({
    document: selectedDocument,
    preview: evidencePreview,
    setDocuments,
    refreshDocuments,
    markUnsaved: () => setHasUnsavedChanges(true),
    onError: setMessage,
  });
  const confirmingReceipt = useRef(false);
  const review = useImportConfirmation({
    household,
    selectedDocument,
    setDocuments,
    editedReceiptDocuments,
    clearReceiptDraft,
    markUnsaved: () => setHasUnsavedChanges(true),
    setHasUnsavedChanges,
    onAdded: (keepOpen) => onAdded(keepOpen ?? confirmingReceipt.current),
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
        model: importModelForUpload,
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
        body.set("model", entry.model);
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
    if (isQuotaLimitedExtraction(document)) {
      setImportModelChoice({ model: proImportModel, quotaDocumentScope: quotaLimitedDocumentIDs });
    }
    setPending(true);
    setMessage("");
    try {
      const response = await fetch(`/documents/${encodeURIComponent(document.id)}/retry`, {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(isQuotaLimitedExtraction(document) ? { model: proImportModel } : {}),
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
    confirmingReceipt.current = true;
    try {
      await review.confirm(document);
      setCompletedReceiptState((current) => ({
        scope: documentFilterScope,
        ids: new Set([...(current.scope === documentFilterScope ? current.ids : []), document.id]),
      }));
      const receipts = reviewableReceipts(
        documents,
        new Set([...completedReceiptIDs, document.id]),
      );
      const nextReceipt = nextReceiptToReview(receipts, document.id);
      if (nextReceipt && selectedDocumentID.current === document.id) {
        selectedDocumentID.current = nextReceipt.id;
        setSelected(nextReceipt.id);
      }
    } catch (reason) {
      setMessage(errorMessage(reason));
    } finally {
      confirmingReceipt.current = false;
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
            model: importModelForUpload,
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
          <div className="import-model-selector">
            <label htmlFor="import-model">AIモデル</label>
            <select
              id="import-model"
              value={importModelForUpload}
              onChange={(event) => {
                if (isImportModel(event.target.value)) {
                  setImportModelChoice({
                    model: event.target.value,
                    quotaDocumentScope: quotaLimitedDocumentIDs,
                  });
                }
              }}
            >
              <optgroup label="無料枠">
                {importModels
                  .filter((model) => model.tier === "free")
                  .map((model) => (
                    <option key={model.value} value={model.value}>
                      {model.label} · {model.value}
                    </option>
                  ))}
              </optgroup>
              <optgroup label="有料">
                {importModels
                  .filter((model) => model.tier === "paid")
                  .map((model) => (
                    <option key={model.value} value={model.value}>
                      {model.label} · {model.value}
                    </option>
                  ))}
              </optgroup>
            </select>
            <small aria-live="polite">
              {selectedImportModel?.tier === "paid"
                ? "有料枠では入力は製品改善に使われません。利用ごとに料金が発生します。"
                : "無料枠では入力がGoogleの製品改善に使われる場合があります。"}
            </small>
            {files.length > 0 && (
              <small aria-live="polite">
                追加済み:{" "}
                {importModels
                  .map((model) => {
                    const count = files.filter((file) => file.model === model.value).length;
                    return count > 0 ? `${model.label} ${count}件` : null;
                  })
                  .filter((value) => value !== null)
                  .join("・")}
              </small>
            )}
          </div>
          {quotaLimitedDocuments.length > 0 && (
            <output className="import-quota-notice">
              <AlertCircle size={16} aria-hidden="true" />
              <span>
                {importModelWasAutoSelectedForQuota
                  ? "無料モデルの利用上限に達したためGemini 3.1 Pro Previewを選択しました。次の実行には料金が発生します。"
                  : "利用上限に達した書類はGemini 3.1 Pro Previewで再抽出できます。料金が発生します。"}
              </span>
            </output>
          )}
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
            <Link
              className="button button-soft"
              to="/households/$householdId/new"
              params={{ householdId: household.id }}
              search
            >
              <PenLine size={16} />
              手入力で追加
            </Link>
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
            reviewableReceiptCount={receiptProgress.receipts.length}
            completedReceiptCount={receiptProgress.completedCount}
            selectedReceiptName={selectedReceipt?.name}
            onNextReceipt={() => {
              const nextReceipt = nextReceiptToReview(receiptProgress.receipts, selected ?? "");
              if (nextReceipt) handleSelectDocument(nextReceipt.id);
            }}
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
            onSelectDocument={handleSelectDocument}
            onRetryDocument={(document) => void retryDocument(document)}
            onDeleteDocument={(document) => void removeDocument(document)}
            onReclassifyAttachment={(document, kind) => void reclassifyAttachment(document, kind)}
            nextCursor={nextDocumentCursor}
            loadingDocuments={loadingDocuments}
            documentLoadFailed={documentLoadFailed}
            hasActiveFilters={Object.values(documentFilters).some(Boolean)}
            onRetryLoadDocuments={() => void refreshDocuments()}
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
              onInteractionQueued={() => refreshDocuments(true)}
              onSelectDocument={handleSelectDocument}
            />
          )}
          {displayedMessage && <InlineError message={displayedMessage} />}
        </section>
      </div>
      <div className="import-note">
        <ShieldCheck size={17} />
        <span>原本は帳簿の品目と紐づけて確認できます</span>
      </div>
    </>
  );
}
