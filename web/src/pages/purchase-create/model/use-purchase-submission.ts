import { useRef, useState, type SyntheticEvent } from "react";

import type { PaymentDraft } from "./purchase-payments";

import { makeIdempotencyKey, type Household } from "../../../shared/api";
import { enqueuePurchase, errorMessage, type PurchaseDraftItem } from "../../../shared/lib";
import { convertPurchaseItems } from "./purchase-items";

type SubmissionOverrides = {
  items: PurchaseDraftItem[];
  paymentAmount: (payment: PaymentDraft) => number;
  paymentDifference: number;
};

type SubmissionFieldError =
  | { field: "occurredAt" }
  | { field: "paymentAccount" | "paymentAmount"; paymentKey: string };

export function usePurchaseSubmission({
  memberId,
  household,
  merchant,
  occurredAt,
  items,
  paymentRows,
  paymentAutoTotal,
  paymentAmount,
  paymentDifference,
  cancelScheduledSave,
  onQueued,
  onSaved,
  idempotencyKey: sharedDraftId,
}: {
  memberId: string;
  household: Household;
  merchant: string;
  occurredAt: string;
  items: PurchaseDraftItem[];
  paymentRows: PaymentDraft[];
  paymentAutoTotal: boolean;
  paymentAmount: (payment: PaymentDraft) => number;
  paymentDifference: number;
  cancelScheduledSave: () => void;
  onQueued: (idempotencyKey: string) => void;
  onSaved: () => void;
  idempotencyKey?: string;
}) {
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");
  const [fieldError, setFieldError] = useState<SubmissionFieldError | null>(null);
  const saving = useRef(false);

  const submit = async (event: SyntheticEvent, overrides?: SubmissionOverrides) => {
    event.preventDefault();
    if (saving.current) return;
    const submittedItems = overrides?.items ?? items;
    const submittedPaymentAmount = overrides?.paymentAmount ?? paymentAmount;
    const submittedPaymentDifference = overrides?.paymentDifference ?? paymentDifference;
    setFieldError(null);
    if (!memberId.trim()) {
      setMessage("ログイン中のメンバーを確認できません");
      return;
    }
    if (!household.members.some((member) => member.id === memberId)) {
      setMessage("この家計簿のメンバーを確認できません");
      return;
    }
    if (!submittedItems.length) {
      setMessage("品目を1つ以上追加してください");
      return;
    }
    if (!Number.isFinite(new Date(occurredAt).getTime())) {
      setFieldError({ field: "occurredAt" });
      setMessage("");
      return;
    }
    if (!household.accounts.length) {
      setMessage("設定で決済元を登録してください");
      return;
    }
    const missingPaymentAccount = paymentRows.find((payment) => !payment.paymentAccountId);
    if (missingPaymentAccount) {
      setFieldError({ field: "paymentAccount", paymentKey: missingPaymentAccount.key });
      setMessage("");
      return;
    }
    const autoRemainderKey = paymentAutoTotal ? paymentRows.at(-1)?.key : undefined;
    const invalidPaymentAmount = paymentRows.find(
      (payment) => payment.key !== autoRemainderKey && submittedPaymentAmount(payment) < 1,
    );
    if (invalidPaymentAmount) {
      setFieldError({ field: "paymentAmount", paymentKey: invalidPaymentAmount.key });
      setMessage("");
      return;
    }
    if (submittedPaymentDifference !== 0) {
      const editablePayments = paymentRows.filter((payment) => payment.key !== autoRemainderKey);
      if (editablePayments.length === 1) {
        setFieldError({ field: "paymentAmount", paymentKey: editablePayments[0].key });
        setMessage("");
        return;
      }
      setMessage("決済元と金額を確認してください");
      return;
    }
    if (!paymentRows.length) {
      setMessage("決済元と金額を確認してください");
      return;
    }

    saving.current = true;
    setPending(true);
    setMessage("");
    try {
      cancelScheduledSave();
      const idempotencyKey = sharedDraftId ?? makeIdempotencyKey();
      await enqueuePurchase(
        {
          idempotencyKey,
          householdId: household.id,
          occurredAt: new Date(occurredAt).toISOString(),
          merchant: merchant.trim(),
          origin: "MANUAL",
          paymentAllocations: paymentRows.map((payment) => ({
            paymentAccountId: payment.paymentAccountId,
            amountYen: submittedPaymentAmount(payment),
          })),
          items: convertPurchaseItems(submittedItems),
        },
        memberId,
        { preserveLocalDraft: Boolean(sharedDraftId) },
      );
      onSaved();
      onQueued(idempotencyKey);
    } catch (reason) {
      setMessage(errorMessage(reason));
    } finally {
      saving.current = false;
      setPending(false);
    }
  };

  const clearFieldError = (field: SubmissionFieldError["field"], paymentKey?: string) => {
    setFieldError((current) =>
      current?.field === field &&
      (current.field === "occurredAt" || current.paymentKey === paymentKey)
        ? null
        : current,
    );
  };

  return { pending, message, setMessage, fieldError, clearFieldError, submit };
}
