import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchQuery, requestSubscription, useLazyLoadQuery } from "react-relay";

import type { Household } from "../../../shared/api";
import type { SharedPurchaseDraftChangesSubscription } from "../../../shared/api/__generated__/SharedPurchaseDraftChangesSubscription.graphql";
import type { SharedPurchaseDraftQuery } from "../../../shared/api/__generated__/SharedPurchaseDraftQuery.graphql";

import { getRelayEnvironment } from "../../../shared/api";
import {
  updateSharedPurchaseDraft,
  type SharedPurchaseDraftRecord,
} from "../api/shared-purchase-draft";
import {
  decodeSharedPurchaseDraft,
  encodeSharedPurchaseDraft,
} from "../model/shared-purchase-draft";
import {
  sharedPurchaseDraftChangesSubscription,
  sharedPurchaseDraftQuery,
} from "../model/shared-purchase-draft-operations";
import {
  clearSharedPurchaseDraftRecovery,
  decideSharedPurchaseDraftRecovery,
  readSharedPurchaseDraftRecovery,
  rebaseSharedPurchaseDraftRecovery,
  writeSharedPurchaseDraftRecovery,
} from "../model/shared-purchase-draft-recovery";
import { PurchaseCreatePage } from "./purchase-create-page";

type SharedPurchaseDraftPageProps = {
  household: Household;
  memberId: string;
  draftId: string;
  onBack: () => void;
  onQueued: (idempotencyKey: string) => void;
  onUnsavedChanges: (unsaved: boolean) => void;
};

export function SharedPurchaseDraftPage(props: SharedPurchaseDraftPageProps) {
  const data = useLazyLoadQuery<SharedPurchaseDraftQuery>(
    sharedPurchaseDraftQuery,
    { id: props.draftId },
    { fetchPolicy: "network-only" },
  );
  const record = data.sharedPurchaseDraft;
  if (!record || record.householdId !== props.household.id)
    return (
      <main className="loading-screen">
        <span>共有下書きを開けません</span>
      </main>
    );
  return <SharedPurchaseDraftEditor key={record.id} {...props} record={record} />;
}

function SharedPurchaseDraftEditor({
  household,
  memberId,
  onBack,
  onQueued,
  onUnsavedChanges,
  record,
}: SharedPurchaseDraftPageProps & { record: SharedPurchaseDraftRecord }) {
  const initial = useMemo(() => decodeSharedPurchaseDraft(record.payload), [record.payload]);
  const initialPayload = useMemo(() => encodeSharedPurchaseDraft(initial), [initial]);
  const [editorDraft, setEditorDraft] = useState(initial);
  const [editorKey, setEditorKey] = useState(0);
  const [latestPayload, setLatestPayload] = useState(initialPayload);
  const latestPayloadRef = useRef(initialPayload);
  const savedPayloadRef = useRef(initialPayload);
  const revisionRef = useRef(record.revision);
  const inFlight = useRef(false);
  const paused = useRef(false);
  const [conflictRecord, setConflictRecord] = useState<SharedPurchaseDraftRecord | null>(null);
  const conflictRecordRef = useRef<SharedPurchaseDraftRecord | null>(null);
  const remoteRevisionRef = useRef(record.revision);
  const recoveryBaseRevisionRef = useRef(record.revision);
  const didInitializeRecovery = useRef(false);
  const [recoveryInitialized, setRecoveryInitialized] = useState(false);
  const [syncStatus, setSyncStatus] = useState<"saved" | "saving" | "conflict" | "error">("saved");
  const [syncError, setSyncError] = useState("");
  const [recoveryError, setRecoveryError] = useState("");
  const [submissionQueued, setSubmissionQueued] = useState(false);

  useEffect(() => {
    if (didInitializeRecovery.current) return;
    didInitializeRecovery.current = true;
    try {
      const recovery = readSharedPurchaseDraftRecovery(record.id, household.id);
      if (!recovery) {
        setRecoveryInitialized(true);
        return;
      }
      const recoveryDraft = decodeSharedPurchaseDraft(recovery.payload);
      const recoveryPayload = encodeSharedPurchaseDraft(recoveryDraft);
      const decision = decideSharedPurchaseDraftRecovery(recovery, record.revision, initialPayload);
      if (decision === "clear") {
        clearSharedPurchaseDraftRecovery(record.id, household.id, recovery.payload);
      } else {
        recoveryBaseRevisionRef.current = recovery.baseRevision;
        latestPayloadRef.current = recoveryPayload;
        setLatestPayload(recoveryPayload);
        setEditorDraft(recoveryDraft);
        onUnsavedChanges(true);
        if (decision === "conflict") {
          paused.current = true;
          conflictRecordRef.current = record;
          setConflictRecord(record);
          setSyncStatus("conflict");
        }
      }
    } catch (reason) {
      setSyncError(reason instanceof Error ? reason.message : "共有下書きの復元に失敗しました");
      setSyncStatus("error");
    } finally {
      setRecoveryInitialized(true);
    }
  }, [household.id, initialPayload, onUnsavedChanges, record]);

  const savePayload = useCallback(
    async (payload: string, expectedRevision = revisionRef.current) => {
      if (inFlight.current) return;
      inFlight.current = true;
      setSyncStatus("saving");
      setSyncError("");
      try {
        const result = await updateSharedPurchaseDraft({
          id: record.id,
          expectedRevision,
          payload,
        });
        if (result.draft.revision < remoteRevisionRef.current) return;
        remoteRevisionRef.current = result.draft.revision;
        revisionRef.current = result.draft.revision;
        if (result.conflict) {
          const serverPayload = encodeSharedPurchaseDraft(
            decodeSharedPurchaseDraft(result.draft.payload),
          );
          if (serverPayload === payload) {
            savedPayloadRef.current = payload;
            if (latestPayloadRef.current === payload) {
              clearSharedPurchaseDraftRecovery(record.id, household.id, payload);
              setRecoveryError("");
            } else {
              rebaseSharedPurchaseDraftRecovery(
                record.id,
                household.id,
                latestPayloadRef.current,
                recoveryBaseRevisionRef.current,
                result.draft.revision,
              );
            }
            recoveryBaseRevisionRef.current = result.draft.revision;
            setSyncStatus("saved");
            if (latestPayloadRef.current === payload) onUnsavedChanges(false);
            return;
          }
          paused.current = true;
          conflictRecordRef.current = result.draft;
          setConflictRecord(result.draft);
          setSyncStatus("conflict");
          onUnsavedChanges(true);
        } else {
          savedPayloadRef.current = payload;
          if (latestPayloadRef.current === payload) {
            clearSharedPurchaseDraftRecovery(record.id, household.id, payload);
            setRecoveryError("");
          } else {
            rebaseSharedPurchaseDraftRecovery(
              record.id,
              household.id,
              latestPayloadRef.current,
              recoveryBaseRevisionRef.current,
              result.draft.revision,
            );
            recoveryBaseRevisionRef.current = result.draft.revision;
          }
          setSyncStatus("saved");
          if (latestPayloadRef.current === payload) onUnsavedChanges(false);
        }
      } catch (reason) {
        setSyncError(reason instanceof Error ? reason.message : "共有下書きを保存できません");
        setSyncStatus("error");
      } finally {
        inFlight.current = false;
      }
    },
    [household.id, onUnsavedChanges, record.id],
  );

  useEffect(() => {
    if (!recoveryInitialized) return;
    if (paused.current || syncStatus === "error" || latestPayload === savedPayloadRef.current)
      return;
    const timer = window.setTimeout(() => void savePayload(latestPayload), 500);
    return () => window.clearTimeout(timer);
  }, [latestPayload, recoveryInitialized, savePayload, syncStatus]);

  const receiveServerDraft = useCallback(
    (serverDraft: SharedPurchaseDraftRecord) => {
      if (serverDraft.revision < remoteRevisionRef.current) return;
      remoteRevisionRef.current = serverDraft.revision;
      revisionRef.current = serverDraft.revision;
      const serverPayload = encodeSharedPurchaseDraft(
        decodeSharedPurchaseDraft(serverDraft.payload),
      );
      if (conflictRecordRef.current) {
        conflictRecordRef.current = serverDraft;
        setConflictRecord(serverDraft);
        return;
      }
      if (latestPayloadRef.current === serverPayload) {
        savedPayloadRef.current = serverPayload;
        clearSharedPurchaseDraftRecovery(record.id, household.id, serverPayload);
        setRecoveryError("");
        recoveryBaseRevisionRef.current = serverDraft.revision;
        setSyncStatus("saved");
        setSyncError("");
        onUnsavedChanges(false);
        return;
      }
      if (latestPayloadRef.current === savedPayloadRef.current && !inFlight.current) {
        savedPayloadRef.current = serverPayload;
        latestPayloadRef.current = serverPayload;
        clearSharedPurchaseDraftRecovery(record.id, household.id, serverPayload);
        setRecoveryError("");
        recoveryBaseRevisionRef.current = serverDraft.revision;
        setLatestPayload(serverPayload);
        setEditorDraft(decodeSharedPurchaseDraft(serverDraft.payload));
        setEditorKey((key) => key + 1);
        setSyncStatus("saved");
        setSyncError("");
        onUnsavedChanges(false);
        return;
      }
      paused.current = true;
      conflictRecordRef.current = serverDraft;
      setConflictRecord(serverDraft);
      setSyncStatus("conflict");
      onUnsavedChanges(true);
    },
    [household.id, onUnsavedChanges, record.id],
  );

  useEffect(() => {
    if (!recoveryInitialized) return;
    let active = true;
    const environment = getRelayEnvironment();
    const subscription = requestSubscription<SharedPurchaseDraftChangesSubscription>(environment, {
      subscription: sharedPurchaseDraftChangesSubscription,
      variables: { id: record.id, after: String(record.revision) },
      onNext: () => {
        fetchQuery<SharedPurchaseDraftQuery>(
          environment,
          sharedPurchaseDraftQuery,
          { id: record.id },
          {
            fetchPolicy: "network-only",
          },
        ).subscribe({
          next: (data) => {
            if (active && data.sharedPurchaseDraft) receiveServerDraft(data.sharedPurchaseDraft);
          },
          error: (reason: unknown) => {
            if (!active) return;
            setSyncError(reason instanceof Error ? reason.message : "共有下書きを再取得できません");
            setSyncStatus((status) => (status === "conflict" ? status : "error"));
          },
        });
      },
      onError: () => {},
    });
    return () => {
      active = false;
      subscription.dispose();
    };
  }, [receiveServerDraft, recoveryInitialized, record.id, record.revision]);

  const updateLocalDraft = useCallback(
    (draft: Parameters<typeof encodeSharedPurchaseDraft>[0]) => {
      const payload = encodeSharedPurchaseDraft(draft);
      latestPayloadRef.current = payload;
      const recovered = writeSharedPurchaseDraftRecovery({
        draftId: record.id,
        householdId: household.id,
        baseRevision: recoveryBaseRevisionRef.current,
        payload,
      });
      setRecoveryError(recovered ? "" : "共有下書きを端末に保存できません");
      setLatestPayload(payload);
      if (payload !== savedPayloadRef.current) onUnsavedChanges(true);
      else {
        clearSharedPurchaseDraftRecovery(record.id, household.id, payload);
        recoveryBaseRevisionRef.current = revisionRef.current;
        if (!paused.current && !inFlight.current) onUnsavedChanges(false);
      }
    },
    [household.id, onUnsavedChanges, record.id],
  );

  const useServerDraft = useCallback(() => {
    if (!conflictRecord) return;
    const next = decodeSharedPurchaseDraft(conflictRecord.payload);
    const payload = encodeSharedPurchaseDraft(next);
    clearSharedPurchaseDraftRecovery(record.id, household.id, latestPayloadRef.current);
    setRecoveryError("");
    revisionRef.current = conflictRecord.revision;
    remoteRevisionRef.current = conflictRecord.revision;
    recoveryBaseRevisionRef.current = conflictRecord.revision;
    conflictRecordRef.current = null;
    savedPayloadRef.current = payload;
    latestPayloadRef.current = payload;
    paused.current = false;
    setConflictRecord(null);
    setEditorDraft(next);
    setLatestPayload(payload);
    setEditorKey((key) => key + 1);
    setSyncStatus("saved");
    onUnsavedChanges(false);
  }, [conflictRecord, household.id, onUnsavedChanges, record.id]);

  const keepLocalDraft = useCallback(() => {
    if (!conflictRecord) return;
    const expectedRevision = conflictRecord.revision;
    const localPayload = latestPayloadRef.current;
    rebaseSharedPurchaseDraftRecovery(
      record.id,
      household.id,
      localPayload,
      recoveryBaseRevisionRef.current,
      expectedRevision,
    );
    recoveryBaseRevisionRef.current = expectedRevision;
    revisionRef.current = expectedRevision;
    paused.current = false;
    conflictRecordRef.current = null;
    setConflictRecord(null);
    void savePayload(latestPayloadRef.current, expectedRevision);
  }, [conflictRecord, household.id, record.id, savePayload]);

  const retrySave = useCallback(() => {
    paused.current = false;
    void savePayload(latestPayloadRef.current);
  }, [savePayload]);

  const queuePurchase = useCallback(
    (idempotencyKey: string) => {
      clearSharedPurchaseDraftRecovery(record.id, household.id, latestPayloadRef.current);
      setRecoveryError("");
      setSubmissionQueued(true);
      onQueued(idempotencyKey);
    },
    [household.id, onQueued, record.id],
  );

  const sharedSync = useMemo(
    () => ({
      status: syncStatus,
      serverDraft: conflictRecord ? decodeSharedPurchaseDraft(conflictRecord.payload) : undefined,
      error: syncError,
      recoveryError,
      onUseServer: useServerDraft,
      onKeepMine: keepLocalDraft,
      onRetry: retrySave,
    }),
    [
      conflictRecord,
      keepLocalDraft,
      recoveryError,
      retrySave,
      syncError,
      syncStatus,
      useServerDraft,
    ],
  );

  return recoveryInitialized ? (
    <PurchaseCreatePage
      household={household}
      memberId={memberId}
      sharedDraftId={record.id}
      onBack={onBack}
      onQueued={queuePurchase}
      onUnsavedChanges={onUnsavedChanges}
      sharedDraft={editorDraft}
      sharedDraftKey={editorKey}
      onDraftChange={updateLocalDraft}
      submissionQueued={submissionQueued}
      sharedSync={sharedSync}
    />
  ) : (
    <main className="loading-screen" />
  );
}
