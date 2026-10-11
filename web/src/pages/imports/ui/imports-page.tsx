import { useCallback, useEffect, useRef, useState } from "react";
import { Check, ChevronRight, FileImage, ShieldCheck, Upload, X } from "lucide-react";
import {
  createPurchaseMutation,
  makeIdempotencyKey,
  readGraphql,
  type Household,
} from "../../../shared/api";
import { InlineError } from "../../../shared/ui";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const sourceKind = (file: File, csvKind: "card" | "bank") =>
  /\.csv$/i.test(file.name) ? csvKind : /\.eml$/i.test(file.name) ? "email" : "receipt";

type ExtractedItem = {
  name: string;
  printedAmount: number;
  grossAmount: number;
  confidence: number;
  verification: string;
  categoryHint?: string;
};
type StatementEntry = {
  row: number;
  kind: string;
  occurredAt: string;
  merchant: string;
  amount: number;
  transactionId?: string;
  orderId?: string;
  cardLast4?: string;
};
type VerificationCandidate = {
  id: string;
  amounts: number[];
  computedTotal: number;
  consistent: boolean;
  issues: string[];
};
type DocumentReview = {
  id: string;
  name?: string;
  status: string;
  sourceKind?: string;
  mimeType?: string;
  createdAt?: string;
  sha256?: string;
  duplicateOf?: string;
  result?: {
    receipt?: { merchant?: string; purchasedAt?: string; items?: ExtractedItem[] };
    computedTotal?: number;
    balanced?: boolean;
    candidates?: VerificationCandidate[];
    issues?: string[];
    ocrLines?: Array<{ text: string; confidence: number }>;
    statements?: StatementEntry[];
  };
};
const terminalStatuses = new Set([
  "READY",
  "REVIEW",
  "COMPLETED",
  "SUCCEEDED",
  "FAILED",
  "DUPLICATE",
  "DONE",
]);
const statusLabel = (status: string) =>
  ({
    QUEUED: "待機中",
    RUNNING: "抽出中",
    READY: "確認待ち",
    REVIEW: "確認待ち",
    COMPLETED: "抽出済み",
    SUCCEEDED: "抽出済み",
    FAILED: "失敗",
    DUPLICATE: "重複",
    DONE: "抽出済み",
  })[status.toUpperCase()] ?? "状態を確認中";
const sourceKindLabel = (sourceKind?: string) =>
  ({
    receipt: "レシート",
    order: "注文履歴",
    email: "メール",
    card: "カード明細",
    bank: "銀行明細",
  })[sourceKind ?? ""] ?? "証拠";
const isProcessing = (status: string) => !terminalStatuses.has(status.toUpperCase());

export function ImportsPage({
  household,
  onAdded,
}: {
  household: Household;
  onAdded: (keepOpen?: boolean) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [files, setFiles] = useState<File[]>([]);
  const [csvKinds, setCsvKinds] = useState<Record<string, "card" | "bank">>({});
  const [documents, setDocuments] = useState<DocumentReview[]>([]);
  const [dragging, setDragging] = useState(false);
  const [message, setMessage] = useState("");
  const [pending, setPending] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [confirmedStatements, setConfirmedStatements] = useState<Set<string>>(() => new Set());
  const [selectedCandidates, setSelectedCandidates] = useState<Record<string, string>>({});
  const [paymentAccountId, setPaymentAccountId] = useState(household.accounts[0]?.id ?? "");
  const selectedDocument = documents.find((document) => document.id === selected);
  const refreshDocuments = useCallback(
    async (silent = false) => {
      try {
        const response = await fetch(
          `/documents?householdId=${encodeURIComponent(household.id)}&first=50`,
          { credentials: "include" },
        );
        if (!response.ok) throw new Error(`証拠一覧を取得できませんでした (${response.status})`);
        const payload = (await response.json()) as { documents: DocumentReview[] };
        setDocuments((current) =>
          payload.documents.map((document) => ({
            ...document,
            name:
              current.find((existing) => existing.id === document.id)?.name ??
              `${sourceKindLabel(document.sourceKind)} · ${
                document.createdAt
                  ? new Date(document.createdAt).toLocaleString("ja-JP", {
                      month: "numeric",
                      day: "numeric",
                      hour: "2-digit",
                      minute: "2-digit",
                    })
                  : document.id.slice(0, 8)
              }`,
          })),
        );
        setSelected((current) => current ?? payload.documents[0]?.id ?? null);
      } catch (reason) {
        if (!silent) setMessage((reason as Error).message);
      }
    },
    [household.id],
  );
  useEffect(() => {
    setDocuments([]);
    setSelected(null);
    setConfirmedStatements(new Set());
    setSelectedCandidates({});
    void refreshDocuments();
  }, [refreshDocuments]);
  const hasProcessingDocuments = documents.some((document) => isProcessing(document.status));
  useEffect(() => {
    if (!hasProcessingDocuments) return;
    const interval = window.setInterval(() => void refreshDocuments(true), 1200);
    return () => window.clearInterval(interval);
  }, [hasProcessingDocuments, refreshDocuments]);
  const addFiles = (selected: FileList | null) => {
    if (!selected) return;
    setFiles((current) => [
      ...current,
      ...Array.from(selected).filter(
        (file) =>
          file.type.startsWith("image/") ||
          file.type === "application/pdf" ||
          file.type === "text/csv" ||
          file.type === "application/vnd.ms-excel" ||
          file.type === "message/rfc822" ||
          /\.(csv|eml)$/i.test(file.name),
      ),
    ]);
  };
  const uploadFiles = async () => {
    if (!files.length) return;
    setPending(true);
    setMessage("");
    const uploaded: DocumentReview[] = [];
    try {
      for (const file of files) {
        const body = new FormData();
        body.set("file", file);
        body.set("householdId", household.id);
        body.set("sourceKind", sourceKind(file, csvKinds[file.name] ?? "card"));
        const response = await fetch("/documents", {
          method: "POST",
          credentials: "include",
          body,
        });
        if (!response.ok) throw new Error(`証拠を送信できませんでした (${response.status})`);
        const accepted = (await response.json()) as {
          id: string;
          sha256: string;
          status: string;
          duplicateOf?: string;
          duplicate?: boolean;
          jobId?: string;
        };
        let documentReview: DocumentReview = {
          id: accepted.id,
          name: file.name,
          status: accepted.status,
          sourceKind: sourceKind(file, csvKinds[file.name] ?? "card"),
          duplicateOf: accepted.duplicateOf,
          sha256: accepted.sha256,
        };
        for (let attempt = 0; attempt < 24 && isProcessing(documentReview.status); attempt++) {
          await new Promise((resolve) => window.setTimeout(resolve, 1000));
          const resultResponse = await fetch(`/documents/${encodeURIComponent(accepted.id)}`, {
            credentials: "include",
          });
          if (!resultResponse.ok)
            throw new Error(`認識結果を取得できませんでした (${resultResponse.status})`);
          documentReview = {
            ...documentReview,
            ...((await resultResponse.json()) as Partial<DocumentReview>),
            name: file.name,
          };
        }
        uploaded.push(documentReview);
      }
      setDocuments((current) => [...uploaded, ...current]);
      setFiles([]);
      setSelected(uploaded.find((document) => document.result)?.id ?? uploaded[0]?.id ?? null);
      void refreshDocuments(true);
    } catch (reason) {
      setMessage((reason as Error).message);
      setDocuments((current) => [...uploaded, ...current]);
    } finally {
      setPending(false);
    }
  };
  const retryDocument = async (document: DocumentReview) => {
    setPending(true);
    setMessage("");
    try {
      const response = await fetch(`/documents/${encodeURIComponent(document.id)}/retry`, {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ hints: [] }),
      });
      if (!response.ok) throw new Error(`再抽出を開始できませんでした (${response.status})`);
      await refreshDocuments(true);
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const editItem = (documentId: string, index: number, patch: Partial<ExtractedItem>) =>
    setDocuments((current) =>
      current.map((document) =>
        document.id !== documentId || !document.result?.receipt?.items
          ? document
          : {
              ...document,
              result: {
                ...document.result,
                receipt: {
                  ...document.result.receipt,
                  items: document.result.receipt.items.map((item, itemIndex) =>
                    itemIndex === index ? { ...item, ...patch } : item,
                  ),
                },
              },
            },
      ),
    );
  const confirm = async (document: DocumentReview) => {
    const receipt = document.result?.receipt;
    const items = receipt?.items ?? [];
    if (!items.length) {
      setMessage("確定できる品目がありません");
      return;
    }
    if (document.result?.candidates?.length && !selectedCandidates[document.id]) {
      setMessage("税計算候補を選択してください");
      return;
    }
    setPending(true);
    setMessage("");
    try {
      await readGraphql(createPurchaseMutation, {
        input: {
          idempotencyKey: makeIdempotencyKey(),
          householdId: household.id,
          sourceDocumentId: document.id,
          occurredAt: receipt?.purchasedAt
            ? new Date(receipt.purchasedAt).toISOString()
            : new Date().toISOString(),
          merchant: receipt?.merchant ?? "",
          paymentAccountId: paymentAccountId || null,
          items: items.map((item, index) => ({
            name: item.name,
            amountYen:
              document.result?.candidates?.find(
                (candidate) => candidate.id === selectedCandidates[document.id],
              )?.amounts[index] ??
              (item.grossAmount || item.printedAmount),
            allocations: null,
          })),
        },
      });
      onAdded();
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const confirmStatement = async (document: DocumentReview, entry: StatementEntry) => {
    if (!document.result || entry.kind !== "CARD_PURCHASE") return;
    const reference = entry.transactionId
      ? `transaction:${entry.transactionId}`
      : `row:${entry.row}`;
    const key = `${document.id}:${reference}`;
    if (confirmedStatements.has(key)) return;
    setPending(true);
    setMessage("");
    try {
      await readGraphql(createPurchaseMutation, {
        input: {
          idempotencyKey: makeIdempotencyKey(),
          householdId: household.id,
          sourceDocumentId: document.id,
          sourceReference: reference,
          occurredAt: new Date(entry.occurredAt).toISOString(),
          merchant: entry.merchant,
          paymentAccountId: paymentAccountId || null,
          items: [
            {
              name: entry.merchant || "カード利用",
              amountYen: Math.abs(entry.amount),
              allocations: null,
            },
          ],
        },
      });
      setConfirmedStatements((current) => new Set(current).add(key));
      onAdded(true);
    } catch (reason) {
      setMessage((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">証拠と照合</span>
          <h1>取り込み</h1>
          <p>レシート、注文履歴、明細を追加</p>
        </div>
        <button className="button button-soft" onClick={() => inputRef.current?.click()}>
          <Upload size={16} />
          ファイルを選ぶ
        </button>
      </div>
      <input
        ref={inputRef}
        type="file"
        accept="image/*,application/pdf,.csv,.eml,text/csv,message/rfc822"
        capture="environment"
        multiple
        hidden
        onChange={(event) => addFiles(event.currentTarget.files)}
      />
      <div className="import-layout">
        <section
          className={`dropzone ${dragging ? "dropzone-active" : ""}`}
          onDragOver={(event) => {
            event.preventDefault();
            setDragging(true);
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={(event) => {
            event.preventDefault();
            setDragging(false);
            addFiles(event.dataTransfer.files);
          }}
        >
          <div className="drop-icon">
            <FileImage size={28} />
          </div>
          <h2>購入記録を追加</h2>
          <p>画像、PDF、CSV、メール</p>
          <button className="button button-primary" onClick={() => inputRef.current?.click()}>
            <Upload size={16} />
            ファイルを選ぶ
          </button>
          <span className="drop-caption">スマートフォンではカメラを使えます</span>
        </section>
        <section className="panel import-review">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">確認待ち</span>
              <h2>証拠レビュー</h2>
            </div>
            <span className="count-badge">{documents.length + files.length}</span>
          </div>
          {files.map((file, index) => (
            <div className="file-review-row" key={`${file.name}-${index}`}>
              <span className="file-thumb">
                <FileImage />
              </span>
              <div>
                <b>{file.name}</b>
                <small>{(file.size / 1024).toFixed(0)} KB · 送信待ち</small>
              </div>
              {/\.csv$/i.test(file.name) && (
                <select
                  aria-label={`${file.name}の明細種別`}
                  value={csvKinds[file.name] ?? "card"}
                  onChange={(event) =>
                    setCsvKinds((current) => ({
                      ...current,
                      [file.name]: event.target.value as "card" | "bank",
                    }))
                  }
                >
                  <option value="card">カード明細</option>
                  <option value="bank">銀行明細</option>
                </select>
              )}
              <button
                className="icon-button"
                aria-label="削除"
                onClick={() =>
                  setFiles((current) => current.filter((_, fileIndex) => fileIndex !== index))
                }
              >
                <X size={16} />
              </button>
            </div>
          ))}
          {documents.map((document) => (
            <div className="persisted-document-row" key={document.id}>
              <button
                type="button"
                className={`file-review-row review-select ${selected === document.id ? "review-selected" : ""}`}
                onClick={() => setSelected(document.id)}
              >
                <span className="file-thumb">
                  <FileImage />
                </span>
                <div>
                  <b>
                    {document.name ??
                      `${sourceKindLabel(document.sourceKind)} · ${document.id.slice(0, 8)}`}
                  </b>
                  <small>
                    {statusLabel(document.status)}
                    {document.duplicateOf ? " · 重複候補" : ""}
                  </small>
                </div>
                <ChevronRight size={15} />
              </button>
              {document.status.toUpperCase() === "FAILED" && (
                <button
                  type="button"
                  className="text-button"
                  disabled={pending}
                  onClick={() => void retryDocument(document)}
                >
                  再抽出
                </button>
              )}
            </div>
          ))}
          {files.length > 0 && (
            <button
              className="button button-primary button-wide"
              disabled={pending}
              onClick={() => void uploadFiles()}
            >
              <Upload size={15} />
              {pending ? "認識しています" : "証拠を送信"}
            </button>
          )}
          {selectedDocument && (
            <a
              className="review-image"
              href={`/documents/${encodeURIComponent(selectedDocument.id)}/content`}
              target="_blank"
              rel="noreferrer"
            >
              <FileImage size={23} />
              <span>{selectedDocument.name ?? "原本を開く"}</span>
            </a>
          )}
          {!!selectedDocument?.result?.receipt?.items?.length && (
            <div className="receipt-review">
              <div className="review-fields">
                <div className="review-merchant">
                  <label>
                    店名
                    <input
                      value={selectedDocument.result.receipt.merchant ?? ""}
                      onChange={(event) =>
                        setDocuments((current) =>
                          current.map((document) =>
                            document.id === selectedDocument.id && document.result
                              ? {
                                  ...document,
                                  result: {
                                    ...document.result,
                                    receipt: {
                                      ...document.result.receipt,
                                      merchant: event.target.value,
                                    },
                                  },
                                }
                              : document,
                          ),
                        )
                      }
                    />
                  </label>
                  <label>
                    決済元
                    <select
                      value={paymentAccountId}
                      onChange={(event) => setPaymentAccountId(event.target.value)}
                    >
                      <option value="">未設定</option>
                      {household.accounts.map((account) => (
                        <option value={account.id} key={account.id}>
                          {account.name}
                        </option>
                      ))}
                    </select>
                  </label>
                </div>
                {selectedDocument.result.receipt.items.map((item, index) => (
                  <div className="receipt-item-review" key={index}>
                    <div>
                      <input
                        aria-label="品名"
                        value={item.name}
                        onChange={(event) =>
                          editItem(selectedDocument.id, index, { name: event.target.value })
                        }
                      />
                      <small>
                        {item.verification} · {Math.round(item.confidence * 100)}%
                      </small>
                    </div>
                    <label>
                      ¥
                      <input
                        inputMode="numeric"
                        value={String(item.grossAmount || item.printedAmount)}
                        onChange={(event) =>
                          editItem(selectedDocument.id, index, {
                            grossAmount: Number(event.target.value.replace(/\D/g, "")),
                          })
                        }
                      />
                    </label>
                  </div>
                ))}
                {!!selectedDocument.result.candidates?.length && (
                  <div className="candidate-review">
                    <div className="panel-heading">
                      <div>
                        <span className="eyebrow">金額の解釈</span>
                        <h3>計算候補</h3>
                      </div>
                    </div>
                    {selectedDocument.result.candidates.map((candidate) => (
                      <button
                        type="button"
                        className={`candidate-option ${selectedCandidates[selectedDocument.id] === candidate.id ? "candidate-selected" : ""}`}
                        key={candidate.id}
                        onClick={() =>
                          setSelectedCandidates((current) => ({
                            ...current,
                            [selectedDocument.id]: candidate.id,
                          }))
                        }
                      >
                        <span>
                          <b>{candidate.id || "候補"}</b>
                          <small>
                            {candidate.consistent ? "合計一致" : "合計差異"}
                            {candidate.issues.length ? ` · ${candidate.issues.join("、")}` : ""}
                          </small>
                        </span>
                        <strong>{yen(candidate.computedTotal)}</strong>
                      </button>
                    ))}
                    <small>品目の印字額は保持されます。選んだ計算額を帳簿に使います。</small>
                  </div>
                )}
                <div className="review-total">
                  <span>認識した合計</span>
                  <b>
                    {yen(
                      selectedDocument.result.candidates?.find(
                        (candidate) => candidate.id === selectedCandidates[selectedDocument.id],
                      )?.computedTotal ?? selectedDocument.result.computedTotal,
                    )}
                  </b>
                </div>
                <button
                  className="button button-primary button-wide"
                  disabled={pending}
                  onClick={() => void confirm(selectedDocument)}
                >
                  <Check size={16} />
                  帳簿に追加
                </button>
              </div>
            </div>
          )}
          {selectedDocument?.result?.statements && (
            <div className="statement-review">
              <div className="panel-heading">
                <div>
                  <span className="eyebrow">明細行</span>
                  <h3>{selectedDocument.result.statements.length}件</h3>
                </div>
              </div>
              {selectedDocument.result.statements.map((entry) => (
                <div
                  className="statement-row"
                  key={`${entry.row}-${entry.transactionId ?? entry.occurredAt}`}
                >
                  <div>
                    <b>{entry.merchant || "利用先未入力"}</b>
                    <small>
                      {entry.occurredAt.slice(0, 10)} ·{" "}
                      {entry.kind === "BANK_MOVEMENT" ? "入出金" : "カード利用"}
                      {entry.cardLast4 ? ` · •••• ${entry.cardLast4}` : ""}
                    </small>
                  </div>
                  <div className="statement-row-action">
                    <strong>{yen(entry.amount)}</strong>
                    {entry.kind === "CARD_PURCHASE" && (
                      <button
                        className="text-button"
                        disabled={
                          pending ||
                          confirmedStatements.has(
                            `${selectedDocument.id}:${entry.transactionId ? `transaction:${entry.transactionId}` : `row:${entry.row}`}`,
                          )
                        }
                        onClick={() => void confirmStatement(selectedDocument, entry)}
                      >
                        {confirmedStatements.has(
                          `${selectedDocument.id}:${entry.transactionId ? `transaction:${entry.transactionId}` : `row:${entry.row}`}`,
                        )
                          ? "追加済み"
                          : "購入に追加"}
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
          {message && <InlineError message={message} />}
        </section>
      </div>
      {message && <InlineError message={message} />}
      <div className="import-note">
        <ShieldCheck size={17} />
        <span>原本は帳簿の品目と紐づけて確認できます</span>
      </div>
    </>
  );
}
