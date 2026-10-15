import { HeadContent, Scripts, createRootRoute } from "@tanstack/react-router";
import { useEffect, type ReactNode } from "react";
import { RelayEnvironmentProvider } from "react-relay";

import { getRelayEnvironment } from "../../shared/api";
import { QueryProvider } from "../data/query-provider";
import { App } from "../horse-app";

import "../styles/global.css";

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { title: "horse | 家計簿" },
    ],
    links: [
      { rel: "icon", type: "image/svg+xml", href: "/favicon.svg" },
      { rel: "manifest", href: "/manifest.webmanifest" },
      { rel: "apple-touch-icon", href: "/icons/horse-192.png" },
    ],
  }),
  component: Root,
});

function Root() {
  useEffect(() => {
    if (import.meta.env.PROD && "serviceWorker" in navigator) {
      void navigator.serviceWorker.register("/service-worker.js").catch(() => undefined);
    }
  }, []);

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
