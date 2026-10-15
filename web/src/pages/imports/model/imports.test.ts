import { describe, expect, it } from "vitest";

import {
  canDeleteDocument,
  canReclassifyAttachment,
  deferredExtractionMessage,
  defaultUploadSourceKind,
  isExtractionDeferred,
  parseDocumentFilters,
  serializeDocumentFilters,
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

describe("保存済み原本の削除", () => {
  it("未確定で処理が終わった単独原本だけを対象にする", () => {
    const document: DocumentReview = { id: "document-1", status: "SUCCEEDED" };

    expect(canDeleteDocument(document)).toBe(true);
    expect(canDeleteDocument({ ...document, status: "FAILED" })).toBe(true);
    expect(canDeleteDocument({ ...document, status: "RUNNING" })).toBe(false);
    expect(canDeleteDocument({ ...document, status: "QUEUED" })).toBe(false);
    expect(canDeleteDocument({ ...document, hasEvidence: true })).toBe(false);
    expect(
      canDeleteDocument({
        ...document,
        confirmations: [{ sourceReference: "row:1", purchaseId: "purchase-1" }],
      }),
    ).toBe(false);
    expect(
      canDeleteDocument({
        ...document,
        attachments: [
          { id: "child", name: "mail", filename: "file.pdf", mimeType: "application/pdf" },
        ],
      }),
    ).toBe(false);
    expect(
      canDeleteDocument({
        ...document,
        attachmentParent: { id: "parent", name: "mail", filename: "mail.eml", index: 0 },
      }),
    ).toBe(false);
  });
});

describe("AIモデルの利用上限", () => {
  it("有料Projectの上限や停止を保留として扱う", () => {
    expect(
      isExtractionDeferred({
        id: "document-1",
        status: "QUEUED",
        errorCode: "rate_limited",
      }),
    ).toBe(true);
    expect(
      isExtractionDeferred({
        id: "document-2",
        status: "FAILED",
        errorCode: "quota_reservation_deferred",
      }),
    ).toBe(true);
    expect(
      isExtractionDeferred({
        id: "document-6",
        status: "QUEUED",
        errorCode: "spend_cap_reached",
      }),
    ).toBe(true);
    expect(
      isExtractionDeferred({
        id: "document-7",
        status: "QUEUED",
        errorCode: "paid_route_unavailable",
      }),
    ).toBe(true);
    expect(deferredExtractionMessage("spend_cap_reached")).toContain("500円");
    expect(deferredExtractionMessage("paid_route_unavailable")).toContain("保留");
    expect(
      isExtractionDeferred({
        id: "document-8",
        status: "FAILED",
        errorCode: "developer_api_consumer_use_restricted",
      }),
    ).toBe(true);
    expect(deferredExtractionMessage("developer_api_consumer_use_restricted")).toContain(
      "一時停止",
    );
    expect(isExtractionDeferred({ id: "document-3", status: "FAILED" })).toBe(false);
    expect(
      isExtractionDeferred({
        id: "document-4",
        status: "SUCCEEDED",
        errorCode: "rate_limited",
      }),
    ).toBe(false);
    expect(
      isExtractionDeferred({ id: "document-5", status: "QUEUED", errorCode: "unknown" }),
    ).toBe(false);
  });
});

describe("取り込み検索条件", () => {
  it("URL検索値を許可された条件だけに正規化する", () => {
    expect(
      parseDocumentFilters({
        store: "スーパー",
        from: "2026-10-01",
        sourceKind: "unknown",
        confirmed: "true",
        ignored: "value",
      }),
    ).toEqual({
      store: "スーパー",
      from: "2026-10-01",
      to: "",
      sourceKind: "",
      confirmed: "true",
    });
  });

  it("空の条件をURLから除き、設定済み条件だけを残す", () => {
    expect(
      serializeDocumentFilters({
        store: "スーパー",
        from: "",
        to: "2026-10-31",
        sourceKind: "",
        confirmed: "false",
      }),
    ).toEqual({ store: "スーパー", to: "2026-10-31", confirmed: "false" });
  });
});
