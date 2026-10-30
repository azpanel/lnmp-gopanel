import { test, expect, type Page } from "./fixtures";

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

async function openBuildInfo(page: Page, mobile: boolean) {
  const avatar = page.getByRole("button", { name: "查看版本和构建时间" });
  await avatar.scrollIntoViewIfNeeded();
  if (mobile) await avatar.tap();
  else await avatar.hover();
  const tooltip = page.getByRole("tooltip");
  await expect(tooltip).toBeVisible();
  return tooltip;
}

test("avatar tooltip shows build info with icons in both themes", async ({
  page,
}, testInfo) => {
  const info = {
    version: "source-" + "a1b2c3d4".repeat(8),
    built_at: "2026-10-29T01:45:00Z",
  };
  await page.route("**/api/v1/build", (route) => route.fulfill({ json: info }));
  await login(page);
  const mobile = testInfo.project.name === "mobile";
  await expect(page.locator(".main-footer .version")).toHaveCount(0);
  await expect(page.getByRole("tooltip")).toHaveCount(0);
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
  await expect(sidebar.locator(".build-version")).toHaveCount(0);
  let tooltip = await openBuildInfo(page, mobile);
  await expect(tooltip.locator(".build-version")).toHaveText(info.version);
  await expect(tooltip.locator(".build-time")).toHaveText(
    "2026-10-29 09:45:00（UTC+8）",
  );
  await expect(tooltip.locator("dt svg")).toHaveCount(2);
  expect(
    await sidebar.evaluate((node) => node.scrollWidth <= node.clientWidth),
  ).toBeTruthy();
  expect(
    await page.locator(".profile-build-tooltip").evaluate((node) => {
      const bounds = node.getBoundingClientRect();
      return bounds.left >= 0 && bounds.right <= innerWidth;
    }),
  ).toBeTruthy();
  await page.screenshot({
    path: testInfo.outputPath("navigation-build-light.png"),
    fullPage: true,
  });
  await page.keyboard.press("Escape");
  await expect(page.getByRole("tooltip")).toHaveCount(0);
  if (!mobile) {
    await page.getByRole("button", { name: "查看版本和构建时间" }).focus();
    await expect(page.getByRole("tooltip")).toContainText(info.version);
    await page.keyboard.press("Escape");
    await expect(page.getByRole("tooltip")).toHaveCount(0);
  }
  if (mobile) await page.getByRole("button", { name: "关闭导航" }).click();
  await page.getByRole("button", { name: "切换深色主题" }).click();
  await openSidebar(page, mobile);
  await expect(page.locator("html")).toHaveClass(/dark/);
  tooltip = await openBuildInfo(page, mobile);
  await expect(tooltip.locator(".build-version")).toHaveText(info.version);
  await page.screenshot({
    path: testInfo.outputPath("navigation-build-dark.png"),
    fullPage: true,
  });

  if (mobile) {
    await page.getByRole("button", { name: "关闭导航" }).click();
  } else {
    await page.getByRole("button", { name: "隐藏导航", exact: true }).click();
  }
  await expect(page.getByRole("tooltip")).toHaveCount(0);
  if (!mobile)
    await page.getByRole("button", { name: "显示导航", exact: true }).click();

  info.version = "dev";
  info.built_at = "";
  await page.reload();
  await openSidebar(page, mobile);
  tooltip = await openBuildInfo(page, mobile);
  await expect(tooltip.locator(".build-version")).toHaveText("dev");
  await expect(tooltip.locator(".build-time")).toHaveText("未知");
});

test("unavailable build info does not block navigation", async ({
  page,
}, testInfo) => {
  await page.route("**/api/v1/build", (route) =>
    route.fulfill({ status: 503, json: { error: "build info unavailable" } }),
  );
  await login(page);
  const mobile = testInfo.project.name === "mobile";
  await openSidebar(page, mobile);
  const tooltip = await openBuildInfo(page, mobile);
  await expect(tooltip.locator(".build-version")).toHaveText("暂不可用");
  await expect(tooltip.locator(".build-time")).toHaveText("暂不可用");
  await page.getByRole("link", { name: "系统信息", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "系统信息", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("tooltip")).toHaveCount(0);
});
