import { defineConfig, devices } from "@playwright/test";
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  timeout: 30000,
  use: {
    baseURL: "https://127.0.0.1:18091",
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
        baseURL: "https://127.0.0.1:18092",
      },
    },
  ],
  // Each project has its own sessions and login rate limit.
  webServer: [18091, 18092].map((port) => ({
    command: `node scripts/e2e-server.mjs ${port}`,
    url: `https://127.0.0.1:${port}`,
    ignoreHTTPSErrors: true,
    reuseExistingServer: false,
    timeout: 30000,
  })),
});
