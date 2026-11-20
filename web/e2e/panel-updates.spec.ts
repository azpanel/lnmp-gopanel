import { test, expect, type Page } from "./fixtures";

const release = {
  current_version: "v0.1.29",
  latest_version: "v0.1.30",
  has_update: true,
  changelog:
    "## 更新日志\n\n修复面板问题\n<script>window.updateInjected = true</script>\n" +
    "更多更新内容\n".repeat(70),
  release_url: "https://github.com/azpanel/lnmp-gopanel/releases/tag/v0.1.30",
};
const notice = (page: Page) =>
  page.getByRole("button", { name: "发现新版本 v0.1.30，查看更新日志" });

async function login(page: Page) {
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板", exact: true }).click();
  await expect(page.locator(".workspace")).toBeVisible();
}

async function sidebar(page: Page) {
  await expect(page.locator(".workspace")).toBeVisible();
  const toggle = page.getByRole("button", { name: "打开导航", exact: true });
  if ((await page.viewportSize())!.width <= 720) {
    await expect(toggle).toBeVisible();
    if ((await toggle.getAttribute("aria-expanded")) !== "true")
      await toggle.click();
  }
  await expect(page.locator(".sidebar")).toHaveAttribute(
    "aria-hidden",
    "false",
  );
}

async function settings(page: Page) {
  await page.getByRole("button", { name: "打开界面设置" }).click();
  return page.getByRole("dialog", { name: "界面设置" });
}

test("login update notice survives refresh, dismisses for the session, and preserves build info", async ({
  page,
}, testInfo) => {
  let checks = 0;
  await page.route("**/api/v1/panel/release", (route) => {
    checks++;
    return route.fulfill({ json: release });
  });
  await login(page);
  await sidebar(page);
  await expect(notice(page)).toBeVisible();
  expect(checks).toBe(1);
  const avatar = page.getByRole("button", { name: "查看版本和构建时间" });
  await avatar.scrollIntoViewIfNeeded();
  await expect(notice(page)).toBeInViewport();
  await page.screenshot({ path: testInfo.outputPath("update-notice.png") });
  await expect
    .poll(async () => {
      const badge = await notice(page).boundingBox();
      const profile = await avatar.boundingBox();
      return (
        !!badge &&
        !!profile &&
        badge.y + badge.height < profile.y &&
        badge.x >= 0 &&
        badge.x + badge.width <= (await page.viewportSize())!.width
      );
    })
    .toBe(true);
  await page.reload();
  await sidebar(page);
  await expect(notice(page)).toBeVisible();
  expect(checks).toBe(1);
  await page.getByRole("link", { name: "系统信息", exact: true }).click();
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await sidebar(page);
  await notice(page).click();
  const dialog = page.getByRole("dialog", { name: "新版本更新日志" });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("当前版本 v0.1.29 / 最新版本 v0.1.30");
  await expect(dialog.locator("pre")).toContainText(
    "<script>window.updateInjected = true</script>",
  );
  expect(await page.evaluate(() => "updateInjected" in window)).toBe(false);
  await expect(
    dialog.getByRole("link", { name: "查看 GitHub Release" }),
  ).toHaveAttribute("href", release.release_url);
  expect(
    await dialog
      .locator("pre")
      .evaluate((node) => node.scrollHeight > node.clientHeight),
  ).toBe(true);
  await page.screenshot({ path: testInfo.outputPath("update-dialog.png") });
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(notice(page)).toHaveCount(0);
  await expect(avatar).toBeFocused();
  await avatar.click();
  await expect(page.getByRole("tooltip")).toContainText("构建时间");
  await page.keyboard.press("Escape");
  await page.reload();
  await sidebar(page);
  await expect(notice(page)).toHaveCount(0);
  expect(checks).toBe(1);
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await page.getByRole("button", { name: "确认退出" }).click();
  await expect(
    page.getByRole("heading", { name: "登录管理面板" }),
  ).toBeVisible();
  await login(page);
  await sidebar(page);
  await expect(notice(page)).toBeVisible();
  expect(checks).toBe(2);
});

test("update switch persists and re-enabling checks immediately with a white settings dialog", async ({
  page,
}) => {
  let checks = 0;
  await page.addInitScript(() => {
    if (!localStorage.getItem("check-panel-updates"))
      localStorage.setItem("check-panel-updates", "false");
  });
  await page.route("**/api/v1/panel/release", (route) => {
    checks++;
    return route.fulfill({ json: { ...release, changelog: "" } });
  });
  await login(page);
  await sidebar(page);
  let dialog = await settings(page);
  await expect(
    dialog.getByRole("switch", { name: "检查更新" }),
  ).not.toBeChecked();
  expect(checks).toBe(0);
  await dialog.getByRole("switch", { name: "检查更新" }).click();
  await page.keyboard.press("Escape");
  await expect(notice(page)).toBeVisible();
  expect(checks).toBe(1);
  await notice(page).click();
  await expect(
    page.getByRole("dialog", { name: "新版本更新日志" }),
  ).toContainText("该版本暂未提供更新日志");
  await page.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "新版本更新日志" }),
  ).toHaveCount(0);
  if ((await page.viewportSize())!.width <= 720)
    await page.getByRole("button", { name: "关闭导航" }).click();
  await page.getByRole("button", { name: "切换深色主题" }).click();
  await sidebar(page);
  dialog = await settings(page);
  await expect(dialog).toHaveCSS("background-color", "rgb(255, 255, 255)");
  await dialog.getByRole("switch", { name: "检查更新" }).click();
  await page.keyboard.press("Escape");
  await expect(notice(page)).toHaveCount(0);
  await page.reload();
  await sidebar(page);
  dialog = await settings(page);
  await expect(
    dialog.getByRole("switch", { name: "检查更新" }),
  ).not.toBeChecked();
  await dialog.getByRole("switch", { name: "检查更新" }).click();
  await page.keyboard.press("Escape");
  await expect(notice(page)).toBeVisible();
  expect(checks).toBe(2);
});

test("failed and current-version checks remain quiet and do not repeat on refresh", async ({
  page,
}) => {
  let checks = 0;
  await page.route("**/api/v1/panel/release", (route) => {
    checks++;
    return route.fulfill({ status: 502, json: { error: "GitHub rate limit" } });
  });
  await login(page);
  await sidebar(page);
  await expect.poll(() => checks).toBe(1);
  await expect(notice(page)).toHaveCount(0);
  await expect(page.locator("[data-sonner-toast]")).toHaveCount(0);
  await page.reload();
  await sidebar(page);
  await expect(notice(page)).toHaveCount(0);
  expect(checks).toBe(1);
  await page.route("**/api/v1/panel/release", (route) => {
    checks++;
    return route.fulfill({ json: { ...release, has_update: false } });
  });
  const dialog = await settings(page);
  await dialog.getByRole("switch", { name: "检查更新" }).click();
  await dialog.getByRole("switch", { name: "检查更新" }).click();
  await page.keyboard.press("Escape");
  await expect.poll(() => checks).toBe(2);
  await expect(notice(page)).toHaveCount(0);
});

test("TOTP challenge does not check until verification succeeds", async ({
  page,
}) => {
  let checks = 0;
  let authenticated: unknown;
  await page.route("**/api/v1/panel/release", (route) => {
    checks++;
    return route.fulfill({ json: release });
  });
  await page.route("**/api/v1/auth/login", async (route) => {
    const response = await route.fetch();
    authenticated = await response.json();
    await route.fulfill({
      response,
      json: {
        totp_required: true,
        challenge_token: "test-challenge",
        expires_in: 300,
      },
    });
  });
  await page.route("**/api/v1/auth/login/totp", (route) =>
    route.fulfill({ json: authenticated }),
  );
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板", exact: true }).click();
  await expect(page.getByLabel("动态验证码", { exact: true })).toBeVisible();
  expect(checks).toBe(0);
  await page.getByLabel("动态验证码", { exact: true }).fill("123456");
  await expect(page.locator(".workspace")).toBeVisible();
  await sidebar(page);
  await expect(notice(page)).toBeVisible();
  expect(checks).toBe(1);
});
