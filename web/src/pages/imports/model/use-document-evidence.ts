import { useState, type Dispatch, type SetStateAction } from "react";

import type { Region } from "./evidence-geometry";
import type { EvidencePreviewController } from "./evidence-preview";
import type { DocumentReview } from "./imports";

import { errorMessage } from "../../../shared/lib/errors";
import { asRecord } from "./imports";

function isJobStatus(value: unknown): value is { jobId: string; status: string } {
  const record = asRecord(value);
  return (
    record !== undefined && typeof record.jobId === "string" && typeof record.status === "string"
  );
}

function toJobStatus(value: unknown): { jobId: string; status: string } {
  return isJobStatus(value) ? value : { jobId: "", status: "" };
}

export function useDocumentEvidence({
  document,
  preview,
  setDocuments,
  refreshDocuments,
  markUnsaved,
  onError,
}: {
  document: DocumentReview | undefined;
  preview: EvidencePreviewController;
  setDocuments: Dispatch<SetStateAction<DocumentReview[]>>;
  refreshDocuments: (silent?: boolean) => Promise<void>;
  markUnsaved: () => void;
  onError: (message: string) => void;
}) {
  const [cropRegionState, setCropRegionState] = useState<{
    documentId: string;
    region: Region | null;
  }>({ documentId: "", region: null });
  const [pending, setPending] = useState(false);

  const documentId = document?.id ?? "";
  // 文書が切り替わった直後は前の文書の範囲を選択していない状態として扱う
  const cropRegion = cropRegionState.documentId === documentId ? cropRegionState.region : null;
  const setCropRegion: Dispatch<SetStateAction<Region | null>> = (action) => {
    setCropRegionState((current) => {
      const previous = current.documentId === documentId ? current.region : null;
      const region = typeof action === "function" ? action(previous) : action;
      return { documentId, region };
    });
  };

  const selectCropRegion = (region: Region) => {
    markUnsaved();
    setCropRegion(region);
    preview.selectPage(region.page);
  };

  const retryCrop = async () => {
    if (!document || !cropRegion) return;
    setPending(true);
    onError("");
    try {
      const response = await fetch(`/documents/${encodeURIComponent(document.id)}/retry`, {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ crop: cropRegion }),
      });
      if (!response.ok) throw new Error(`範囲を再抽出できませんでした (${response.status})`);
      const accepted = toJobStatus(await response.json());
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
      onError(errorMessage(reason));
    } finally {
      setPending(false);
    }
  };

  return { cropRegion, setCropRegion, pending, selectCropRegion, retryCrop };
}
