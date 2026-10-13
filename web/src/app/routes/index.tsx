import { createFileRoute } from "@tanstack/react-router";
import { RelayEnvironmentProvider } from "react-relay";
import { getRelayEnvironment } from "../../shared/api";
import { isAppPage, type AppPage } from "../app-page";
import { App } from "../horse-app";

export const Route = createFileRoute("/")({
  validateSearch: (search: Record<string, unknown>): { page?: AppPage } => ({
    page: isAppPage(search.page) ? search.page : undefined,
  }),
  component: () => (
    <RelayEnvironmentProvider environment={getRelayEnvironment()}>
      <App />
    </RelayEnvironmentProvider>
  ),
});
