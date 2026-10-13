import { createFileRoute } from "@tanstack/react-router";
import { PurchaseCreatePage } from "../../pages/purchase-create";
import { useHouseholdRouteContext } from "../household-route-context";

export const Route = createFileRoute("/households/$householdId/new")({ component: NewPurchase });

function NewPurchase() {
  const { household, onBackFromPurchase, onPurchaseQueued, reportUnsavedChanges } =
    useHouseholdRouteContext();
  if (!household) return null;
  return (
    <PurchaseCreatePage
      household={household}
      onBack={onBackFromPurchase}
      onUnsavedChanges={reportUnsavedChanges}
      onQueued={onPurchaseQueued}
    />
  );
}
