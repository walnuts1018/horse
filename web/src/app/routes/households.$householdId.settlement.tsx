import { createFileRoute } from "@tanstack/react-router";
import { SettlementPage } from "../../pages/settlement";
import { useHouseholdRouteContext } from "../household-route-context";

export const Route = createFileRoute("/households/$householdId/settlement")({
  component: HouseholdSettlement,
});

function HouseholdSettlement() {
  const { household, reportUnsavedChanges } = useHouseholdRouteContext();
  if (!household) return null;
  return <SettlementPage household={household} onUnsavedChanges={reportUnsavedChanges} />;
}
