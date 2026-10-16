import { defineConfig } from "@playwright/test";

const databaseUrl = process.env.HORSE_TEST_DATABASE_URL;
if (!databaseUrl) throw new Error("HORSE_TEST_DATABASE_URL is required");

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  retries: 0,
  reporter: "list",
  use: {
    baseURL: "http://127.0.0.1:3000",
    browserName: "chromium",
    launchOptions: { channel: "chromium" },
  },
  webServer: [
    {
      command: "node e2e/mock-oidc.mjs",
      url: "http://127.0.0.1:8090/.well-known/openid-configuration",
      reuseExistingServer: false,
      timeout: 10_000,
    },
    {
      command: "bash e2e/start-api.sh",
      url: "http://127.0.0.1:18080/healthz",
      reuseExistingServer: false,
      timeout: 180_000,
      env: {
        DATABASE_URL: databaseUrl,
        PORT: "18080",
        OIDC_ISSUER_URL: "http://127.0.0.1:8090",
        OIDC_CLIENT_ID: "horse-e2e",
        OIDC_CLIENT_SECRET: "horse-e2e-only",
        OIDC_REDIRECT_URL: "http://127.0.0.1:3000/auth/callback",
        PUBLIC_ORIGIN: "http://127.0.0.1:3000",
        FRONTEND_ORIGIN: "http://127.0.0.1:3000",
        RUST_LOG: "warn",
      },
    },
    {
      command: "./node_modules/.bin/vite --host 127.0.0.1",
      url: "http://127.0.0.1:3000",
      reuseExistingServer: false,
      timeout: 180_000,
      env: { API_ORIGIN: "http://127.0.0.1:18080", HORSE_E2E: "1" },
    },
  ],
});
