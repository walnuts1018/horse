import { describe, expect, it } from "vitest";

import {
  canReclassifyAttachment,
  defaultUploadSourceKind,
  uploadSourceKinds,
  type DocumentReview,
} from "./imports";

const file = (name: string, type = "") => new File([], name, { type });

describe("アップロード時の原本種別", () => {
  it("CSVは従来どおりカード明細を初期値にしてカードか銀行を選べる", () => {
    const csv = file("statement.csv", "text/csv");

    expect(defaultUploadSourceKind(csv)).toBe("card");
    expect(uploadSourceKinds(csv)).toEqual(["card", "bank"]);
  });

  it("EMLはメールとして扱い、ほかのファイルは4種から選べる", () => {
    const eml = file("order.eml", "message/rfc822");
    const pdf = file("statement.pdf", "application/pdf");

    expect(defaultUploadSourceKind(eml)).toBe("email");
    expect(uploadSourceKinds(eml)).toEqual([]);
    expect(defaultUploadSourceKind(pdf)).toBe("receipt");
    expect(uploadSourceKinds(pdf)).toEqual(["receipt", "order", "card", "bank"]);
  });
});

describe("添付文書の明細種別変更", () => {
  it("未確定で処理中ではない添付文書だけを対象にする", () => {
    const attachment: DocumentReview = {
      id: "attachment-1",
      status: "FAILED",
      attachmentParent: { id: "email-1", name: "注文メール", filename: "order.eml", index: 0 },
    };

    expect(canReclassifyAttachment(attachment)).toBe(true);
    expect(canReclassifyAttachment({ ...attachment, hasEvidence: false })).toBe(true);
    expect(canReclassifyAttachment({ ...attachment, hasEvidence: true })).toBe(false);
    expect(canReclassifyAttachment({ ...attachment, sourceKind: "card" }, "card")).toBe(false);
    expect(canReclassifyAttachment({ ...attachment, sourceKind: "card" }, "bank")).toBe(true);
    expect(canReclassifyAttachment({ ...attachment, attachmentParent: undefined })).toBe(false);
    expect(
      canReclassifyAttachment({
        ...attachment,
        confirmations: [{ sourceReference: "row:1", purchaseId: "purchase-1" }],
      }),
    ).toBe(false);
    expect(canReclassifyAttachment({ ...attachment, status: "RUNNING" })).toBe(false);
  });
});
