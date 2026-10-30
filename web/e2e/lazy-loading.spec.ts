import { expect, test, type Page } from "./fixtures";

const password = "Test-Only-Panel-Password-2026";

async function authenticate(page: Page) {
  const response = await page.request.post("/api/v1/auth/login", {
    data: { username: "admin", password },
  });
  expect(response.ok()).toBe(true);
}

async function openNavigation(page: Page, mobile: boolean) {
  const menu = page.getByRole("button", { name: "打开导航", exact: true });
  if (mobile && (await menu.getAttribute("aria-expanded")) !== "true")
    await menu.click();
}

test("a failed optional demo preserves the login form and allows login", async ({
  page,
  isMobile,
}) => {
  test.skip(isMobile, "Mobile does not load the optional demo");
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  let navigations = 0;
  page.on("request", (request) => {
    if (request.isNavigationRequest()) navigations++;
  });
  await page.route("**/assets/login-demo-*.js", (route) => route.abort());
  await page.goto("/");
  await expect(page.getByRole("status")).toContainText("演示暂不可用");
  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.unroute("**/assets/login-demo-*.js");
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(page.getByLabel("密码", { exact: true })).toHaveValue(password);
  await page.getByRole("button", { name: "登录面板", exact: true }).click();
  await expect(page.locator(".workspace")).toBeVisible();
  expect(navigations).toBe(1);
  expect(errors).toEqual([]);
});

test("a failed workspace offers a reload that recovers the authenticated page", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await authenticate(page);
  await page.route("**/assets/workspace-*.js", (route) => route.abort());
  await page.goto("/");
  await expect(page.getByRole("alert")).toContainText("工作空间加载失败");
  await page.unroute("**/assets/workspace-*.js");
  await page.getByRole("button", { name: "重新加载页面", exact: true }).click();
  await expect(page.locator(".workspace")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "系统概览", exact: true }),
  ).toBeVisible();
  expect(errors).toEqual([]);
});

test("a failed business page preserves navigation and can recover after reload", async ({
  page,
  isMobile,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await authenticate(page);
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "系统概览", exact: true }),
  ).toBeVisible();
  await page.route("**/assets/system-*.js", (route) => route.abort());
  await openNavigation(page, isMobile);
  await page.getByRole("link", { name: "系统信息", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("页面加载失败");
  await expect(page.locator(".workspace")).toBeVisible();
  await openNavigation(page, isMobile);
  await page.getByRole("link", { name: "系统概览", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "系统概览", exact: true }),
  ).toBeVisible();
  await openNavigation(page, isMobile);
  await page.getByRole("link", { name: "系统信息", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("页面加载失败");
  await page.unroute("**/assets/system-*.js");
  await page.getByRole("button", { name: "重新加载页面", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "系统信息", exact: true }),
  ).toBeVisible();
  expect(errors).toEqual([]);
});

test("failed account settings preserve the workspace and the dialog can close and recover", async ({
  page,
  isMobile,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await authenticate(page);
  await page.goto("/");
  await expect(page.locator(".workspace")).toBeVisible();
  await page.route("**/assets/account-security-contents-*.js", (route) =>
    route.abort(),
  );
  await openNavigation(page, isMobile);
  const trigger = page.getByRole("button", { name: "账户安全", exact: true });
  await trigger.click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("alert")).toContainText("账户设置加载失败");
  await expect(page.locator(".workspace")).toBeVisible();
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await trigger.click();
  await page.unroute("**/assets/account-security-contents-*.js");
  await dialog
    .getByRole("button", { name: "重新加载页面", exact: true })
    .click();
  await expect(page.locator(".workspace")).toBeVisible();
  await openNavigation(page, isMobile);
  await trigger.click();
  await expect(
    dialog.getByRole("tab", { name: "修改密码", exact: true }),
  ).toBeVisible();
  expect(errors).toEqual([]);
});
