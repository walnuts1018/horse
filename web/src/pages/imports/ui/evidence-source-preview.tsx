import type { Dispatch, SetStateAction } from "react";
import { FileImage } from "lucide-react";
import { InlineError } from "../../../shared/ui";
import type { EvidencePreviewController } from "../model/evidence-preview";
import { yen, type DocumentReview } from "../model/imports";
import { regionPolygon, type Region } from "../model/evidence-geometry";

export function EvidenceSourcePreview({
  document,
  previewState,
  cropRegion,
  cropPending,
  pending,
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

  return (
    <details className="evidence-source-column" open>
      <summary>原本</summary>
      <div className="evidence-preview">
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
              <div className="evidence-preview-page-controls">
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
              <div className="evidence-image-stage">
                <img src={preview.src} alt={`${page + 1}ページ目の原本`} />
                {document.result?.receipt?.items?.map((item, index) =>
                  item.region?.page === page ? (
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
                        onSelectCropRegion(item.region!);
                      }}
                    />
                  ) : null,
                )}
                {document.result?.ocrLines?.map((line, index) =>
                  line.region?.page === page ? (
                    <button
                      key={`line-${index}`}
                      type="button"
                      className="evidence-region evidence-region-ocr"
                      style={{
                        clipPath: `polygon(${regionPolygon(line.region, preview.transform)})`,
                      }}
                      aria-label={`${line.text}の原本位置`}
                      onClick={() => onSelectCropRegion(line.region!)}
                    />
                  ) : null,
                )}
              </div>
            )}
          </>
        ) : null}
      </div>
      {(document.result?.receipt?.items?.length ||
        document.result?.ocrLines?.some((line) => line.region)) && (
        <section className="panel evidence-crop-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">原本座標</span>
              <h3>範囲の再抽出</h3>
            </div>
            <span>{cropRegion ? `ページ ${cropRegion.page + 1}` : "範囲未選択"}</span>
          </div>
          <div className="evidence-source-items">
            {document.result?.receipt?.items?.map((item, index) => (
              <button
                type="button"
                key={index}
                className={`text-button ${Number(receiptEvidenceIndex) === index ? "evidence-item-selected" : ""}`}
                aria-pressed={Number(receiptEvidenceIndex) === index}
                disabled={!item.region}
                onClick={() => {
                  setReceiptEvidenceIndex(String(index));
                  if (item.region) onSelectCropRegion(item.region);
                }}
              >
                {item.name} · {yen(item.grossAmount ?? item.printedAmount)}
              </button>
            ))}
          </div>
          {!!document.result?.ocrLines?.length && (
            <div className="evidence-ocr-lines">
              {document.result.ocrLines
                .filter((line) => line.region)
                .slice(0, 80)
                .map((line, index) => (
                  <button
                    type="button"
                    className="text-button"
                    key={`${line.text}-${index}`}
                    onClick={() => onSelectCropRegion(line.region!)}
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
