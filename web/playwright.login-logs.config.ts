import { defineConfig } from "@playwright/test";
import base, { e2ePortBase } from "./playwright.config";

// Failed-login auditing exercises the real limiter; use separate accounts so
// those attempts do not consume other browser tests' shared login budget.
export default defineConfig({
  ...base,
  testIgnore: [],
  testMatch: "login-logs.spec.ts",
  outputDir: "./test-results/login-logs",
  projects: base.projects!.map((project, index) => ({
    ...project,
    use: {
      ...project.use,
      baseURL: `https://127.0.0.1:${e2ePortBase + 4 + index}`,
    },
  })),
});
