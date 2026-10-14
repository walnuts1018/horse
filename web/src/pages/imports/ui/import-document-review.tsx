import { useState } from "react";

import type { Household, Purchase } from "../../../shared/api";
import type { EvidencePreviewController } from "../model/evidence-preview";
import type { useDocumentEvidence } from "../model/use-document-evidence";
import type { useImportConfirmation } from "../model/use-import-confirmation";

import {
  statusLabel,
  statementSourceReference,
  type DocumentReview,
  type StatementEntry,
} from "../model/imports";
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
  onConfirmRefund,
  onConfirmTransfer,
  onConfirmLoan,
  onLinkExistingTransfer,
  onError,
  onLinked,
  onSelectDocument,
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
  onConfirmStatement: (entry: StatementEntry, accountId: string) => void;
  onConfirmRefund: (entry: StatementEntry, purchaseId: string, itemId?: string) => void;
  onConfirmTransfer: (
    entry: StatementEntry,
    statementAccountId: string,
    otherAccountId: string,
  ) => void;
  onConfirmLoan: (
    entry: StatementEntry,
    accountId: string,
    fromMemberId: string,
    toMemberId: string,
    loanId?: string,
  ) => void;
  onLinkExistingTransfer: (entry: StatementEntry, accountId: string, transferId: string) => void;
  onError: (message: string) => void;
  onLinked: () => Promise<void> | void;
  onSelectDocument: (id: string) => void;
}) {
  const [selectedStatementRegion, setSelectedStatementRegion] = useState<{
    documentId: string;
    reference: string;
  } | null>(null);
  const selectedStatementReference =
    selectedStatementRegion?.documentId === document.id ? selectedStatementRegion.reference : "";
  const selectStatementRegion = (reference: string, page: number) => {
    setSelectedStatementRegion({ documentId: document.id, reference });
    previewState.selectPage(page);
  };

  return (
    <>
      {document.attachmentParent && (
        <section className={styles.attachmentGroup}>
          <span className="eyebrow">メール添付</span>
          <button
            type="button"
            className="text-button"
            onClick={() => onSelectDocument(document.attachmentParent!.id)}
          >
            {document.attachmentParent.name}
          </button>
          <small>{document.attachmentParent.filename}</small>
        </section>
      )}
      {!!document.attachments?.length && (
        <section className={styles.attachmentGroup}>
          <span className="eyebrow">添付ファイル</span>
          {document.attachments.map((attachment) => {
            return (
              <div className={styles.attachmentRow} key={attachment.id}>
                <button
                  type="button"
                  className="text-button"
                  onClick={() => onSelectDocument(attachment.id)}
                >
                  {attachment.filename}
                </button>
                <small>{attachment.status ? statusLabel(attachment.status) : "処理待ち"}</small>
              </div>
            );
          })}
        </section>
      )}
      <div className={styles.evidenceReviewLayout}>
        <EvidenceSourcePreview
          document={document}
          previewState={previewState}
          cropRegion={evidence.cropRegion}
          cropPending={evidence.pending}
          selectedStatementReference={selectedStatementReference}
          onSelectStatementRegion={selectStatementRegion}
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
              purchaseDate={review.purchaseDateFor(document)}
              markUnsaved={onDirty}
              setReviewTargetIndex={review.setReviewTargetIndex}
              setReceiptPayments={review.setReceiptPayments}
              editItem={review.editItem}
              onPurchaseDateChange={(date) => review.editPurchaseDate(document.id, date)}
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
        purchases={purchases}
        household={household}
        accounts={household.accounts}
        confirmedStatements={review.confirmedStatements}
        pending={pending}
        onConfirm={onConfirmStatement}
        onConfirmRefund={onConfirmRefund}
        onConfirmTransfer={onConfirmTransfer}
        onConfirmLoan={onConfirmLoan}
        onLinkExistingTransfer={onLinkExistingTransfer}
        onSelectRegion={(entry) => {
          if (entry.region)
            selectStatementRegion(statementSourceReference(entry), entry.region.page);
        }}
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
