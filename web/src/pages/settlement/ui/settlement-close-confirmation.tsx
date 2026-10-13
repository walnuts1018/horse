import { useState } from "react";
import { Check } from "lucide-react";
import type { OpenSettlement } from "../../../shared/api";
import { formatSettlementYen, settlementPeriodLabel } from "../model/settlement-view";

export function SettlementCloseConfirmation({
  settlement,
  period,
  totalDue,
  pending,
  onClose,
  onConfirm,
}: {
  settlement: OpenSettlement;
  period: string;
  totalDue: number;
  pending: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const [accepted, setAccepted] = useState(false);
  const periodLabel = settlementPeriodLabel(period);

  return (
    <section className="panel settlement-close-confirmation" aria-labelledby="close-heading">
      <div className="panel-heading">
        <h2 id="close-heading">{periodLabel}分の確定</h2>
        <b>{formatSettlementYen(totalDue)}</b>
      </div>
      {settlement.lines.map((line) => (
        <div className="settlement-close-line" key={line.memberId}>
          <span>{line.memberName}</span>
          <b>{formatSettlementYen(line.amountDueYen)}</b>
        </div>
      ))}
      <label className="settlement-close-accept">
        <input
          type="checkbox"
          checked={accepted}
          onChange={(event) => setAccepted(event.target.checked)}
        />
        {periodLabel}分を{formatSettlementYen(totalDue)}で確定する
      </label>
      <div className="settlement-actions">
        <button className="button button-soft" type="button" disabled={pending} onClick={onClose}>
          戻る
        </button>
        <button
          className="button button-primary"
          type="button"
          disabled={pending || !accepted}
          onClick={onConfirm}
        >
          <Check size={16} />
          確定する
        </button>
      </div>
    </section>
  );
}
