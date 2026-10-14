import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useCallback } from "react";

import {
  createSharedPurchaseDraft,
  encodeSharedPurchaseDraft,
  PurchaseCreatePage,
} from "../../pages/purchase-create";
import { useHouseholdRouteContext } from "../household-route-context";

export const Route = createFileRoute("/households/$householdId/new")({ component: NewPurchase });

function NewPurchase() {
  const { household, viewerId, onBackFromPurchase, onPurchaseQueued, reportUnsavedChanges } =
    useHouseholdRouteContext();
  const navigate = useNavigate();
  const shareDraft = useCallback(
    async (draft: Parameters<typeof encodeSharedPurchaseDraft>[0]) => {
      if (!household) throw new Error("世帯を確認できません");
      const sharedDraft = await createSharedPurchaseDraft(
        household.id,
        encodeSharedPurchaseDraft(draft),
      );
      reportUnsavedChanges(false);
      await navigate({
        to: "/households/$householdId/drafts/$draftId",
        params: { householdId: household.id, draftId: sharedDraft.id },
      });
    },
    [household, navigate, reportUnsavedChanges],
  );
  if (!household || !viewerId) return null;
  return (
    <PurchaseCreatePage
      household={household}
      memberId={viewerId}
      onBack={onBackFromPurchase}
      onUnsavedChanges={reportUnsavedChanges}
      onQueued={onPurchaseQueued}
      onShareDraft={shareDraft}
    />
  );
}
