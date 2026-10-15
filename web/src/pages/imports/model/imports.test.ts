import { describe, expect, it } from "vitest";

import {
  canDeleteDocument,
  canReclassifyAttachment,
  canRetryExtraction,
  deferredExtractionMessage,
  defaultUploadSourceKind,
  isExtractionDeferred,
  importModelCostLabel,
  importModelTier,
  importModels,
  parseDocumentFilters,
  retryImportModel,
  serializeDocumentFilters,
  uploadSourceKinds,
  type DocumentReview,
} from "./imports";

const file = (name: string, type = "") => new File([], name, { type });

describe("取り込みAIモデル", () => {
  it("Gemini Developer APIのモデルと無料・有料区分を示す", () => {
    expect(importModels.map((model) => model.value)).toEqual([
      "gemini-3.8-flash",
      "gemini-3.5-flash-lite",
      "gemini-3.1-flash-lite",
      "gemini-3.1-pro-preview",
      "gemini-4-argon",
    ]);
    expect(importModelTier("gemini-3.8-flash")).toBe("free");
    expect(importModelTier("gemini-3.1-pro-preview")).toBe("paid");
    expect(importModelTier("gemini-4-argon")).toBe("paid");
    expect(importModelCostLabel("gemini-3.8-flash")).toBe("無料・利用上限あり");
    expect(importModelCostLabel("gemini-3.1-pro-preview")).toBe("有料・従量課金");
    expect(importModelCostLabel("gemini-4-argon")).toBe("有料・従量課金");
  });
});

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
  it("利用上限の案内を原因に合わせて表示する", () => {
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
    expect(deferredExtractionMessage("quota_reservation_deferred")).toContain("利用枠");
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
    expect(
      canRetryExtraction({
        id: "document-7",
        status: "QUEUED",
        errorCode: "paid_route_unavailable",
      }),
    ).toBe(true);
    expect(deferredExtractionMessage("spend_cap_reached")).toContain("500円");
    expect(deferredExtractionMessage("paid_route_unavailable")).toContain("有料モデル");
    expect(
      isExtractionDeferred({
        id: "document-8",
        status: "FAILED",
        errorCode: "developer_api_consumer_use_restricted",
      }),
    ).toBe(false);
    expect(isExtractionDeferred({ id: "document-3", status: "FAILED" })).toBe(false);
    expect(
      isExtractionDeferred({
        id: "document-4",
        status: "SUCCEEDED",
        errorCode: "rate_limited",
      }),
    ).toBe(false);
    expect(isExtractionDeferred({ id: "document-5", status: "QUEUED", errorCode: "unknown" })).toBe(
      false,
    );
  });
});

describe("再抽出モデルの初期選択", () => {
  it("quota超過時はProを選び、spend capでは無料モデルを選ぶ", () => {
    expect(
      retryImportModel({
        id: "quota",
        status: "FAILED",
        errorCode: "rate_limited",
        model: "gemini-3.8-flash",
      }),
    ).toBe("gemini-3.1-pro-preview");
    expect(
      retryImportModel({
        id: "reservation",
        status: "FAILED",
        errorCode: "quota_reservation_deferred",
        model: "free",
      }),
    ).toBe("gemini-3.1-pro-preview");
    expect(
      retryImportModel({
        id: "spend-cap",
        status: "FAILED",
        errorCode: "spend_cap_reached",
        model: "gemini-3.1-pro-preview",
      }),
    ).toBe("gemini-3.8-flash");
    expect(
      retryImportModel({
        id: "argon-spend-cap",
        status: "FAILED",
        errorCode: "spend_cap_reached",
        model: "gemini-4-argon",
      }),
    ).toBe("gemini-3.8-flash");
    expect(
      retryImportModel({
        id: "paid-unavailable",
        status: "FAILED",
        errorCode: "paid_route_unavailable",
        model: "gemini-3.5-flash-lite",
      }),
    ).toBe("gemini-3.5-flash-lite");
  });

  it("通常の失敗では既存のモデルIDを維持し、tier値を具体的なモデルIDに解決する", () => {
    expect(
      retryImportModel({ id: "exact", status: "FAILED", model: "gemini-3.1-flash-lite" }),
    ).toBe("gemini-3.1-flash-lite");
    expect(retryImportModel({ id: "free-tier", status: "FAILED", model: "free" })).toBe(
      "gemini-3.8-flash",
    );
    expect(retryImportModel({ id: "pro-tier", status: "FAILED", model: "pro" })).toBe(
      "gemini-3.1-pro-preview",
    );
  });
});

describe("抽出エラーの再試行", () => {
  it("失敗した原本はエラー種別に関係なく再試行できる", () => {
    expect(canRetryExtraction({ id: "normal-error", status: "FAILED" })).toBe(true);
    expect(
      canRetryExtraction({
        id: "developer-api-paused",
        status: "FAILED",
        errorCode: "developer_api_consumer_use_restricted",
      }),
    ).toBe(true);
    expect(
      canRetryExtraction({ id: "spend-cap", status: "FAILED", errorCode: "spend_cap_reached" }),
    ).toBe(true);
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
