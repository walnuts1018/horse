import { useState } from "react";
import { Plus, X } from "lucide-react";
import {
  makeIdempotencyKey,
  type Household,
  type Purchase,
  type PurchaseItem,
  type PurchasePaymentInput,
} from "../../../shared/api";
import { useCorrectPurchaseMutation } from "../../../entities/purchase";
import { getRemainingRefundAmount } from "../model/purchase-correction";
import { InlineError } from "../../../shared/ui";

type RefundPayment = {
  key: string;
  paymentAccountId: string;
  amountYen: string;
};

export function PurchaseCorrection({
  householdId,
  accounts,
  purchase,
  item,
  onCancel,
  onSaved,
}: {
  householdId: string;
  accounts: Household["accounts"];
  purchase: Purchase;
  item: PurchaseItem;
  onCancel: () => void;
  onSaved: () => Promise<void>;
}) {
  const { mutate } = useCorrectPurchaseMutation();
  const [refundAmount, setRefundAmount] = useState("");
  const [refundReason, setRefundReason] = useState("");
  const [refundPayments, setRefundPayments] = useState<RefundPayment[]>([]);
  const [refundAutoTotal, setRefundAutoTotal] = useState(true);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const remaining = getRemainingRefundAmount(purchase, item);
  const refundPaymentTotal = refundPayments.reduce(
    (sum, payment) =>
      sum +
      Number(refundAutoTotal && refundPayments.length === 1 ? refundAmount : payment.amountYen),
    0,
  );
  const yen = (amount = 0) =>
    new Intl.NumberFormat("ja-JP", {
      style: "currency",
      currency: "JPY",
      maximumFractionDigits: 0,
    }).format(amount);

  const addRefundPayment = () => {
    setRefundPayments((current) => [
      ...current.map((payment) => ({
        ...payment,
        amountYen: refundAutoTotal && current.length === 1 ? refundAmount : payment.amountYen,
      })),
      {
        key: crypto.randomUUID(),
        paymentAccountId: "",
        amountYen: current.length === 0 ? refundAmount : "",
      },
    ]);
    setRefundAutoTotal(false);
  };

  const submit = async () => {
    const amountYen = Number(refundAmount);
    const paymentAllocations: PurchasePaymentInput[] = refundPayments.map((payment) => ({
      paymentAccountId: payment.paymentAccountId,
      amountYen: Number(
        refundAutoTotal && refundPayments.length === 1 ? refundAmount : payment.amountYen,
      ),
    }));
    const refundTotal = paymentAllocations.reduce((sum, payment) => sum + payment.amountYen, 0);
    if (!Number.isSafeInteger(amountYen) || amountYen < 1 || amountYen > remaining) {
      setError("返金額を確認してください");
      return;
    }
    if (
      refundPayments.some(
        (payment, index) =>
          !payment.paymentAccountId ||
          !Number.isSafeInteger(paymentAllocations[index].amountYen) ||
          paymentAllocations[index].amountYen < 1,
      ) ||
      (paymentAllocations.length > 0 && refundTotal !== amountYen)
    ) {
      setError("返金先と返金額を確認してください");
      return;
    }
    if (!refundReason.trim()) {
      setError("理由を入力してください");
      return;
    }
    const allocations = item.allocations
      .filter((allocation) => allocation.weight > 0)
      .map(({ poolId, weight }) => ({ poolId, weight }));
    if (!allocations.length) {
      setError("元品目の負担先がありません");
      return;
    }

    setPending(true);
    setError("");
    try {
      await mutate({
        input: {
          householdId,
          target: { item: { purchaseId: purchase.id, itemId: item.id } },
          amountDeltaYen: -amountYen,
          reason: refundReason.trim(),
          allocations,
          paymentAllocations,
          idempotencyKey: makeIdempotencyKey(),
        },
      });
      await onSaved();
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setPending(false);
    }
  };

  return (
    <form
      className="purchase-correction-form"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <b>{item.name}の返品</b>
      <span>残額 {yen(Math.max(0, remaining))}</span>
      <label>
        返金額
        <input
          aria-label="返金額"
          inputMode="numeric"
          value={refundAmount}
          max={remaining}
          onChange={(event) => setRefundAmount(event.target.value.replace(/\D/g, ""))}
          required
        />
      </label>
      <label>
        理由
        <input
          aria-label="返品理由"
          value={refundReason}
          onChange={(event) => setRefundReason(event.target.value)}
          required
        />
      </label>
      {refundPayments.length === 0 ? (
        <span>返金先なし</span>
      ) : (
        <div className="refund-payment-list">
          {refundPayments.map((payment, index) => (
            <div className="refund-payment-row" key={payment.key}>
              <select
                aria-label={"返金先 " + (index + 1)}
                value={payment.paymentAccountId}
                onChange={(event) =>
                  setRefundPayments((current) =>
                    current.map((row) =>
                      row.key === payment.key
                        ? { ...row, paymentAccountId: event.target.value }
                        : row,
                    ),
                  )
                }
              >
                <option value="">口座を選択</option>
                {accounts.map((account) => (
                  <option key={account.id} value={account.id}>
                    {account.name}
                  </option>
                ))}
              </select>
              <input
                aria-label={"返金額 " + (index + 1)}
                inputMode="numeric"
                value={
                  refundAutoTotal && refundPayments.length === 1 ? refundAmount : payment.amountYen
                }
                onChange={(event) => {
                  setRefundAutoTotal(false);
                  setRefundPayments((current) =>
                    current.map((row) =>
                      row.key === payment.key
                        ? { ...row, amountYen: event.target.value.replace(/\D/g, "") }
                        : row,
                    ),
                  );
                }}
              />
              <button
                className="row-remove"
                type="button"
                aria-label={"返金先 " + (index + 1) + " を削除"}
                onClick={() =>
                  setRefundPayments((current) => current.filter((row) => row.key !== payment.key))
                }
              >
                <X size={14} />
              </button>
            </div>
          ))}
        </div>
      )}
      <button className="text-button" type="button" onClick={addRefundPayment}>
        <Plus size={14} />
        返金先を追加
      </button>
      {refundPayments.length > 0 && (
        <span className="payment-difference">
          返金差額 {yen(Number(refundAmount) - refundPaymentTotal)}
        </span>
      )}
      {error && <InlineError message={error} />}
      <div className="edit-actions">
        <button className="button button-soft" type="button" disabled={pending} onClick={onCancel}>
          キャンセル
        </button>
        <button className="button button-primary" disabled={pending || remaining <= 0}>
          返品を記録
        </button>
      </div>
    </form>
  );
}
