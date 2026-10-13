import { createFileRoute } from "@tanstack/react-router";
import { RelayEnvironmentProvider } from "react-relay";
import { getRelayEnvironment } from "../../shared/api";
import { App } from "../horse-app";

export const Route = createFileRoute("/")({
  component: () => (
    <RelayEnvironmentProvider environment={getRelayEnvironment()}>
      <App />
    </RelayEnvironmentProvider>
  ),
});
