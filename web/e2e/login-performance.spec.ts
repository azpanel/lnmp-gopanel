import { writeFile } from "node:fs/promises";
import { expect, test } from "./fixtures";

test("login transfer stays within budget without loading business pages", async ({
  page,
  isMobile,
}, testInfo) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  const downloaded: string[] = [];
  page.on("request", (request) => {
    if (request.resourceType() === "script") downloaded.push(request.url());
  });
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "登录管理面板" }),
  ).toBeVisible();
  if (!isMobile)
    await expect(
      page
        .locator('.login-slide[data-active="true"] .recharts-area-area')
        .first(),
    ).toBeVisible();
  await page.waitForLoadState("networkidle");
  const measurements = await page.evaluate(() => {
    const resources = performance.getEntriesByType(
      "resource",
    ) as PerformanceResourceTiming[];
    const bytes = (extension: string) =>
      resources
        .filter((entry) => new URL(entry.name).pathname.endsWith(extension))
        .reduce((total, entry) => total + entry.encodedBodySize, 0);
    return {
      js: bytes(".js"),
      css: bytes(".css"),
      resources: resources.map((entry) => ({
        name: new URL(entry.name).pathname,
        duration: entry.duration,
        encodedBytes: entry.encodedBodySize,
      })),
      navigation: performance
        .getEntriesByType("navigation")
        .map((entry) => entry.toJSON()),
    };
  });
  const report = testInfo.outputPath("login-performance.json");
  await writeFile(report, JSON.stringify(measurements, null, 2));
  await testInfo.attach("login-performance.json", {
    path: report,
    contentType: "application/json",
  });
  expect(measurements.js).toBeGreaterThan(0);
  expect(measurements.js).toBeLessThanOrEqual(250 * 1024);
  expect(measurements.css).toBeGreaterThan(0);
  expect(measurements.css).toBeLessThanOrEqual(40 * 1024);
  expect(
    downloaded.filter((url) =>
      /\/assets\/(workspace|dashboard|services|lnmp-install|system|storage|vhosts|docker|cron|logs|account-security|password-strength|cron-source)-/.test(
        url,
      ),
    ),
  ).toEqual([]);
});
