import type { ReactNode } from "react";

import { HeadContent, Scripts, createRootRoute } from "@tanstack/react-router";
import { RelayEnvironmentProvider } from "react-relay";

import { getRelayEnvironment } from "../../shared/api";
import { App } from "../horse-app";
import { QueryProvider } from "../providers/query-provider";

import "../styles/global.css";

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: "horse | 家計簿" },
    ],
  }),
  component: Root,
});

function Root() {
  return (
    <RootDocument>
      <QueryProvider>
        <RelayEnvironmentProvider environment={getRelayEnvironment()}>
          <App />
        </RelayEnvironmentProvider>
      </QueryProvider>
    </RootDocument>
  );
}

function RootDocument({ children }: { children: ReactNode }) {
  return (
    <html lang="ja">
      <head>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  );
}
