import { ChevronLeft, ChevronRight } from "lucide-react";

function shiftPeriod(period: string, amount: number) {
  const match = /^(\d{4})-(0[1-9]|1[0-2])$/.exec(period);
  if (!match) return period;

  const monthIndex = Number(match[2]) - 1 + amount;
  const year = Number(match[1]) + Math.floor(monthIndex / 12);
  const month = (((monthIndex % 12) + 12) % 12) + 1;
  return `${String(year).padStart(4, "0")}-${String(month).padStart(2, "0")}`;
}

export function SettlementPeriodPicker({
  period,
  onChange,
}: {
  period: string;
  onChange: (period: string) => void;
}) {
  return (
    <div className="period-picker">
      <button
        className="period-picker-nav"
        type="button"
        aria-label="前の月"
        disabled={!/^\d{4}-(0[1-9]|1[0-2])$/.test(period)}
        onClick={() => onChange(shiftPeriod(period, -1))}
      >
        <ChevronLeft size={15} />
      </button>
      <input
        type="month"
        aria-label="精算期間"
        value={period}
        onChange={(event) => onChange(event.target.value)}
      />
      <button
        className="period-picker-nav"
        type="button"
        aria-label="次の月"
        disabled={!/^\d{4}-(0[1-9]|1[0-2])$/.test(period)}
        onClick={() => onChange(shiftPeriod(period, 1))}
      >
        <ChevronRight size={15} />
      </button>
    </div>
  );
}
