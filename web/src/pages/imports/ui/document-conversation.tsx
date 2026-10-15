import { useEffect, useState } from "react";

import {
  importModels,
  isQuotaLimitedExtraction,
  proImportModel,
  type DocumentReview,
} from "../model/imports";

import styles from "./document-conversation.module.css";

export function DocumentConversation({
  document,
  onError,
  onUpdated,
}: {
  document: DocumentReview;
  onError: (message: string) => void;
  onUpdated: () => Promise<unknown> | unknown;
}) {
  const [message, setMessage] = useState("");
  const [sending, setSending] = useState(false);
  const [pendingMessage, setPendingMessage] = useState("");
  const messages = document.result?.conversation ?? [];
  const hasExtraction = !!document.result?.receipt || !!document.result?.statements?.length;
  const selectedModel = importModels.find((option) => option.value === document.model);
  const model =
    selectedModel?.label ?? (document.model === "pro" ? "既定の有料モデル" : "既定の無料モデル");
  const paidModel = selectedModel?.tier === "paid" || document.model === "pro";
  const quotaLimited = isQuotaLimitedExtraction(document);
  const processing = ["queued", "running"].includes(document.status.toLowerCase());

  useEffect(() => {
    if (
      pendingMessage &&
      messages.some((entry) => entry.role === "user" && entry.text === pendingMessage)
    ) {
      setPendingMessage("");
    } else if (pendingMessage && document.status.toLowerCase() === "failed") {
      setPendingMessage("");
    }
  }, [document.status, messages, pendingMessage]);

  const send = async (modelOverride?: string) => {
    if (!message.trim() || sending) return;
    setSending(true);
    try {
      const response = await fetch(`/documents/${encodeURIComponent(document.id)}/interaction`, {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ message, ...(modelOverride ? { model: modelOverride } : {}) }),
      });
      if (!response.ok) throw new Error(`再確認を開始できませんでした (${response.status})`);
      setPendingMessage(message.trim());
      setMessage("");
      await onUpdated();
    } catch (reason) {
      onError(reason instanceof Error ? reason.message : "再確認を開始できませんでした");
    } finally {
      setSending(false);
    }
  };

  if (!hasExtraction) return null;

  return (
    <section className={styles.conversation} aria-label="AIとの再確認">
      {(messages.length > 0 || pendingMessage) && (
        <ol className={styles.messages}>
          {messages.map((entry, index) => (
            <li
              className={entry.role === "user" ? styles.userMessage : styles.assistantMessage}
              key={`${index}-${entry.role}`}
            >
              <span>{entry.role === "user" ? "あなた" : "AI"}</span>
              <p>{entry.text}</p>
            </li>
          ))}
          {pendingMessage && (
            <li className={styles.userMessage}>
              <span>あなた</span>
              <p>{pendingMessage}</p>
            </li>
          )}
        </ol>
      )}
      <form
        className={styles.form}
        onSubmit={(event) => {
          event.preventDefault();
          void send();
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
              onClick={() => void send(proImportModel)}
            >
              Gemini 3.1 Pro Previewで再確認・有料
            </button>
          )}
        </div>
      </form>
    </section>
  );
}
