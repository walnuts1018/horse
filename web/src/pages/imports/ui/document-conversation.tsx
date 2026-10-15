import { AlertCircle } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import {
  readDocumentExtraction,
  readDocumentExtractionVersions,
} from "../api/document-extractions";
import {
  compareExtractions,
  type ExtractionRowChange,
  readReceiptExtractionSnapshot,
} from "../model/extraction-diff";
import {
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
  const [historyOpened, setHistoryOpened] = useState(false);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [versions, setVersions] = useState<DocumentExtractionVersion[]>([]);
  const [selectedVersionId, setSelectedVersionId] = useState("");
  const [selectedExtraction, setSelectedExtraction] = useState<DocumentExtraction | null>(null);
  const [acceptedInteraction, setAcceptedInteraction] = useState<AcceptedInteraction | null>(null);
  const onUpdatedRef = useRef(onUpdated);
  const messages = selectedExtraction?.result.conversation ?? document.result?.conversation;
  const keyedMessages = withConversationKeys(messages);
  const activeResult = selectedExtraction?.result ?? document.result;
  const hasExtraction = !!activeResult?.receipt || !!activeResult?.statements?.length;
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

  if (!hasExtraction) return null;

  return (
    <section className={styles.conversation} aria-label="抽出履歴">
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
              抽出版
              <select
                value={selectedVersionId}
                onChange={(event) => setSelectedVersionId(event.currentTarget.value)}
                disabled={historyLoading || processing}
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
              disabled={processing}
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
            抽出履歴へ戻る
          </button>
        )
      ) : (
        <button
          type="button"
          className={styles.historyButton}
          onClick={() => setHistoryOpened(true)}
          disabled={processing}
        >
          過去の抽出版を見る
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
      <output className={styles.processingNotice} aria-live="polite">
        <AlertCircle size={16} aria-hidden="true" /> Gemini
        APIの利用条件確認中。AI解析は一時停止中です。
      </output>
    </section>
  );
}
