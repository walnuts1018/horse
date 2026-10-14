export function summarizeSettlementDues(lines: Array<{ amountDueYen: number }>) {
  return lines.reduce(
    (totals, line) => {
      if (line.amountDueYen > 0) totals.fundingDueYen += line.amountDueYen;
      if (line.amountDueYen < 0) totals.creditYen -= line.amountDueYen;
      return totals;
    },
    { fundingDueYen: 0, creditYen: 0 },
  );
}
