import { useEffect, useState, type FormEvent } from "react";
import { makeIdempotencyKey, type Household } from "../../../shared/api";
import { recordSettlementTransfer } from "../api/settlement-mutations";
import { formatSettlementYen } from "./settlement-view";
import {
  clearTransferIntent,
  getTransferIntent,
  saveTransferIntent,
  type TransferIntent,
} from "./pending-ledger-mutations";

const ledgerKindLabels: Record<string, string> = {
  FUNDING: "入金",
  ADVANCE: "立て替え",
  LOAN: "貸付",
  GIFT: "贈与",
};

export function useSettlementTransfer({
  household,
  setPending,
  setError,
  setSuccess,
  onRecorded,
}: {
  household: Household;
  setPending: (pending: boolean) => void;
  setError: (error: string) => void;
  setSuccess: (success: string) => void;
  onRecorded: () => Promise<void>;
}) {
  const [pendingTransfer, setPendingTransfer] = useState<TransferIntent | null>(null);
  const [transferOpen, setTransferOpen] = useState(false);
  const [transferFrom, setTransferFrom] = useState(household.members[0]?.id ?? "");
  const [transferTo, setTransferTo] = useState(household.members[1]?.id ?? "");
  const [transferKind, setTransferKind] = useState<"ADVANCE" | "LOAN" | "GIFT">("ADVANCE");
  const [transferAmount, setTransferAmount] = useState("");
  const [transferDescription, setTransferDescription] = useState("");
  const [transferPaymentAccountId, setTransferPaymentAccountId] = useState("");
  const [transferDestinationAccountId, setTransferDestinationAccountId] = useState("");
  const [hasUnsavedTransfer, setHasUnsavedTransfer] = useState(false);
  const hasPendingTransfer = Boolean(pendingTransfer);

  useEffect(() => {
    try {
      const transfer = getTransferIntent(household.id);
      setPendingTransfer(transfer?.input.kind === "REPAYMENT" ? null : transfer);
      if (transfer) {
        const input = transfer.input;
        if (input.kind !== "REPAYMENT") {
          setTransferKind(input.kind);
          setTransferFrom(input.fromMemberId);
          setTransferTo(input.toMemberId);
          setTransferAmount(String(input.amountYen));
          setTransferDescription(input.description);
          setTransferPaymentAccountId(input.paymentAccountId ?? "");
          setTransferDestinationAccountId(input.destinationAccountId ?? "");
          setTransferOpen(true);
        } else {
          setTransferOpen(false);
        }
        setHasUnsavedTransfer(false);
      }
    } catch (reason) {
      setError((reason as Error).message);
    }
  }, [household.id, household.members, setError]);

  const markUnsaved = () => setHasUnsavedTransfer(true);
  const toggle = (hasPendingLoanRepayment: boolean) => {
    if (transferOpen || hasPendingTransfer || hasPendingLoanRepayment) setHasUnsavedTransfer(false);
    else markUnsaved();
    setTransferOpen(!transferOpen);
  };
  const prepareLoan = () => {
    markUnsaved();
    setTransferKind("LOAN");
    setTransferAmount("");
    setTransferDescription("");
    setError("");
    setTransferOpen(true);
  };
  const save = async (event: FormEvent) => {
    event.preventDefault();
    setPending(true);
    setError("");
    setSuccess("");
    try {
      const existingIntent = getTransferIntent(household.id);
      if (existingIntent?.input.kind === "REPAYMENT") {
        throw new Error("貸付残高から返済を再試行してください");
      }
      const amountYen = Number(transferAmount);
      if (
        !existingIntent &&
        (!transferFrom ||
          !transferTo ||
          transferFrom === transferTo ||
          !Number.isSafeInteger(amountYen) ||
          amountYen < 1)
      ) {
        return;
      }
      const idempotencyKey = makeIdempotencyKey();
      const intent = existingIntent ?? {
        idempotencyKey,
        input: {
          householdId: household.id,
          fromMemberId: transferFrom,
          toMemberId: transferTo,
          paymentAccountId: transferPaymentAccountId || null,
          destinationAccountId: transferDestinationAccountId || null,
          amountYen,
          occurredAt: new Date().toISOString(),
          description: transferDescription.trim() || "メンバー間の送金",
          idempotencyKey,
          kind: transferKind,
        },
      };
      saveTransferIntent(household.id, intent);
      setPendingTransfer(intent);
      if (intent.input.kind === "REPAYMENT") {
        throw new Error("貸付残高から返済を再試行してください");
      }
      await recordSettlementTransfer(intent.input);
      clearTransferIntent(household.id);
      setPendingTransfer(null);
      setTransferAmount("");
      setTransferDescription("");
      setTransferPaymentAccountId("");
      setTransferDestinationAccountId("");
      setTransferOpen(false);
      setHasUnsavedTransfer(false);
      const fromName = household.members.find(
        (member) => member.id === intent.input.fromMemberId,
      )?.displayName;
      const toName = household.members.find(
        (member) => member.id === intent.input.toMemberId,
      )?.displayName;
      setSuccess(
        `${fromName ?? "メンバー"}から${toName ?? "メンバー"}への${formatSettlementYen(intent.input.amountYen)}の${ledgerKindLabels[intent.input.kind]}を記録しました`,
      );
      await onRecorded();
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setPending(false);
    }
  };

  return {
    hasPendingTransfer,
    hasUnsavedTransfer,
    markUnsaved,
    pendingTransfer,
    prepareLoan,
    save,
    transferAmount,
    transferDescription,
    transferDestinationAccountId,
    transferFrom,
    transferKind,
    transferOpen,
    transferPaymentAccountId,
    transferTo,
    toggle,
    setTransferAmount,
    setTransferDescription,
    setTransferDestinationAccountId,
    setTransferFrom,
    setTransferKind,
    setTransferOpen,
    setTransferPaymentAccountId,
    setTransferTo,
  };
}
