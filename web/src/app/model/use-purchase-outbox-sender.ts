import { useCallback, useEffect, useRef } from "react";

import type { AppPage } from "../app-page";

import { ApiError, type CreatePurchaseInput } from "../../shared/api";
import {
  claimPurchaseOutbox,
  completePurchaseOutbox,
  markPurchaseOutboxFailed,
  renewPurchaseOutboxLease,
  usePurchaseOutbox,
} from "../../shared/lib";

export function shouldAutoNavigateAfterPurchase(input: {
  queuedKey: string | null;
  completedKey: string;
  currentPage: AppPage;
  hasUnsavedChanges: boolean;
}) {
  return (
    input.queuedKey === input.completedKey &&
    input.currentPage === "new" &&
    !input.hasUnsavedChanges
  );
}

export async function refreshHouseholdAfterPurchase(
  refreshHousehold: (id: string) => Promise<void>,
  householdId: string,
): Promise<boolean> {
  try {
    await refreshHousehold(householdId);
    return true;
  } catch {
    return false;
  }
}

export function usePurchaseOutboxSender({
  viewerId,
  households,
  outbox,
  createAndPostPurchase,
  currentPage,
  currentHouseholdId,
  hasUnsavedChanges,
  refreshHousehold,
  navigateToLedger,
  showNotice,
}: {
  viewerId: string | undefined;
  households: Array<{ id: string; name: string }>;
  outbox: ReturnType<typeof usePurchaseOutbox>;
  createAndPostPurchase: (input: CreatePurchaseInput) => Promise<unknown>;
  currentPage: () => AppPage;
  currentHouseholdId: () => string | undefined;
  hasUnsavedChanges: () => boolean;
  refreshHousehold: (id: string) => Promise<void>;
  navigateToLedger: () => void;
  showNotice: (message: string, kind?: "info" | "error" | "success") => void;
}) {
  const activeSends = useRef(new Set<string>());
  const senderId = useRef("");
  const autoNavigateKey = useRef<string | null>(null);

  const onQueued = useCallback(
    (idempotencyKey: string) => {
      autoNavigateKey.current = idempotencyKey;
      showNotice("送信待ちに保存しました", "info");
    },
    [showNotice],
  );
  const clearAutoNavigate = useCallback(() => {
    autoNavigateKey.current = null;
  }, []);

  useEffect(() => {
    if (!viewerId || !outbox.ready || !outbox.online) return;
    if (!senderId.current) senderId.current = crypto.randomUUID();
    const owner = senderId.current;
    const availableHouseholds = new Set(households.map((item) => item.id));
    for (const entry of outbox.entries) {
      const key = entry.input.idempotencyKey;
      if (
        entry.status !== "PENDING" ||
        !availableHouseholds.has(entry.input.householdId) ||
        activeSends.current.has(key)
      )
        continue;
      activeSends.current.add(key);
      void (async () => {
        let claimed = false;
        let leaseRenewal: number | undefined;
        try {
          const request = await claimPurchaseOutbox(viewerId, entry.input.householdId, key, owner);
          if (!request) return;
          claimed = true;
          leaseRenewal = window.setInterval(() => {
            void renewPurchaseOutboxLease(viewerId, entry.input.householdId, key, owner).catch(
              () => {},
            );
          }, 10_000);
          await createAndPostPurchase(request.input);
          await completePurchaseOutbox(viewerId, entry.input.householdId, key, owner);
          if (currentHouseholdId() === request.input.householdId) {
            const refreshed = await refreshHouseholdAfterPurchase(
              refreshHousehold,
              request.input.householdId,
            );
            showNotice(
              refreshed
                ? "買い物を帳簿に追加しました"
                : "購入を追加しました。帳簿を再読み込みしてください",
              refreshed ? "success" : "error",
            );
          }
          if (
            shouldAutoNavigateAfterPurchase({
              queuedKey: autoNavigateKey.current,
              completedKey: key,
              currentPage: currentPage(),
              hasUnsavedChanges: hasUnsavedChanges(),
            })
          ) {
            autoNavigateKey.current = null;
            navigateToLedger();
          }
        } catch (reason) {
          if (claimed) {
            const message =
              reason instanceof ApiError
                ? reason.message
                : reason instanceof TypeError ||
                    (reason instanceof DOMException &&
                      ["AbortError", "NetworkError"].includes(reason.name))
                  ? "接続を確認してから再送してください"
                  : "買い物を送信できませんでした。再送または破棄してください";
            await markPurchaseOutboxFailed(
              viewerId,
              entry.input.householdId,
              key,
              owner,
              message,
            ).catch(() => {});
          }
        } finally {
          if (leaseRenewal !== undefined) window.clearInterval(leaseRenewal);
          activeSends.current.delete(key);
          if (claimed) await outbox.refresh();
        }
      })();
    }
  }, [
    viewerId,
    households,
    outbox,
    createAndPostPurchase,
    currentPage,
    currentHouseholdId,
    hasUnsavedChanges,
    refreshHousehold,
    navigateToLedger,
    showNotice,
  ]);

  return { onQueued, clearAutoNavigate };
}
