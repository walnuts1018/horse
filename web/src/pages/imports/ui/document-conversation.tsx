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
  deferredExtractionMessage,
  importModelCostLabel,
  importModels,
  isImportModel,
  isQuotaLimitedExtraction,
  proImportModel,
  retryImportModel,
  type DocumentExtraction,
  type DocumentExtractionVersion,
  type DocumentReview,
} from "../model/imports";

import styles from "./document-conversation.module.css";

type PendingInteraction = NonNullable<DocumentReview["pendingInteraction"]>;
type InteractionStatus = PendingInteraction["status"] | "succeeded";
type AcceptedInteraction = Omit<PendingInteraction, "status"> & { status: InteractionStatus };

function isInteractionStatus(value: unknown): value is InteractionStatus {
  return value === "queued" || value === "running" || value === "succeeded" || value === "failed";
}

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
  const [modelChoice, setModelChoice] = useState(() => ({
    errorCode: document.errorCode,
    model: retryImportModel(document),
  }));
  const model =
    modelChoice.errorCode === document.errorCode ? modelChoice.model : retryImportModel(document);
  const [sending, setSending] = useState(false);
  const [historyOpened, setHistoryOpened] = useState(false);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [versions, setVersions] = useState<DocumentExtractionVersion[]>([]);
  const [selectedVersionId, setSelectedVersionId] = useState("");
  const [selectedExtraction, setSelectedExtraction] = useState<DocumentExtraction | null>(null);
  const [branchJobId, setBranchJobId] = useState("");
  const [acceptedInteraction, setAcceptedInteraction] = useState<AcceptedInteraction | null>(null);
  const sendingRef = useRef(false);
  const onUpdatedRef = useRef(onUpdated);
  const messages = selectedExtraction?.result.conversation ?? document.result?.conversation;
  const keyedMessages = withConversationKeys(messages);
  const activeResult = selectedExtraction?.result ?? document.result;
  const hasExtraction = !!activeResult?.receipt || !!activeResult?.statements?.length;
  const modelLabel = importModels.find((option) => option.value === model)?.label ?? model;
  const quotaLimited = isQuotaLimitedExtraction(document);
  const pendingInteraction = acceptedInteraction ?? document.pendingInteraction;
  const pendingInteractionJobID = pendingInteraction?.jobId;
  const pendingInteractionStatus = pendingInteraction?.status;
  const pendingInteractionRef = useRef(pendingInteraction);
  const interactionProcessing =
    pendingInteraction?.status === "queued" || pendingInteraction?.status === "running";
  const processing =
    ["queued", "running"].includes(document.status.toLowerCase()) || interactionProcessing;
  const previousExtraction = readReceiptExtractionSnapshot(document.id);
  const extractionDiff =
    previousExtraction &&
    document.jobId &&
    document.jobId !== previousExtraction.jobId &&
    document.jobId === previousExtraction.nextJobId &&
    document.status.toLowerCase() === "succeeded"
      ? compareExtractions(previousExtraction, document.result ?? {})
      : null;
  useEffect(() => {
    onUpdatedRef.current = onUpdated;
  }, [onUpdated]);

  useEffect(() => {
    pendingInteractionRef.current = pendingInteraction;
  }, [pendingInteraction]);

  useEffect(() => {
    if (!pendingInteractionJobID || !["queued", "running"].includes(pendingInteractionStatus ?? ""))
      return;
    const source = new EventSource(
      `/documents/${encodeURIComponent(document.id)}/interactions/${encodeURIComponent(pendingInteractionJobID)}/events`,
      { withCredentials: true },
    );
    source.onmessage = (messageEvent) => {
      try {
        const update: unknown = JSON.parse(messageEvent.data);
        if (
          typeof update !== "object" ||
          update === null ||
          !("status" in update) ||
          !("reply" in update) ||
          !isInteractionStatus(update.status) ||
          typeof update.reply !== "string"
        ) {
          return;
        }
        const status = update.status;
        const reply = update.reply;
        if (!isInteractionStatus(status) || typeof reply !== "string") return;
        setAcceptedInteraction((current) => {
          const fallback = pendingInteractionRef.current;
          if (!fallback || fallback.jobId !== pendingInteractionJobID) return current;
          const accepted = current?.jobId === pendingInteractionJobID ? current : fallback;
          return { ...accepted, status, replyPreview: reply };
        });
        if (!["queued", "running"].includes(status)) {
          source.close();
          void onUpdatedRef.current();
        }
      } catch {
        source.close();
      }
    };
    return () => source.close();
  }, [document.id, pendingInteractionJobID, pendingInteractionStatus]);
  const hasExtractionDiff = Boolean(
    extractionDiff &&
    (extractionDiff.receiptChanges.length > 0 ||
      extractionDiff.itemChanges.length > 0 ||
      extractionDiff.taxChanges.length > 0 ||
      extractionDiff.paymentChanges.length > 0 ||
      extractionDiff.statementChanges.length > 0),
  );
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
  }, [document.id, document.jobId, document.status, historyOpened, onError]);

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
    if (
      acceptedInteraction &&
      document.pendingInteraction?.jobId === acceptedInteraction.jobId &&
      document.pendingInteraction.status === acceptedInteraction.status &&
      document.pendingInteraction.replyPreview === acceptedInteraction.replyPreview
    ) {
      setAcceptedInteraction(null);
      return;
    }
    if (acceptedInteraction && document.jobId === acceptedInteraction.jobId) {
      setAcceptedInteraction(null);
    }
  }, [acceptedInteraction, document.jobId, document.pendingInteraction]);

  const send = async (
    text: string,
    modelOverride?: (typeof importModels)[number]["value"],
    baseJobIdOverride?: string,
  ) => {
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
    const baseJobId = baseJobIdOverride ?? selectedExtraction?.id;
    if (baseJobIdOverride && !historyOpened) setHistoryOpened(true);
    try {
      const response = await fetch(`/documents/${encodeURIComponent(document.id)}/interaction`, {
        method: "POST",
        credentials: "include",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          message: instruction,
          model: modelOverride ?? model,
          ...(baseJobId ? { baseJobId } : {}),
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
      if (baseJobId && nextJobId) setBranchJobId(nextJobId);
      const interactionModel = modelOverride ?? model;
      if (nextJobId) {
        setAcceptedInteraction({
          jobId: nextJobId,
          status: "queued",
          message: instruction,
          replyPreview: "",
          ...(interactionModel ? { model: interactionModel } : {}),
          ...(baseJobId ? { baseJobId } : {}),
        });
      }
      const snapshotResult =
        baseJobIdOverride && selectedExtraction?.id !== baseJobIdOverride
          ? undefined
          : (selectedExtraction?.result ?? activeResult);
      if (
        document.jobId &&
        (snapshotResult?.receipt || snapshotResult?.statements?.length) &&
        nextJobId
      ) {
        storeReceiptExtractionSnapshot(document.id, {
          jobId: baseJobId ?? document.jobId,
          nextJobId,
          receipt: snapshotResult?.receipt,
          statements: snapshotResult?.statements,
        });
      }
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
      {deferredExtractionMessage(document.errorCode) && (
        <output className={styles.processingNotice} aria-live="polite">
          {deferredExtractionMessage(document.errorCode)}
        </output>
      )}
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
      {((messages?.length ?? 0) > 0 || pendingInteraction) && (
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
          {pendingInteraction && (
            <li
              className={
                pendingInteraction.status === "failed" ? styles.failedMessage : styles.userMessage
              }
            >
              <span role={pendingInteraction.status === "failed" ? "status" : undefined}>
                {pendingInteraction.status === "failed" ? "再確認に失敗しました" : "あなた"}
              </span>
              <p>{pendingInteraction.message}</p>
              {pendingInteraction.status === "failed" && (
                <div className={styles.retryActions}>
                  <button
                    type="button"
                    disabled={sending || processing}
                    onClick={() =>
                      void send(pendingInteraction.message, model, pendingInteraction.baseJobId)
                    }
                  >
                    {modelLabel}で同じ指示を再送
                  </button>
                  {quotaLimited && model !== proImportModel && (
                    <button
                      type="button"
                      disabled={sending || processing}
                      onClick={() =>
                        void send(
                          pendingInteraction.message,
                          proImportModel,
                          pendingInteraction.baseJobId,
                        )
                      }
                    >
                      Gemini 3.1 Pro Previewで同じ指示を再送・有料
                    </button>
                  )}
                </div>
              )}
            </li>
          )}
          {interactionProcessing && (
            <li className={styles.assistantMessage}>
              <span>{pendingInteraction.replyPreview ? "AI" : "確認中"}</span>
              <p>{pendingInteraction.replyPreview || "原本を確認しています"}</p>
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
          <label className={styles.versionPicker}>
            AIモデル
            <select
              aria-label="再確認AIモデル"
              value={model}
              disabled={sending || processing}
              onChange={(event) => {
                if (isImportModel(event.currentTarget.value)) {
                  setModelChoice({
                    errorCode: document.errorCode,
                    model: event.currentTarget.value,
                  });
                }
              }}
            >
              <optgroup label="無料">
                {importModels
                  .filter((option) => option.tier === "free")
                  .map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
              </optgroup>
              <optgroup label="有料">
                {importModels
                  .filter((option) => option.tier === "paid")
                  .map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
              </optgroup>
            </select>
            <small>{importModelCostLabel(model)}</small>
          </label>
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
            {sending || processing ? "再確認中" : `${modelLabel}で再確認`}
          </button>
          {quotaLimited && model !== proImportModel && (
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
