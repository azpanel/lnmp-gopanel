import { defineConfig } from "@playwright/test";
import base from "./playwright.config";

const baseURL = process.env.GOPANEL_E2E_BASE_URL;
if (!baseURL || !process.env.GOPANEL_E2E_PASSWORD)
  throw new Error(
    "Installed contract tests require a disposable panel URL and password",
  );

export default defineConfig({
  ...base,
  testIgnore: [],
  testMatch: "installed-contract.spec.ts",
  outputDir: "./test-results/installed-contract",
  webServer: undefined,
  expect: { timeout: 20000 },
  projects: base.projects!.map((project) => ({
    ...project,
    use: { ...project.use, baseURL },
  })),
});
