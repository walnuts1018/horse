import { describe, expect, it } from "vitest";

import { parsePastedPurchaseItems } from "./pasted-purchase-items";

describe("pasted purchase items", () => {
  it("TSVの金額に含まれるカンマを保持する", () => {
    expect(parsePastedPurchaseItems("ヨーグルト\t1,280\n牛乳\t248")).toMatchObject({
      items: [
        { name: "ヨーグルト", amount: "1280" },
        { name: "牛乳", amount: "248" },
      ],
      invalidRowCount: 0,
      invalidSyntax: false,
    });
  });

  it("CSVの引用フィールドとエスケープされた引用符を解析する", () => {
    expect(
      parsePastedPurchaseItems('"ヨーグルト, 無糖","1,280"\n"""特売""牛乳",248'),
    ).toMatchObject({
      items: [
        { name: "ヨーグルト, 無糖", amount: "1280" },
        { name: '"特売"牛乳', amount: "248" },
      ],
      invalidRowCount: 0,
      invalidSyntax: false,
    });
  });

  it("解析できない行数を返し、閉じていない引用符を報告する", () => {
    expect(parsePastedPurchaseItems('牛乳,248\n卵,"128')).toMatchObject({
      invalidSyntax: true,
    });
  });
});
