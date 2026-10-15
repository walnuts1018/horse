import {
  identityTransform,
  invertTransform,
  type PreviewTransform,
} from "../model/evidence-geometry";

function isPreviewTransform(value: unknown): value is PreviewTransform {
  return (
    Array.isArray(value) &&
    value.length === 9 &&
    value.every((item) => typeof item === "number" && Number.isFinite(item))
  );
}

export async function readDocumentPreview(
  documentId: string,
  page: number,
  signal: AbortSignal,
): Promise<{ blob: Blob; transform: PreviewTransform }> {
  const response = await fetch(
    `/documents/${encodeURIComponent(documentId)}/preview?page=${page}`,
    { credentials: "include", signal },
  );
  if (!response.ok) throw new Error(`原本ページを表示できませんでした (${response.status})`);
  const sourcePage = Number(response.headers.get("X-Source-Page"));
  if (sourcePage !== page) throw new Error("原本ページが一致しません");
  const encodedTransform = response.headers.get("X-Coordinate-Transform");
  let transform = identityTransform;
  if (encodedTransform) {
    const parsed: unknown = JSON.parse(encodedTransform);
    if (isPreviewTransform(parsed)) transform = parsed;
  }
  if (!invertTransform(transform)) throw new Error("原本の座標を読み取れません");
  const blob = await response.blob();
  if (blob.type !== "image/png") throw new Error("原本ページを表示できませんでした");
  return { blob, transform };
}
