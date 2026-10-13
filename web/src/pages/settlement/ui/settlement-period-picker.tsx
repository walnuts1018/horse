import { ChevronLeft, ChevronRight } from "lucide-react";

export function SettlementPeriodPicker({
  period,
  onChange,
}: {
  period: string;
  onChange: (period: string) => void;
}) {
  return (
    <label className="period-picker">
      <ChevronLeft size={15} />
      <input type="month" value={period} onChange={(event) => onChange(event.target.value)} />
      <ChevronRight size={15} />
    </label>
  );
}
