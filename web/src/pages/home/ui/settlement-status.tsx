export type SettlementStatusState = "closed" | "open";

export function SettlementStatus({ state }: { state: SettlementStatusState }) {
  const closed = state === "closed";

  return (
    <div className="settle-line">
      <span
        aria-hidden="true"
        className={`status-dot ${closed ? "status-dot--closed" : "status-dot--open"}`}
      />
      {closed ? "締め済み" : "今月分の精算"}
    </div>
  );
}
