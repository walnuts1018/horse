import { createFileRoute } from "@tanstack/react-router";

import { LedgerPage, parseLedgerSearch, serializeLedgerSearch } from "../../pages/ledger";
import { useHouseholdRouteContext } from "../household-route-context";

export const Route = createFileRoute("/households/$householdId/ledger")({
  validateSearch: (search: Record<string, unknown>) =>
    serializeLedgerSearch(parseLedgerSearch(search)),
  component: HouseholdLedger,
});

function HouseholdLedger() {
  const { household, navigateToPage, reportUnsavedChanges } = useHouseholdRouteContext();
  const filters = parseLedgerSearch(Route.useSearch());
  const navigate = Route.useNavigate();
  if (!household) return null;
  return (
    <LedgerPage
      household={household}
      filters={filters}
      onFiltersChange={(updates) =>
        void navigate({
          replace: true,
          search: (previous) =>
            serializeLedgerSearch({ ...parseLedgerSearch(previous), ...updates }),
        })
      }
      onAdd={() => navigateToPage("new")}
      onUnsavedChanges={reportUnsavedChanges}
    />
  );
}
