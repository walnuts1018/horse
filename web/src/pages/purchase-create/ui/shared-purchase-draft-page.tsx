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
  const [syncStatus, setSyncStatus] = useState<"saved" | "saving" | "conflict" | "error">("saved");
  const [syncError, setSyncError] = useState("");
  const [submissionQueued, setSubmissionQueued] = useState(false);

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
    [onUnsavedChanges, record.id],
  );

  useEffect(() => {
    if (paused.current || syncStatus === "error" || latestPayload === savedPayloadRef.current)
      return;
    const timer = window.setTimeout(() => void savePayload(latestPayload), 500);
    return () => window.clearTimeout(timer);
  }, [latestPayload, savePayload, syncStatus]);

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
        setSyncStatus("saved");
        setSyncError("");
        onUnsavedChanges(false);
        return;
      }
      if (latestPayloadRef.current === savedPayloadRef.current && !inFlight.current) {
        savedPayloadRef.current = serverPayload;
        latestPayloadRef.current = serverPayload;
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
    [onUnsavedChanges],
  );

  useEffect(() => {
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
  }, [receiveServerDraft, record.id, record.revision]);

  const updateLocalDraft = useCallback(
    (draft: Parameters<typeof encodeSharedPurchaseDraft>[0]) => {
      const payload = encodeSharedPurchaseDraft(draft);
      latestPayloadRef.current = payload;
      setLatestPayload(payload);
      if (payload !== savedPayloadRef.current) onUnsavedChanges(true);
      else if (!paused.current && !inFlight.current) onUnsavedChanges(false);
    },
    [onUnsavedChanges],
  );

  const useServerDraft = useCallback(() => {
    if (!conflictRecord) return;
    const next = decodeSharedPurchaseDraft(conflictRecord.payload);
    const payload = encodeSharedPurchaseDraft(next);
    revisionRef.current = conflictRecord.revision;
    remoteRevisionRef.current = conflictRecord.revision;
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
  }, [conflictRecord, onUnsavedChanges]);

  const keepLocalDraft = useCallback(() => {
    if (!conflictRecord) return;
    const expectedRevision = conflictRecord.revision;
    revisionRef.current = expectedRevision;
    paused.current = false;
    conflictRecordRef.current = null;
    setConflictRecord(null);
    void savePayload(latestPayloadRef.current, expectedRevision);
  }, [conflictRecord, savePayload]);

  const retrySave = useCallback(() => {
    paused.current = false;
    void savePayload(latestPayloadRef.current);
  }, [savePayload]);

  const queuePurchase = useCallback(
    (idempotencyKey: string) => {
      setSubmissionQueued(true);
      onQueued(idempotencyKey);
    },
    [onQueued],
  );

  const sharedSync = useMemo(
    () => ({
      status: syncStatus,
      serverDraft: conflictRecord ? decodeSharedPurchaseDraft(conflictRecord.payload) : undefined,
      error: syncError,
      onUseServer: useServerDraft,
      onKeepMine: keepLocalDraft,
      onRetry: retrySave,
    }),
    [conflictRecord, keepLocalDraft, retrySave, syncError, syncStatus, useServerDraft],
  );

  return (
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
  );
}
