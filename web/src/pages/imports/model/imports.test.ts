import { describe, expect, it } from "vitest";
import { canReclassifyAttachment, type DocumentReview } from "./imports";

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
