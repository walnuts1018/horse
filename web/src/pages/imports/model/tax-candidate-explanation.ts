import type { ExtractedItem, VerificationCandidate } from "./imports";

type ReceiptTaxGroup = {
  id: string;
  numerator: number;
  denominator: number;
  basis?: string;
  rounding?: string;
};

type ReceiptTaxData = {
  items: ExtractedItem[];
  taxGroups: ReceiptTaxGroup[];
};

export type CandidateAmountDifference = {
  index: number;
  name: string;
};

const basisLabels: Record<string, string> = {
  TAX_INCLUDED: "税込",
  TAX_EXCLUDED: "税抜",
  EXEMPT: "免税",
  NON_TAXABLE: "非課税",
};

const roundingLabels: Record<string, string> = {
  FLOOR: "端数切捨て",
  CEIL: "端数切り上げ",
  NEAREST: "四捨五入",
};

function taxRateLabel(group: ReceiptTaxGroup | undefined): string | undefined {
  if (!group || group.denominator <= 0 || group.numerator < 0) return undefined;
  const percentageNumerator = group.numerator * 100;
  if (percentageNumerator % group.denominator === 0) {
    return `${percentageNumerator / group.denominator}%`;
  }
  return `${group.numerator}/${group.denominator}率`;
}

export function candidateInterpretationLabels(
  candidate: VerificationCandidate,
  receipt: ReceiptTaxData,
): string[] {
  const labels: string[] = [];

  for (const interpretation of candidate.itemInterpretations ?? []) {
    const item = receipt.items.at(interpretation.index);
    if (!item) continue;
    const basis = basisLabels[interpretation.priceBasis];
    const group = receipt.taxGroups.find((entry) => entry.id === interpretation.taxGroup);
    const rate = taxRateLabel(group);
    const changed =
      interpretation.priceBasis !== item.priceBasis || interpretation.taxGroup !== item.taxGroup;
    if (!changed || !basis) continue;
    const name = item.name || `品目${interpretation.index + 1}`;
    labels.push(`${name} ${basis}${rate ? `・${rate}` : ""}`);
  }

  for (const interpretation of candidate.groupInterpretations ?? []) {
    const group = receipt.taxGroups.find((entry) => entry.id === interpretation.id);
    if (!group) continue;
    const rate = taxRateLabel(group);
    const basis = basisLabels[interpretation.basis];
    if (basis && interpretation.basis !== group.basis) {
      labels.push(`${rate ? `${rate} ` : ""}税額 ${basis}基準`);
    }
    const rounding = roundingLabels[interpretation.rounding];
    if (rounding && interpretation.rounding !== group.rounding) {
      labels.push(`${rate ? `${rate} ` : ""}${rounding}`);
    }
  }

  return [...new Set(labels)];
}

export function candidateAmountDifferences(
  candidates: VerificationCandidate[],
  items: ExtractedItem[],
): CandidateAmountDifference[] {
  return items.flatMap((item, index) => {
    const amounts = candidates
      .map((candidate) => candidate.amounts[index])
      .filter(Number.isSafeInteger);
    if (new Set(amounts).size < 2) return [];
    return [{ index, name: item.name || `品目${index + 1}` }];
  });
}

export function candidateAmountForItem(
  candidate: VerificationCandidate,
  difference: CandidateAmountDifference,
): (CandidateAmountDifference & { amount: number }) | undefined {
  const amount = candidate.amounts[difference.index];
  return Number.isSafeInteger(amount) ? { ...difference, amount } : undefined;
}
