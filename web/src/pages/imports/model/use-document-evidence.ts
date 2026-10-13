import {
  useEffect,
  useState,
  type Dispatch,
  type MutableRefObject,
  type SetStateAction,
} from "react";
import type { EvidencePreviewController } from "./evidence-preview";
import type { DocumentReview } from "./imports";
import type { Region } from "./evidence-geometry";

export function useDocumentEvidence({
  document,
  preview,
  setDocuments,
  editedReceiptDocuments,
  refreshDocuments,
  markUnsaved,
  onError,
}: {
  document: DocumentReview | undefined;
  preview: EvidencePreviewController;
  setDocuments: Dispatch<SetStateAction<DocumentReview[]>>;
  editedReceiptDocuments: MutableRefObject<Set<string>>;
  refreshDocuments: (silent?: boolean) => Promise<void>;
  markUnsaved: () => void;
  onError: (message: string) => void;
}) {
  const [cropRegion, setCropRegion] = useState<Region | null>(null);
  const [pending, setPending] = useState(false);

  useEffect(() => {
    setCropRegion(null);
  }, [document?.id]);

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
      onError((reason as Error).message);
    } finally {
      setPending(false);
    }
  };

  return { cropRegion, setCropRegion, pending, selectCropRegion, retryCrop };
}
