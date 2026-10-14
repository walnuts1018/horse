import { describe, expect, it } from "vitest";

import type { VerificationCandidate } from "./imports";

import {
  candidateAmountDifferences,
  candidateAmountForItem,
  candidateInterpretationLabels,
} from "./tax-candidate-explanation";

describe("税計算候補の説明", () => {
  it("構造化された税区分と税率だけを短いラベルにする", () => {
    const candidate: VerificationCandidate = {
      id: "candidate-a",
      amounts: [108, 220],
      itemInterpretations: [
        { index: 0, priceBasis: "TAX_EXCLUDED", taxGroup: "reduced" },
        { index: 1, priceBasis: "TAX_EXCLUDED", taxGroup: "standard" },
      ],
      groupInterpretations: [{ id: "reduced", basis: "TAX_EXCLUDED", rounding: "FLOOR" }],
      computedTotal: 328,
      consistent: true,
      issues: [],
    };
    const receipt = {
      items: [
        {
          name: "牛乳",
          printedAmount: 100,
          grossAmount: 100,
          priceBasis: "UNKNOWN",
          taxGroup: "reduced",
          confidence: 1,
          verification: "UNRESOLVED",
        },
        {
          name: "ノート",
          printedAmount: 200,
          grossAmount: 200,
          priceBasis: "UNKNOWN",
          taxGroup: "standard",
          confidence: 1,
          verification: "UNRESOLVED",
        },
      ],
      taxGroups: [
        { id: "reduced", numerator: 8, denominator: 100, basis: "UNKNOWN", rounding: "UNKNOWN" },
        { id: "standard", numerator: 10, denominator: 100, basis: "UNKNOWN", rounding: "UNKNOWN" },
      ],
    };

    expect(candidateInterpretationLabels(candidate, receipt)).toEqual([
      "牛乳 税抜・8%",
      "ノート 税抜・10%",
      "8% 税額 税抜基準",
      "8% 端数切捨て",
    ]);
  });

  it("候補間で金額が変わる品目と選択候補の金額を返す", () => {
    const candidates: VerificationCandidate[] = [
      { id: "a", amounts: [108, 220], computedTotal: 328, consistent: true, issues: [] },
      { id: "b", amounts: [110, 220], computedTotal: 330, consistent: false, issues: [] },
    ];
    const items = [
      {
        name: "牛乳",
        printedAmount: 100,
        grossAmount: 100,
        confidence: 1,
        verification: "DERIVED",
      },
      {
        name: "ノート",
        printedAmount: 200,
        grossAmount: 200,
        confidence: 1,
        verification: "DERIVED",
      },
    ];
    const differences = candidateAmountDifferences(candidates, items);

    expect(differences).toEqual([{ index: 0, name: "牛乳" }]);
    expect(candidateAmountForItem(candidates[1], differences[0])).toEqual({
      index: 0,
      name: "牛乳",
      amount: 110,
    });
  });
});
