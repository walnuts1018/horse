import { AlertCircle, ChevronRight, FileImage, Trash2, Upload, X } from "lucide-react";
import { useState } from "react";

import { isOneOf } from "../../../shared/lib/guard";
import {
  canReclassifyAttachment,
  canDeleteDocument,
  isCsvFile,
  isEmailFile,
  receiptDateInputValue,
  sourceKindLabel,
  statusLabel,
  uploadSourceKinds,
  yen,
  type DocumentFilters,
  type DocumentReview,
  type PendingUpload,
  type StatementSourceKind,
  type UploadSourceKind,
} from "../model/imports";

import styles from "./imports-page.module.css";

export function ImportDocumentList({
  files,
  documents,
  selected,
  reviewableReceiptCount,
  completedReceiptCount,
  selectedReceiptName,
  onNextReceipt,
  filters,
  onFiltersChange,
  onSearch,
  onSourceKindChange,
  onRemoveFile,
  onSelectDocument,
  onRetryDocument,
  onDeleteDocument,
  onReclassifyAttachment,
  nextCursor,
  loadingDocuments,
  documentLoadFailed,
  hasActiveFilters,
  onRetryLoadDocuments,
  loadingOlder,
  onLoadOlder,
  pending,
  onUpload,
}: {
  files: PendingUpload[];
  documents: DocumentReview[];
  selected: string | null;
  reviewableReceiptCount: number;
  completedReceiptCount: number;
  selectedReceiptName?: string;
  onNextReceipt: () => void;
  filters: DocumentFilters;
  onFiltersChange: (filters: DocumentFilters) => void;
  onSearch: () => void;
  onSourceKindChange: (id: string, kind: UploadSourceKind) => void;
  onRemoveFile: (id: string) => void;
  onSelectDocument: (id: string) => void;
  onRetryDocument: (document: DocumentReview) => void;
  onDeleteDocument: (document: DocumentReview) => void;
  onReclassifyAttachment: (document: DocumentReview, sourceKind: StatementSourceKind) => void;
  nextCursor: string | null;
  loadingDocuments: boolean;
  documentLoadFailed: boolean;
  hasActiveFilters: boolean;
  onRetryLoadDocuments: () => void;
  loadingOlder: boolean;
  onLoadOlder: () => void;
  pending: boolean;
  onUpload: () => void;
}) {
  const updateFilter = <K extends keyof DocumentFilters>(key: K, value: DocumentFilters[K]) =>
    onFiltersChange({ ...filters, [key]: value });

  return (
    <>
      <div className={styles.receiptSequence} aria-live="polite">
        <div>
          <strong>
            {reviewableReceiptCount + completedReceiptCount === 0
              ? "確認対象なし"
              : reviewableReceiptCount === 0
                ? "すべて確認しました"
                : `確認済み ${completedReceiptCount} / ${reviewableReceiptCount + completedReceiptCount}`}
          </strong>
          {selectedReceiptName && <span>選択中: {selectedReceiptName}</span>}
        </div>
        <button
          type="button"
          className="button button-soft"
          disabled={reviewableReceiptCount === 0}
          onClick={onNextReceipt}
        >
          次のレシート
        </button>
      </div>
      <form
        className="ledger-filters"
        onSubmit={(event) => {
          event.preventDefault();
          onSearch();
        }}
      >
        <label className="select-filter">
          店舗
          <input
            type="search"
            value={filters.store}
            onChange={(event) => updateFilter("store", event.target.value)}
          />
        </label>
        <label className="select-filter">
          取り込み開始日
          <input
            type="date"
            value={filters.from}
            onChange={(event) => updateFilter("from", event.target.value)}
          />
        </label>
        <label className="select-filter">
          取り込み終了日
          <input
            type="date"
            value={filters.to}
            onChange={(event) => updateFilter("to", event.target.value)}
          />
        </label>
        <label className="select-filter">
          原本種別
          <select
            value={filters.sourceKind}
            onChange={(event) => updateFilter("sourceKind", event.target.value)}
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
            value={filters.confirmed}
            onChange={(event) => updateFilter("confirmed", event.target.value)}
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
          {!isEmailFile(upload.file) && (
            <select
              aria-label={`${upload.file.name}の原本種別`}
              value={upload.sourceKind}
              onChange={(event) => {
                const sourceKinds = uploadSourceKinds(upload.file);
                if (isOneOf(event.target.value, sourceKinds)) {
                  onSourceKindChange(upload.id, event.target.value);
                }
              }}
            >
              {isCsvFile(upload.file) ? (
                <>
                  <option value="card">カード明細</option>
                  <option value="bank">銀行明細</option>
                </>
              ) : (
                <>
                  <option value="receipt">レシート</option>
                  <option value="order">注文履歴</option>
                  <option value="card">カード明細</option>
                  <option value="bank">銀行明細</option>
                </>
              )}
            </select>
          )}
          <button
            type="button"
            className="icon-button"
            aria-label="削除"
            disabled={pending}
            onClick={() => onRemoveFile(upload.id)}
          >
            <X size={16} />
          </button>
        </div>
      ))}
      {documents.length > 0 && documentLoadFailed && (
        <div className={styles.receiptIssues} role="alert">
          <p>取り込み履歴の更新に失敗しました。表示中の履歴は最新ではない可能性があります。</p>
          <button
            type="button"
            className="button button-soft"
            disabled={loadingDocuments}
            onClick={onRetryLoadDocuments}
          >
            {loadingDocuments ? "更新中…" : "再読み込み"}
          </button>
        </div>
      )}
      {documents.length === 0 && files.length === 0 && (
        <>
          <output className={styles.documentListEmpty}>
            {loadingDocuments
              ? "取り込み履歴を読み込み中…"
              : documentLoadFailed
                ? "取り込み履歴を表示できません"
                : hasActiveFilters
                  ? "条件に一致する原本はありません"
                  : "取り込み履歴はありません"}
          </output>
          {documentLoadFailed && (
            <button
              type="button"
              className="button button-soft button-wide"
              disabled={loadingDocuments}
              onClick={onRetryLoadDocuments}
            >
              {loadingDocuments ? "読み込み中…" : "再読み込み"}
            </button>
          )}
        </>
      )}
      {documents.map((document) => {
        const receipt = document.result?.receipt;
        const merchant = receipt?.merchant?.trim();
        const title =
          merchant ||
          document.name ||
          `${sourceKindLabel(document.sourceKind)} · ${document.id.slice(0, 8)}`;
        const amount =
          document.result?.computedTotal ??
          receipt?.items?.reduce((total, item) => total + item.grossAmount, 0);
        const purchasedAt = receiptDateInputValue(receipt?.purchasedAt);
        return (
          <div className="persisted-document-row" key={document.id}>
            <button
              type="button"
              className={`file-review-row review-select ${selected === document.id ? "review-selected" : ""}`}
              disabled={document.deletionPending}
              onClick={() => onSelectDocument(document.id)}
            >
              <span className="file-thumb">
                <FileImage />
              </span>
              <div>
                <b>{title}</b>
                <small>
                  {statusLabel(document.status)}
                  {document.duplicateOf ? " · 重複候補" : ""}
                </small>
                {receipt && (
                  <small>
                    {[
                      purchasedAt,
                      amount === undefined ? "" : yen(amount),
                      receipt.items ? `${receipt.items.length}品目` : "",
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </small>
                )}
                {merchant && document.name && <small>{document.name}</small>}
                {document.attachmentParent && (
                  <small>メール添付 · {document.attachmentParent.name}</small>
                )}
              </div>
              <ChevronRight size={15} />
            </button>
            {document.status.toUpperCase() === "FAILED" && (
              <button
                type="button"
                className="text-button"
                disabled={pending}
                onClick={() => onRetryDocument(document)}
              >
                再抽出
              </button>
            )}
            {canDeleteDocument(document) && (
              <button
                type="button"
                className="text-button"
                disabled={pending}
                onClick={() => onDeleteDocument(document)}
              >
                <Trash2 size={15} />
                {document.deletionPending ? "削除を再試行" : "原本を削除"}
              </button>
            )}
            {canReclassifyAttachment(document) && (
              <AttachmentReclassification
                document={document}
                pending={pending}
                onReclassify={onReclassifyAttachment}
              />
            )}
          </div>
        );
      })}
      {nextCursor && (
        <button
          className="button button-soft button-wide"
          type="button"
          disabled={loadingOlder}
          onClick={onLoadOlder}
        >
          {loadingOlder ? "読み込み中" : "古い履歴を読み込む"}
        </button>
      )}
      {files.length > 0 && (
        <button
          type="button"
          className="button button-primary button-wide"
          disabled={pending}
          onClick={onUpload}
        >
          <Upload size={15} />
          {pending ? "送信しています" : "証拠を送信"}
        </button>
      )}
    </>
  );
}

function AttachmentReclassification({
  document,
  pending,
  onReclassify,
}: {
  document: DocumentReview;
  pending: boolean;
  onReclassify: (document: DocumentReview, sourceKind: StatementSourceKind) => void;
}) {
  const [sourceKind, setSourceKind] = useState<StatementSourceKind | "">("");

  return (
    <div className="attachment-reclassification">
      <select
        aria-label={`${document.name ?? "添付文書"}の明細種別`}
        value={sourceKind}
        disabled={pending}
        onChange={(event) => {
          if (isOneOf(event.target.value, ["card", "bank", ""])) {
            setSourceKind(event.target.value);
          }
        }}
      >
        <option value="">明細種別</option>
        <option value="card">カード明細</option>
        <option value="bank">銀行明細</option>
      </select>
      <button
        type="button"
        className="text-button"
        disabled={pending || !sourceKind || !canReclassifyAttachment(document, sourceKind)}
        onClick={() => {
          if (sourceKind) onReclassify(document, sourceKind);
        }}
      >
        選択した種別で再解析
      </button>
    </div>
  );
}
