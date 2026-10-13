import { AlertCircle, ChevronRight, FileImage, Upload, X } from "lucide-react";
import {
  isCsvFile,
  sourceKindLabel,
  statusLabel,
  type DocumentFilters,
  type DocumentReview,
  type PendingUpload,
} from "../model/imports";

export function ImportDocumentList({
  files,
  documents,
  csvKinds,
  selected,
  filters,
  onFiltersChange,
  onSearch,
  onCsvKindChange,
  onRemoveFile,
  onSelectDocument,
  onRetryDocument,
  nextCursor,
  loadingOlder,
  onLoadOlder,
  pending,
  onUpload,
}: {
  files: PendingUpload[];
  documents: DocumentReview[];
  csvKinds: Record<string, "card" | "bank">;
  selected: string | null;
  filters: DocumentFilters;
  onFiltersChange: (filters: DocumentFilters) => void;
  onSearch: () => void;
  onCsvKindChange: (filename: string, kind: "card" | "bank") => void;
  onRemoveFile: (id: string) => void;
  onSelectDocument: (id: string) => void;
  onRetryDocument: (document: DocumentReview) => void;
  nextCursor: string | null;
  loadingOlder: boolean;
  onLoadOlder: () => void;
  pending: boolean;
  onUpload: () => void;
}) {
  const updateFilter = <K extends keyof DocumentFilters>(key: K, value: DocumentFilters[K]) =>
    onFiltersChange({ ...filters, [key]: value });

  return (
    <>
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
          {isCsvFile(upload.file) && (
            <select
              aria-label={`${upload.file.name}の明細種別`}
              value={csvKinds[upload.file.name] ?? "card"}
              onChange={(event) =>
                onCsvKindChange(upload.file.name, event.target.value as "card" | "bank")
              }
            >
              <option value="card">カード明細</option>
              <option value="bank">銀行明細</option>
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
      {documents.map((document) => (
        <div className="persisted-document-row" key={document.id}>
          <button
            type="button"
            className={`file-review-row review-select ${selected === document.id ? "review-selected" : ""}`}
            onClick={() => onSelectDocument(document.id)}
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
              onClick={() => onRetryDocument(document)}
            >
              再抽出
            </button>
          )}
        </div>
      ))}
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
        <button className="button button-primary button-wide" disabled={pending} onClick={onUpload}>
          <Upload size={15} />
          {pending ? "送信しています" : "証拠を送信"}
        </button>
      )}
    </>
  );
}
