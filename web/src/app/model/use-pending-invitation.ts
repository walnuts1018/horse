import { useCallback, useEffect, useState, useSyncExternalStore } from "react";

import type { UnsavedChangesRegistry } from "../household-route-context";

import { acceptHouseholdInvitation } from "../../entities/household";
import { errorMessage } from "../../shared/lib";

const pendingInvitationStorageKey = "horse:pending-household-invitation";
const pendingInvitationChangeEvent = "horse:pending-household-invitation-change";

function subscribeToPendingInvitation(onStoreChange: () => void) {
  window.addEventListener(pendingInvitationChangeEvent, onStoreChange);
  window.addEventListener("storage", onStoreChange);
  return () => {
    window.removeEventListener(pendingInvitationChangeEvent, onStoreChange);
    window.removeEventListener("storage", onStoreChange);
  };
}

function getPendingInvitationSnapshot() {
  return (
    new URLSearchParams(window.location.hash.slice(1)).get("invite") ??
    window.sessionStorage.getItem(pendingInvitationStorageKey) ??
    ""
  );
}

function getServerPendingInvitationSnapshot() {
  return "";
}

function clearPendingInvitation() {
  window.sessionStorage.removeItem(pendingInvitationStorageKey);
  window.dispatchEvent(new Event(pendingInvitationChangeEvent));
}

export function usePendingInvitation({
  unsavedChangesRegistry,
  onAccepted,
  showNotice,
}: {
  unsavedChangesRegistry: UnsavedChangesRegistry;
  onAccepted: (household: { id: string; name: string }) => void;
  showNotice: (message: string, kind?: "info" | "error" | "success") => void;
}) {
  const pendingInvitation = useSyncExternalStore(
    subscribeToPendingInvitation,
    getPendingInvitationSnapshot,
    getServerPendingInvitationSnapshot,
  );
  const [acceptingInvitation, setAcceptingInvitation] = useState(false);

  useEffect(() => {
    const inviteToken = new URLSearchParams(window.location.hash.slice(1)).get("invite");
    if (!inviteToken) return;
    window.sessionStorage.setItem(pendingInvitationStorageKey, inviteToken);
    window.history.replaceState(null, "", `${window.location.pathname}${window.location.search}`);
  }, []);

  const dismissPendingInvitation = useCallback(() => {
    clearPendingInvitation();
  }, []);

  const acceptInvitation = useCallback(async () => {
    if (!pendingInvitation || acceptingInvitation) return;
    const dirtyIds = unsavedChangesRegistry.getDirtyIds();
    const hadUnsavedChanges = dirtyIds.length > 0;
    if (hadUnsavedChanges && !window.confirm("未保存の変更を破棄して招待を受け入れますか？"))
      return;
    if (hadUnsavedChanges) unsavedChangesRegistry.clearAll();
    setAcceptingInvitation(true);
    try {
      const household = await acceptHouseholdInvitation({ token: pendingInvitation });
      clearPendingInvitation();
      onAccepted(household);
    } catch (reason) {
      for (const id of dirtyIds) unsavedChangesRegistry.report(id, true);
      showNotice(errorMessage(reason), "error");
    } finally {
      setAcceptingInvitation(false);
    }
  }, [pendingInvitation, acceptingInvitation, unsavedChangesRegistry, onAccepted, showNotice]);

  return {
    pendingInvitation,
    acceptingInvitation,
    acceptInvitation,
    dismissPendingInvitation,
  };
}
