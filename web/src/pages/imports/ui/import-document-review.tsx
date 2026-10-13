import type { Household, Purchase } from "../../../shared/api";
import type { useDocumentEvidence } from "../model/use-document-evidence";
import type { useImportConfirmation } from "../model/use-import-confirmation";
import type { DocumentReview, StatementEntry } from "../model/imports";
import type { EvidencePreviewController } from "../model/evidence-preview";
import { EvidenceMatching } from "./evidence-matching";
import { EvidenceSourcePreview } from "./evidence-source-preview";
import { ReceiptReview } from "./receipt-review";
import { StatementReview } from "./statement-review";
import styles from "./imports-page.module.css";

export function ImportDocumentReview({
  document,
  documents,
  purchases,
  household,
  previewState,
  evidence,
  review,
  receiptEvidenceIndex,
  onReceiptEvidenceIndexChange,
  pending,
  onPendingChange,
  onDirty,
  onConfirmReceipt,
  onConfirmStatement,
  onError,
  onLinked,
}: {
  document: DocumentReview;
  documents: DocumentReview[];
  purchases: Purchase[];
  household: Household;
  previewState: EvidencePreviewController;
  evidence: ReturnType<typeof useDocumentEvidence>;
  review: ReturnType<typeof useImportConfirmation>;
  receiptEvidenceIndex: string;
  onReceiptEvidenceIndexChange: (index: string) => void;
  pending: boolean;
  onPendingChange: (pending: boolean) => void;
  onDirty: () => void;
  onConfirmReceipt: () => void;
  onConfirmStatement: (entry: StatementEntry) => void;
  onError: (message: string) => void;
  onLinked: () => Promise<void> | void;
}) {
  return (
    <>
      <div className={styles.evidenceReviewLayout}>
        <EvidenceSourcePreview
          document={document}
          previewState={previewState}
          cropRegion={evidence.cropRegion}
          cropPending={evidence.pending}
          pending={pending}
          onCropRegionChange={evidence.setCropRegion}
          onSelectCropRegion={evidence.selectCropRegion}
          onDirty={onDirty}
          onRetryCrop={evidence.retryCrop}
        />
        <div className={styles.evidenceEditColumn}>
          {!!document.result?.receipt?.items?.length && (
            <ReceiptReview
              household={household}
              selectedDocument={document}
              reviewTargets={review.reviewTargets}
              activeReviewIndex={review.activeReviewIndex}
              activeReviewTarget={review.activeReviewTarget}
              paymentDraftFor={review.paymentDraftFor}
              receiptTotal={review.receiptTotal}
              markUnsaved={onDirty}
              setReviewTargetIndex={review.setReviewTargetIndex}
              setReceiptPayments={review.setReceiptPayments}
              editItem={review.editItem}
              onMerchantChange={(merchant) => review.editMerchant(document.id, merchant)}
              suggestedCategoryId={review.suggestedCategoryId}
              selectedCandidates={review.selectedCandidates}
              setSelectedCandidates={review.setSelectedCandidates}
              manualAmountOverrides={review.manualAmountOverrides}
              setManualAmountOverrides={review.setManualAmountOverrides}
              manualAmountOverrideReasons={review.manualAmountOverrideReasons}
              setManualAmountOverrideReasons={review.setManualAmountOverrideReasons}
              pending={pending}
              onConfirm={onConfirmReceipt}
            />
          )}
        </div>
      </div>
      <StatementReview
        document={document}
        confirmedStatements={review.confirmedStatements}
        pending={pending}
        onConfirm={onConfirmStatement}
      />
      {document.result && (
        <EvidenceMatching
          document={document}
          documents={documents}
          purchases={purchases}
          receiptEvidenceIndex={receiptEvidenceIndex}
          onReceiptEvidenceIndexChange={onReceiptEvidenceIndexChange}
          pending={pending}
          onPendingChange={onPendingChange}
          onDirty={onDirty}
          onError={onError}
          onLinked={onLinked}
        />
      )}
    </>
  );
}
