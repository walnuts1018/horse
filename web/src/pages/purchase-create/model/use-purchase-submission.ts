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

export function usePurchaseSubmission({
  memberId,
  household,
  merchant,
  occurredAt,
  items,
  paymentRows,
  paymentAmount,
  paymentDifference,
  cancelScheduledSave,
  onQueued,
  onSaved,
}: {
  memberId: string;
  household: Household;
  merchant: string;
  occurredAt: string;
  items: PurchaseDraftItem[];
  paymentRows: PaymentDraft[];
  paymentAmount: (payment: PaymentDraft) => number;
  paymentDifference: number;
  cancelScheduledSave: () => void;
  onQueued: (idempotencyKey: string) => void;
  onSaved: () => void;
}) {
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");
  const saving = useRef(false);

  const submit = async (event: SyntheticEvent, overrides?: SubmissionOverrides) => {
    event.preventDefault();
    if (saving.current) return;
    const submittedItems = overrides?.items ?? items;
    const submittedPaymentAmount = overrides?.paymentAmount ?? paymentAmount;
    const submittedPaymentDifference = overrides?.paymentDifference ?? paymentDifference;
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
    if (!household.accounts.length) {
      setMessage("設定で決済元を登録してください");
      return;
    }
    if (
      !paymentRows.length ||
      paymentRows.some(
        (payment) => !payment.paymentAccountId || submittedPaymentAmount(payment) < 1,
      ) ||
      submittedPaymentDifference !== 0
    ) {
      setMessage("決済元と金額を確認してください");
      return;
    }

    saving.current = true;
    setPending(true);
    setMessage("");
    try {
      cancelScheduledSave();
      const idempotencyKey = makeIdempotencyKey();
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

  return { pending, message, setMessage, submit };
}
