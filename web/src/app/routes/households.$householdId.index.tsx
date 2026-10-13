import { createFileRoute } from "@tanstack/react-router";
import { HomePage } from "../../pages/home";
import { useHouseholdRouteContext } from "../household-route-context";

export const Route = createFileRoute("/households/$householdId/")({ component: HouseholdHome });

function HouseholdHome() {
  const { household, navigateToPage } = useHouseholdRouteContext();
  if (!household) return null;
  return <HomePage household={household} onNavigate={navigateToPage} />;
}
