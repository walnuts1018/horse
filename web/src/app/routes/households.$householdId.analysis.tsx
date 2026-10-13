import { createFileRoute } from "@tanstack/react-router";

import { AnalysisPage } from "../../pages/analysis";
import { useHouseholdRouteContext } from "../household-route-context";

export const Route = createFileRoute("/households/$householdId/analysis")({
  component: HouseholdAnalysis,
});

function HouseholdAnalysis() {
  const { household } = useHouseholdRouteContext();
  if (!household) return null;
  return <AnalysisPage household={household} />;
}
