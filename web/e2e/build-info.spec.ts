import { test, expect, type Page } from "@playwright/test";

async function login(page: Page) {
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(page.locator(".workspace")).toBeVisible();
}

async function openSidebar(page: Page, mobile: boolean) {
  if (mobile) await page.getByRole("button", { name: "打开导航" }).click();
  await expect(page.locator(".sidebar")).toBeVisible();
}

test("navigation shows build info in both themes and keeps the top breadcrumb", async ({
  page,
}, testInfo) => {
  const info = {
    version: "source-" + "a1b2c3d4".repeat(8),
    built_at: "2026-10-29T01:45:00Z",
  };
  await page.route("**/api/v1/build", (route) => route.fulfill({ json: info }));
  await login(page);
  const mobile = testInfo.project.name === "mobile";
  await expect(page.locator(".main-footer .version")).toHaveText(info.version);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
  await expect(page.locator(".breadcrumb > span")).toHaveText("工作空间");
  await openSidebar(page, mobile);
  const sidebar = page.locator(".sidebar");
  await expect(sidebar.getByText("工作空间", { exact: true })).toHaveCount(0);
  await expect(sidebar.getByRole("link", { name: "LNMP 文档" })).toHaveCount(0);
  await expect(sidebar.locator(".build-version")).toHaveText(info.version);
  await expect(sidebar.locator(".build-time")).toHaveText(
    "2026-10-29 09:45:00（UTC+8）",
  );
  expect(
    await sidebar.evaluate((node) => node.scrollWidth <= node.clientWidth),
  ).toBeTruthy();
  await page.screenshot({
    path: testInfo.outputPath("navigation-build-light.png"),
    fullPage: true,
  });
  if (mobile) await page.getByRole("button", { name: "关闭导航" }).click();
  await page.getByRole("button", { name: "切换深色主题" }).click();
  await openSidebar(page, mobile);
  await expect(page.locator("html")).toHaveClass(/dark/);
  await expect(sidebar.locator(".build-version")).toHaveText(info.version);
  await page.screenshot({
    path: testInfo.outputPath("navigation-build-dark.png"),
    fullPage: true,
  });

  info.version = "dev";
  info.built_at = "";
  await page.reload();
  await openSidebar(page, mobile);
  await expect(sidebar.locator(".build-version")).toHaveText("dev");
  await expect(sidebar.locator(".build-time")).toHaveText("未知");
});

test("unavailable build info does not block navigation", async ({
  page,
}, testInfo) => {
  await page.route("**/api/v1/build", (route) =>
    route.fulfill({ status: 503, json: { error: "build info unavailable" } }),
  );
  await login(page);
  await expect(page.locator(".main-footer .version")).toHaveText("暂不可用");
  await openSidebar(page, testInfo.project.name === "mobile");
  await expect(page.locator(".sidebar .build-version")).toHaveText("暂不可用");
  await expect(page.locator(".sidebar .build-time")).toHaveText("暂不可用");
  await page.getByRole("link", { name: "系统信息", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "系统信息", exact: true }),
  ).toBeVisible();
});
