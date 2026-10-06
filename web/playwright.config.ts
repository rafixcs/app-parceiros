import { defineConfig, devices } from "@playwright/test";

// End-to-end tests of the screens (e2e/). They need the API up on
// localhost:8080 with APP_ENV=dev (Shopee and billing mocked) and the worker
// running. The front end starts by itself with Vite, which proxies /v1 to the API.
//
// E2E_AUTH_PROVIDER picks the identity provider, like the API's AUTH_PROVIDER:
// "dev" (default) runs the MVP walkthrough; "internal" runs the email and
// password sign-in and needs Mailpit (MAILPIT_URL, default localhost:8025)
// receiving the API's emails.
const authProvider = process.env.E2E_AUTH_PROVIDER ?? "dev";

export default defineConfig({
  testDir: "./e2e",
  testMatch: "**/*.e2e.ts",
  timeout: 90_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: process.env.E2E_URL ?? "http://localhost:5173",
    locale: "pt-BR",
    timezoneId: "America/Sao_Paulo",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    serviceWorkers: "block",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: process.env.E2E_URL
    ? undefined
    : {
        command: "npm run dev -- --port 5173 --strictPort",
        url: "http://localhost:5173",
        reuseExistingServer: !process.env.CI,
        timeout: 60_000,
        env: { VITE_AUTH_PROVIDER: authProvider },
      },
});
