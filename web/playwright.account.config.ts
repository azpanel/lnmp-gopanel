import { defineConfig } from "@playwright/test";
import base from "./playwright.config";

// Account flows change credentials and exercise the real login rate limit.
// Run them against dedicated instances rather than other specs' accounts.
export default defineConfig({
  ...base,
  testIgnore: [],
  testMatch: "account-security.spec.ts",
  outputDir: "./account-test-results",
  expect: { timeout: 15000 },
  projects: base.projects!.map((project, index) => ({
    ...project,
    use: { ...project.use, baseURL: `https://127.0.0.1:${18093 + index}` },
  })),
  webServer: [18093, 18094].map((port) => ({
    command: `node scripts/e2e-server.mjs ${port}`,
    url: `https://127.0.0.1:${port}`,
    ignoreHTTPSErrors: true,
    reuseExistingServer: false,
    timeout: 30000,
  })),
});
