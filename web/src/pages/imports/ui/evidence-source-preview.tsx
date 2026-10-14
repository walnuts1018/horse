import type { Dispatch, SetStateAction } from "react";

import { FileImage } from "lucide-react";
import { useEffect, useMemo, useRef, useSyncExternalStore } from "react";

import type { EvidencePreviewController } from "../model/evidence-preview";

import { InlineError } from "../../../shared/ui";
import { regionPolygon, type Region } from "../model/evidence-geometry";
import { yen, type DocumentReview } from "../model/imports";
import {
  getReceiptEvidenceSelection,
  receiptEvidenceSelectionIndex,
  selectReceiptEvidenceItem,
  subscribeReceiptEvidenceSelection,
} from "../model/receipt-evidence-selection";
import { ensureReceiptItemKeys } from "../model/receipt-review-draft";

import styles from "./imports-page.module.css";

export function EvidenceSourcePreview({
  document,
  previewState,
  cropRegion,
  cropPending,
  pending,
  selectedStatementReference,
  onSelectStatementRegion,
  onCropRegionChange,
  onSelectCropRegion,
  onDirty,
  onRetryCrop,
}: {
  document: DocumentReview;
  previewState: EvidencePreviewController;
  cropRegion: Region | null;
  cropPending: boolean;
  pending: boolean;
  selectedStatementReference: string;
  onSelectStatementRegion: (reference: string, page: number) => void;
  onCropRegionChange: Dispatch<SetStateAction<Region | null>>;
  onSelectCropRegion: (region: Region) => void;
  onDirty: () => void;
  onRetryCrop: () => void;
}) {
  const {
    page,
    pageDraft,
    setPageDraft,
    preview,
    loading,
    error,
    showPage,
    selectPage,
    receiptEvidenceIndex,
    setReceiptEvidenceIndex,
  } = previewState;
  const detailsRef = useRef<HTMLDetailsElement>(null);
  const receiptItems = useMemo(
    () => ensureReceiptItemKeys(document.result?.receipt?.items ?? []),
    [document.result?.receipt?.items],
  );
  const selection = useSyncExternalStore(
    (listener) => subscribeReceiptEvidenceSelection(document.id, listener),
    () => getReceiptEvidenceSelection(document.id),
    () => null,
  );
  const selectedReceiptIndex = receiptEvidenceSelectionIndex(
    selection,
    receiptItems.map((item, index) => item.key ?? `item-${index}`),
    Number(receiptEvidenceIndex),
  );

  useEffect(() => {
    if (!selection) return;
    detailsRef.current?.setAttribute("open", "");
    const item = receiptItems.at(selectedReceiptIndex);
    if (!item) return;
    const itemKey = item.key ?? `item-${selectedReceiptIndex}`;
    if (selection.itemKey !== itemKey) {
      selectReceiptEvidenceItem(document.id, {
        itemKey,
        index: selectedReceiptIndex,
        focusEditor: false,
      });
    }
    if (item.region && item.region.page !== page) selectPage(item.region.page);
  }, [document.id, page, receiptItems, selectPage, selectedReceiptIndex, selection]);

  return (
    <details className={styles.evidenceSourceColumn} ref={detailsRef} open>
      <summary>
        <span>原本</span>
        {preview?.documentId === document.id && preview.page === page && (
          <img className={styles.evidenceSummaryPreview} src={preview.src} alt="" />
        )}
      </summary>
      <div className={styles.evidencePreview}>
        <a
          className="review-image"
          href={`/documents/${encodeURIComponent(document.id)}/content`}
          target="_blank"
          rel="noreferrer"
        >
          <FileImage size={23} />
          <span>原本ファイルを開く</span>
        </a>
        {document.mimeType?.startsWith("image/") || document.mimeType === "application/pdf" ? (
          <>
            {document.mimeType === "application/pdf" && (
              <div className={styles.evidencePreviewPageControls}>
                <label>
                  PDFページ
                  <input
                    aria-label="PDFページ"
                    type="number"
                    min="1"
                    step="1"
                    value={pageDraft}
                    onChange={(event) => setPageDraft(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter") showPage();
                    }}
                  />
                </label>
                <button className="text-button" type="button" onClick={showPage}>
                  ページを表示
                </button>
              </div>
            )}
            {loading && <p>ページを読み込んでいます</p>}
            {error && <InlineError message={error} />}
            {preview?.documentId === document.id && preview.page === page && (
              <div className={styles.evidenceImageStage}>
                <img src={preview.src} alt={`${page + 1}ページ目の原本`} />
                {receiptItems.map((item, index) =>
                  item.region?.page === page ? (
                    <button
                      key={item.key}
                      type="button"
                      className={`${styles.evidenceRegion} ${selectedReceiptIndex === index ? styles.evidenceRegionSelected : ""}`}
                      style={{
                        clipPath: `polygon(${regionPolygon(item.region, preview.transform)})`,
                      }}
                      aria-label={`${item.name}の原本位置`}
                      aria-pressed={selectedReceiptIndex === index}
                      onClick={() => {
                        setReceiptEvidenceIndex(String(index));
                        selectReceiptEvidenceItem(document.id, {
                          itemKey: item.key ?? `item-${index}`,
                          index,
                          focusEditor: true,
                        });
                        onSelectCropRegion(item.region!);
                      }}
                    />
                  ) : null,
                )}
                {document.result?.ocrLines?.map((line) =>
                  line.region?.page === page ? (
                    <button
                      key={line.key}
                      type="button"
                      className={`${styles.evidenceRegion} ${styles.evidenceRegionOcr}`}
                      style={{
                        clipPath: `polygon(${regionPolygon(line.region, preview.transform)})`,
                      }}
                      aria-label={`${line.text}の原本位置`}
                      onClick={() => onSelectCropRegion(line.region!)}
                    />
                  ) : null,
                )}
                {document.result?.statements?.map((entry) => {
                  const reference = entry.sourceReference || `row:${entry.row}`;
                  return entry.region?.page === page ? (
                    <button
                      key={reference}
                      type="button"
                      className={`${styles.evidenceRegion} ${selectedStatementReference === reference ? styles.evidenceRegionSelected : styles.evidenceRegionOcr}`}
                      style={{
                        clipPath: `polygon(${regionPolygon(entry.region, preview.transform)})`,
                      }}
                      aria-label={`${entry.merchant}の明細位置`}
                      aria-pressed={selectedStatementReference === reference}
                      onClick={() => onSelectStatementRegion(reference, entry.region!.page)}
                    />
                  ) : null;
                })}
              </div>
            )}
          </>
        ) : null}
      </div>
      {(document.result?.receipt?.items?.length ||
        document.result?.ocrLines?.some((line) => line.region)) && (
        <section className={`panel ${styles.evidenceCropPanel}`}>
          <div className="panel-heading">
            <div>
              <span className="eyebrow">原本座標</span>
              <h3>範囲の再抽出</h3>
            </div>
            <span>{cropRegion ? `ページ ${cropRegion.page + 1}` : "範囲未選択"}</span>
          </div>
          <div className={styles.evidenceSourceItems}>
            {receiptItems.map((item, index) => (
              <button
                type="button"
                key={item.key}
                className={`text-button ${selectedReceiptIndex === index ? styles.evidenceItemSelected : ""}`}
                aria-pressed={selectedReceiptIndex === index}
                disabled={!item.region}
                onClick={() => {
                  setReceiptEvidenceIndex(String(index));
                  selectReceiptEvidenceItem(document.id, {
                    itemKey: item.key ?? `item-${index}`,
                    index,
                    focusEditor: true,
                  });
                  detailsRef.current?.setAttribute("open", "");
                  if (item.region && item.region.page !== page) selectPage(item.region.page);
                  if (item.region) onSelectCropRegion(item.region);
                }}
              >
                {item.name} · {yen(item.grossAmount)}
              </button>
            ))}
          </div>
          {!!document.result.ocrLines?.length && (
            <div className={styles.evidenceOcrLines}>
              {document.result.ocrLines
                .filter((line) => line.region)
                .slice(0, 80)
                .map((line) => (
                  <button
                    type="button"
                    className="text-button"
                    key={line.key}
                    onClick={() => onSelectCropRegion(line.region!)}
                  >
                    {line.text} · ページ {line.region!.page + 1}
                  </button>
                ))}
            </div>
          )}
          {cropRegion && (
            <div className={styles.cropCoordinateFields}>
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
                      onDirty();
                      onCropRegionChange((current) =>
                        current ? { ...current, [key]: Number(event.target.value) } : current,
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
                    onDirty();
                    const requestedPage = Math.max(0, Math.trunc(Number(event.target.value) - 1));
                    onCropRegionChange((current) =>
                      current ? { ...current, page: requestedPage } : current,
                    );
                    selectPage(requestedPage);
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
                onClick={onRetryCrop}
              >
                {cropPending ? "抽出中" : "この範囲で再抽出"}
              </button>
            </div>
          )}
        </section>
      )}
    </details>
  );
}
