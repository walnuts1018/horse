import { useEffect, useRef, useState } from "react";

import {
  importModels,
  isQuotaLimitedExtraction,
  proImportModel,
  type DocumentReview,
} from "../model/imports";

import styles from "./document-conversation.module.css";

type PendingMessage = {
  text: string;
  conversationLength: number;
  status: "processing" | "failed";
};

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
  const [storedPendingMessage, setPendingMessage] = useState<PendingMessage | null>(() =>
    readPendingMessage(document.id),
  );
  const sendingRef = useRef(false);
  const messages = document.result?.conversation;
  const hasExtraction = !!document.result?.receipt || !!document.result?.statements?.length;
  const selectedModel = importModels.find((option) => option.value === document.model);
  const model =
    selectedModel?.label ?? (document.model === "pro" ? "既定の有料モデル" : "既定の無料モデル");
  const paidModel = selectedModel?.tier === "paid" || document.model === "pro";
  const quotaLimited = isQuotaLimitedExtraction(document);
  const processing = ["queued", "running"].includes(document.status.toLowerCase());
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
    if (!instruction || sendingRef.current || processing) return;
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
        }),
      });
      if (!response.ok) throw new Error(`再確認を開始できませんでした (${response.status})`);
      const nextPendingMessage: PendingMessage = {
        text: instruction,
        conversationLength: messages?.length ?? 0,
        status: "processing",
      };
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
      {((messages?.length ?? 0) > 0 || pendingMessage) && (
        <ol className={styles.messages}>
          {messages?.map((entry, index) => (
            <li
              className={entry.role === "user" ? styles.userMessage : styles.assistantMessage}
              key={`${index}-${entry.role}`}
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
          <button type="submit" disabled={sending || !message.trim() || processing}>
            {sending || processing
              ? "再確認中"
              : paidModel
                ? "有料モデルで再確認"
                : "このモデルで再確認"}
          </button>
          {quotaLimited && !paidModel && (
            <button
              type="button"
              disabled={sending || !message.trim() || processing}
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
