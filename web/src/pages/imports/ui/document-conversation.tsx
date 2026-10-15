import { useEffect, useRef, useState } from "react";

import {
  readDocumentExtraction,
  readDocumentExtractionVersions,
} from "../api/document-extractions";
import {
  compareExtractions,
  type ExtractionRowChange,
  readReceiptExtractionSnapshot,
  storeReceiptExtractionSnapshot,
} from "../model/extraction-diff";
import {
  importModels,
  isQuotaLimitedExtraction,
  proImportModel,
  type DocumentExtraction,
  type DocumentExtractionVersion,
  type DocumentReview,
} from "../model/imports";

import styles from "./document-conversation.module.css";

type PendingMessage = {
  text: string;
  conversationLength: number;
  status: "processing" | "failed";
};

type ConversationMessage = NonNullable<
  NonNullable<DocumentReview["result"]>["conversation"]
>[number];

function withConversationKeys(messages: ConversationMessage[] = []) {
  const occurrences = new Map<string, number>();
  return messages.map((entry) => {
    const identity = JSON.stringify([entry.role, entry.text]);
    const occurrence = occurrences.get(identity) ?? 0;
    occurrences.set(identity, occurrence + 1);
    return { ...entry, key: `${identity}-${occurrence}` };
  });
}

function renderExtractionRows(rows: ExtractionRowChange[]) {
  const statusLabels = {
    changed: "変更",
    added: "追加",
    removed: "削除",
    "unmatched-current": "今回・照合なし",
    "unmatched-previous": "前回・照合なし",
  };
  return rows.map((row) => (
    <li className={styles.diffRow} data-status={row.status} key={row.id}>
      <span className={styles.diffStatus}>{statusLabels[row.status]}</span>
      <span className={styles.diffRowLabel}>{row.label}</span>
      <ul className={styles.diffFields}>
        {row.changes.map((change) => (
          <li key={`${row.id}-${change.label}`}>
            <span>{change.label}</span>
            <del>{change.before}</del>
            <span aria-hidden="true">→</span>
            <ins>{change.after}</ins>
          </li>
        ))}
      </ul>
    </li>
  ));
}

function pendingMessageStorageKey(documentId: string) {
  return `document-interaction:${documentId}`;
}

function readPendingMessage(documentId: string): PendingMessage | null {
  try {
    const stored = sessionStorage.getItem(pendingMessageStorageKey(documentId));
    if (!stored) return null;
    const value: unknown = JSON.parse(stored);
    if (
      typeof value !== "object" ||
      value === null ||
      !("text" in value) ||
      typeof value.text !== "string" ||
      !("conversationLength" in value) ||
      typeof value.conversationLength !== "number" ||
      !("status" in value) ||
      (value.status !== "processing" && value.status !== "failed")
    ) {
      return null;
    }
    return {
      text: value.text,
      conversationLength: value.conversationLength,
      status: value.status,
    };
  } catch {
    return null;
  }
}

function storePendingMessage(documentId: string, pendingMessage: PendingMessage) {
  try {
    sessionStorage.setItem(pendingMessageStorageKey(documentId), JSON.stringify(pendingMessage));
    return true;
  } catch {
    return false;
  }
}

function clearPendingMessage(documentId: string) {
  try {
    sessionStorage.removeItem(pendingMessageStorageKey(documentId));
    return true;
  } catch {
    return false;
  }
}

export function DocumentConversation({
  document,
  onError,
  onUpdated,
}: {
  document: DocumentReview;
  onError: (message: string) => void;
  onUpdated: () => Promise<void> | void;
}) {
  const [message, setMessage] = useState("");
  const [sending, setSending] = useState(false);
  const [historyOpened, setHistoryOpened] = useState(false);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [versions, setVersions] = useState<DocumentExtractionVersion[]>([]);
  const [selectedVersionId, setSelectedVersionId] = useState("");
  const [selectedExtraction, setSelectedExtraction] = useState<DocumentExtraction | null>(null);
  const [branchJobId, setBranchJobId] = useState("");
  const [storedPendingMessage, setPendingMessage] = useState<PendingMessage | null>(() =>
    readPendingMessage(document.id),
  );
  const sendingRef = useRef(false);
  const messages = selectedExtraction?.result.conversation ?? document.result?.conversation;
  const keyedMessages = withConversationKeys(messages);
  const activeResult = selectedExtraction?.result ?? document.result;
  const hasExtraction = !!activeResult?.receipt || !!activeResult?.statements?.length;
  const activeModel = selectedExtraction?.model ?? document.model;
  const selectedModel = importModels.find((option) => option.value === activeModel);
  const model =
    selectedModel?.label ?? (activeModel === "pro" ? "既定の有料モデル" : "既定の無料モデル");
  const paidModel = selectedModel?.tier === "paid" || activeModel === "pro";
  const quotaLimited = isQuotaLimitedExtraction(document);
  const processing = ["queued", "running"].includes(document.status.toLowerCase());
  const previousExtraction = readReceiptExtractionSnapshot(document.id);
  const extractionDiff =
    previousExtraction &&
    document.jobId &&
    document.jobId !== previousExtraction.jobId &&
    document.jobId === previousExtraction.nextJobId &&
    document.status.toLowerCase() === "succeeded"
      ? compareExtractions(previousExtraction, document.result ?? {})
      : null;
  const hasExtractionDiff = Boolean(
    extractionDiff &&
    (extractionDiff.receiptChanges.length > 0 ||
      extractionDiff.itemChanges.length > 0 ||
      extractionDiff.taxChanges.length > 0 ||
      extractionDiff.paymentChanges.length > 0 ||
      extractionDiff.statementChanges.length > 0),
  );
  const pendingMessageCompleted =
    !!storedPendingMessage &&
    !!messages
      ?.slice(storedPendingMessage.conversationLength)
      .some((entry) => entry.role === "user" && entry.text === storedPendingMessage.text);
  const pendingMessage =
    storedPendingMessage &&
    !pendingMessageCompleted &&
    storedPendingMessage.status === "processing" &&
    document.status.toLowerCase() === "failed"
      ? { ...storedPendingMessage, status: "failed" as const }
      : pendingMessageCompleted
        ? null
        : storedPendingMessage;

  useEffect(() => {
    if (!historyOpened) return;
    const controller = new AbortController();
    setHistoryLoading(true);
    readDocumentExtractionVersions(document.id, controller.signal)
      .then((loaded) => {
        setVersions(loaded);
        setSelectedVersionId((current) => {
          if (current && loaded.some((entry) => entry.id === current)) return current;
          if (
            document.status.toLowerCase() === "succeeded" &&
            loaded.some((entry) => entry.id === document.jobId)
          ) {
            return document.jobId ?? "";
          }
          return loaded[0]?.id ?? "";
        });
      })
      .catch((reason) => {
        if (!controller.signal.aborted) {
          onError(reason instanceof Error ? reason.message : "抽出履歴を取得できませんでした");
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setHistoryLoading(false);
      });
    return () => controller.abort();
  }, [document.id, historyOpened, onError]);

  useEffect(() => {
    if (!historyOpened || !selectedVersionId) {
      setSelectedExtraction(null);
      return;
    }
    const controller = new AbortController();
    readDocumentExtraction(document.id, selectedVersionId, controller.signal)
      .then(setSelectedExtraction)
      .catch((reason) => {
        if (!controller.signal.aborted) {
          onError(reason instanceof Error ? reason.message : "抽出版を取得できませんでした");
        }
      });
    return () => controller.abort();
  }, [document.id, historyOpened, onError, selectedVersionId]);

  useEffect(() => {
    if (
      !historyOpened ||
      !branchJobId ||
      document.status.toLowerCase() !== "succeeded" ||
      document.jobId !== branchJobId
    ) {
      return;
    }
    setSelectedVersionId(branchJobId);
    const controller = new AbortController();
    Promise.all([
      readDocumentExtractionVersions(document.id, controller.signal),
      readDocumentExtraction(document.id, branchJobId, controller.signal),
    ])
      .then(([loadedVersions, extraction]) => {
        setVersions(loadedVersions);
        setSelectedExtraction(extraction);
        setBranchJobId("");
      })
      .catch((reason) => {
        if (!controller.signal.aborted) {
          onError(reason instanceof Error ? reason.message : "新しい抽出版を取得できませんでした");
        }
      });
    return () => controller.abort();
  }, [branchJobId, document.id, document.jobId, document.status, historyOpened, onError]);

  useEffect(() => {
    if (!storedPendingMessage) return;
    if (pendingMessageCompleted) {
      clearPendingMessage(document.id);
    } else if (
      storedPendingMessage.status === "processing" &&
      document.status.toLowerCase() === "failed"
    ) {
      storePendingMessage(document.id, { ...storedPendingMessage, status: "failed" });
    } else {
      storePendingMessage(document.id, storedPendingMessage);
    }
  }, [document.id, document.status, pendingMessageCompleted, storedPendingMessage]);

  const send = async (text: string, modelOverride?: string) => {
    const instruction = text.trim();
    if (
      !instruction ||
      sendingRef.current ||
      processing ||
      (historyOpened && !!selectedVersionId && !selectedExtraction)
    ) {
      return;
    }
    sendingRef.current = true;
    setSending(true);
    try {
      const response = await fetch(`/documents/${encodeURIComponent(document.id)}/interaction`, {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          message: instruction,
          ...(modelOverride ? { model: modelOverride } : {}),
          ...(selectedExtraction ? { baseJobId: selectedExtraction.id } : {}),
        }),
      });
      if (!response.ok) throw new Error(`再確認を開始できませんでした (${response.status})`);
      const accepted: unknown = await response.json().catch(() => null);
      const nextJobId =
        typeof accepted === "object" &&
        accepted !== null &&
        "jobId" in accepted &&
        typeof accepted.jobId === "string"
          ? accepted.jobId
          : undefined;
      if (selectedExtraction && nextJobId) setBranchJobId(nextJobId);
      const nextPendingMessage: PendingMessage = {
        text: instruction,
        conversationLength: messages?.length ?? 0,
        status: "processing",
      };
      if (
        document.jobId &&
        (activeResult?.receipt || activeResult?.statements?.length) &&
        nextJobId
      ) {
        storeReceiptExtractionSnapshot(document.id, {
          jobId: selectedExtraction?.id ?? document.jobId,
          nextJobId,
          receipt: activeResult?.receipt,
          statements: activeResult?.statements,
        });
      }
      storePendingMessage(document.id, nextPendingMessage);
      setPendingMessage(nextPendingMessage);
      if (instruction === message.trim()) setMessage("");
      await onUpdated();
    } catch (reason) {
      onError(reason instanceof Error ? reason.message : "再確認を開始できませんでした");
    } finally {
      sendingRef.current = false;
      setSending(false);
    }
  };

  if (!hasExtraction) return null;

  return (
    <section className={styles.conversation} aria-label="AIとの再確認">
      {hasExtractionDiff && extractionDiff && (
        <details className={styles.extractionDiff} open>
          <summary>前回からの変更</summary>
          <ul>
            {extractionDiff.receiptChanges.map((change) => (
              <li key={`receipt-${change.label}`}>
                <span>{change.label}</span>
                <del>{change.before}</del>
                <span aria-hidden="true">→</span>
                <ins>{change.after}</ins>
              </li>
            ))}
            {renderExtractionRows(extractionDiff.itemChanges)}
            {renderExtractionRows(extractionDiff.taxChanges)}
            {renderExtractionRows(extractionDiff.paymentChanges)}
            {renderExtractionRows(extractionDiff.statementChanges)}
          </ul>
        </details>
      )}
      {historyOpened ? (
        versions.length > 0 ? (
          <>
            <label className={styles.versionPicker}>
              再確認の起点
              <select
                value={selectedVersionId}
                onChange={(event) => setSelectedVersionId(event.currentTarget.value)}
                disabled={historyLoading || sending || processing}
              >
                {versions.map((version) => (
                  <option key={version.id} value={version.id}>
                    第{version.version}版・{new Date(version.createdAt).toLocaleString("ja-JP")}・
                    {version.model}
                  </option>
                ))}
              </select>
            </label>
            <button
              type="button"
              className={styles.historyButton}
              onClick={() => {
                setHistoryOpened(false);
                setSelectedExtraction(null);
                setSelectedVersionId("");
              }}
              disabled={sending || processing}
            >
              現在の抽出版へ戻る
            </button>
          </>
        ) : (
          <button
            type="button"
            className={styles.historyButton}
            onClick={() => setHistoryOpened(false)}
            disabled={historyLoading}
          >
            会話へ戻る
          </button>
        )
      ) : (
        <button
          type="button"
          className={styles.historyButton}
          onClick={() => setHistoryOpened(true)}
          disabled={sending || processing}
        >
          過去の抽出版から分岐
        </button>
      )}
      {((messages?.length ?? 0) > 0 || pendingMessage) && (
        <ol className={styles.messages}>
          {keyedMessages.map((entry) => (
            <li
              className={entry.role === "user" ? styles.userMessage : styles.assistantMessage}
              key={entry.key}
            >
              <span>{entry.role === "user" ? "あなた" : "AI"}</span>
              <p>{entry.text}</p>
            </li>
          ))}
          {pendingMessage && (
            <li
              className={
                pendingMessage.status === "failed" ? styles.failedMessage : styles.userMessage
              }
            >
              <span role={pendingMessage.status === "failed" ? "status" : undefined}>
                {pendingMessage.status === "failed" ? "再確認に失敗しました" : "あなた"}
              </span>
              <p>{pendingMessage.text}</p>
              {pendingMessage.status === "failed" && (
                <div className={styles.retryActions}>
                  <button
                    type="button"
                    disabled={sending || processing}
                    onClick={() => void send(pendingMessage.text)}
                  >
                    {paidModel ? "有料モデルで同じ指示を再送" : "同じ指示を再送"}
                  </button>
                  {quotaLimited && !paidModel && (
                    <button
                      type="button"
                      disabled={sending || processing}
                      onClick={() => void send(pendingMessage.text, proImportModel)}
                    >
                      Gemini 3.1 Pro Previewで同じ指示を再送・有料
                    </button>
                  )}
                </div>
              )}
            </li>
          )}
        </ol>
      )}
      <form
        className={styles.form}
        onSubmit={(event) => {
          event.preventDefault();
          void send(message);
        }}
      >
        <label htmlFor={`document-interaction-${document.id}`}>AIに追加指示</label>
        <textarea
          id={`document-interaction-${document.id}`}
          value={message}
          onChange={(event) => setMessage(event.currentTarget.value)}
          maxLength={8000}
          rows={3}
          disabled={sending || processing}
        />
        <div className={styles.actions}>
          <div className={styles.modelDisclosure}>
            <span>
              {model}・{paidModel ? "有料" : "無料"}
            </span>
            <small>
              {paidModel
                ? "入力はGoogleの製品改善に使われません。"
                : "入力がGoogleの製品改善に使われる場合があります。"}
            </small>
          </div>
          <button
            type="submit"
            disabled={
              sending ||
              !message.trim() ||
              processing ||
              historyLoading ||
              (historyOpened && !!selectedVersionId && !selectedExtraction)
            }
          >
            {sending || processing
              ? "再確認中"
              : paidModel
                ? "有料モデルで再確認"
                : "このモデルで再確認"}
          </button>
          {quotaLimited && !paidModel && (
            <button
              type="button"
              disabled={
                sending ||
                !message.trim() ||
                processing ||
                historyLoading ||
                (historyOpened && !!selectedVersionId && !selectedExtraction)
              }
              onClick={() => void send(message, proImportModel)}
            >
              Gemini 3.1 Pro Previewで再確認・有料
            </button>
          )}
        </div>
      </form>
    </section>
  );
}
