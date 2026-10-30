import { defineConfig, devices } from "@playwright/test";
export const e2ePortBase = Number(process.env.GOPANEL_E2E_PORT_BASE ?? 18091);
if (!Number.isInteger(e2ePortBase) || e2ePortBase < 1024 || e2ePortBase > 65530)
  throw new Error(
    "GOPANEL_E2E_PORT_BASE must reserve six ports between 1024 and 65535",
  );
export default defineConfig({
  testDir: "./e2e",
  testIgnore: [
    "account-security.spec.ts",
    "login-logs.spec.ts",
    "installed-contract.spec.ts",
  ],
  fullyParallel: false,
  workers: 1,
  timeout: 60000,
  use: {
    baseURL: `https://127.0.0.1:${e2ePortBase}`,
    ignoreHTTPSErrors: true,
    trace: "retain-on-failure",
  },
  projects: [
    {
      name: "desktop",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 1000 },
      },
    },
    {
      name: "mobile",
      use: {
        ...devices["iPhone 13"],
        defaultBrowserType: "chromium",
        baseURL: `https://127.0.0.1:${e2ePortBase + 1}`,
      },
    },
  ],
});
