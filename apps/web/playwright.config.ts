import { defineConfig } from "@playwright/test";

// End-to-end tests run the real Go API on a development database seeded
// with fictional data (cmd/devseed) and the built Next.js app.
// E2E_DATABASE_URL must point at a throwaway database.
const db = process.env.E2E_DATABASE_URL ?? "";
const apiPort = 18080;
const webPort = 13000;

export default defineConfig({
  testDir: "e2e",
  workers: 1,
  timeout: 30_000,
  use: {
    baseURL: `http://localhost:${webPort}`,
    launchOptions: process.env.PW_CHROMIUM_PATH ? { executablePath: process.env.PW_CHROMIUM_PATH } : {},
  },
  webServer: [
    {
      command: `cd ../.. && APP_ENV=development DATABASE_URL='${db}' go run ./cmd/devseed && DATABASE_URL='${db}' COOKIE_SECURE=false API_ADDR=:${apiPort} REDMINE_PUBLIC_URL=https://redmine.example.test go run ./cmd/api -env-file /dev/null`,
      url: `http://localhost:${apiPort}/healthz`,
      timeout: 120_000,
      reuseExistingServer: false,
    },
    {
      command: `API_INTERNAL_URL=http://localhost:${apiPort} npx next build && npx next start -p ${webPort}`,
      url: `http://localhost:${webPort}/login`,
      timeout: 240_000,
      reuseExistingServer: false,
    },
  ],
});
