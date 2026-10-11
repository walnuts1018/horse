import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import { nitro } from "nitro/vite";

const apiOrigin =
  process.env.API_ORIGIN ??
  (process.env.NODE_ENV === "production" ? undefined : "http://localhost:8080");

export default defineConfig({
  plugins: [
    tanstackStart({ router: { routesDirectory: "app/routes" } }),
    nitro(
      apiOrigin
        ? {
            routeRules: {
              "/graphql": { proxy: `${apiOrigin}/graphql` },
              "/events": { proxy: `${apiOrigin}/events` },
              "/events/**": { proxy: `${apiOrigin}/events/**` },
              "/auth/**": { proxy: `${apiOrigin}/auth/**` },
              "/documents": { proxy: `${apiOrigin}/documents` },
              "/documents/**": { proxy: `${apiOrigin}/documents/**` },
              "/purchases/**": { proxy: `${apiOrigin}/purchases/**` },
            },
          }
        : {},
    ),
    react({ babel: { plugins: ["relay"] } }),
  ],
  server: { port: 3000 },
});
