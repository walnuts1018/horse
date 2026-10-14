import { createFileRoute } from "@tanstack/react-router";

import { SharedPurchaseDraftPage } from "../../pages/purchase-create";
import { useHouseholdRouteContext } from "../household-route-context";

export const Route = createFileRoute("/households/$householdId/drafts/$draftId")({
  component: SharedPurchaseDraftRoute,
});

function SharedPurchaseDraftRoute() {
  const { household, viewerId, onBackFromPurchase, onPurchaseQueued, reportUnsavedChanges } =
    useHouseholdRouteContext();
  const { draftId } = Route.useParams();
  if (!household || !viewerId) return null;
  return (
    <SharedPurchaseDraftPage
      household={household}
      memberId={viewerId}
      draftId={draftId}
      onBack={onBackFromPurchase}
      onQueued={onPurchaseQueued}
      onUnsavedChanges={reportUnsavedChanges}
    />
  );
}
