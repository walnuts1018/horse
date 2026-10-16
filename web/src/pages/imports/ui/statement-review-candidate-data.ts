import type { Household } from "../../../shared/api";

import { useStatementLedgerCandidateData } from "./statement-ledger-candidate-data";
import { useStatementRefundCandidateData } from "./statement-refund-candidate-data";

export function useStatementReviewCandidateData({
  documentId,
  household,
}: {
  documentId: string;
  household: Household;
}) {
  const ledger = useStatementLedgerCandidateData(household);
  const refunds = useStatementRefundCandidateData({ documentId, household });

  return { ...ledger, ...refunds };
}
