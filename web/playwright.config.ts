import { defineConfig, devices } from "@playwright/test";

// Testes ponta a ponta das telas (e2e/). Precisam da API no ar em
// localhost:8080 com APP_ENV=dev (Shopee e cobrança em mock) e do worker
// rodando. O front sobe sozinho com o Vite, que repassa /v1 para a API.
//
// E2E_AUTH_PROVIDER escolhe o provedor de identidade, igual ao AUTH_PROVIDER
// da API: "dev" (padrão) roda o roteiro do MVP; "internal" roda o login por
// e-mail e senha e precisa do Mailpit (MAILPIT_URL, padrão localhost:8025)
// recebendo os e-mails da API.
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
