import { test, expect } from "@playwright/test";

test("login, real metrics, routes, pause, theme and logout", async ({
  page,
}, testInfo) => {
  // Real-time status must not depend on the browser and server clocks agreeing.
  await page.addInitScript(() => {
    const realNow = Date.now.bind(Date);
    Date.now = () => realNow() + 3_600_000;
  });
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "登录管理面板" }),
  ).toBeVisible();
  await page.getByLabel("密码", { exact: true }).fill("wrong-password");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(page.getByRole("alert")).toContainText("用户名或密码错误");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "系统概览", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("实时连接", { exact: true })).toBeVisible();
  await expect(
    page.getByText("未检测到 LNMP 服务", { exact: true }),
  ).toBeVisible();
  await expect(page.locator(".stat-value").first()).not.toContainText("—", {
    timeout: 10000,
  });
  await expect(page.locator(".chart-wait")).toHaveCount(0, { timeout: 10000 });
  await expect(page.locator(".recharts-area-area").first()).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("dashboard.png"),
    fullPage: true,
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
  await page.getByRole("button", { name: "暂停实时更新" }).click();
  await expect(page.getByText("已暂停", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "继续实时更新" }).click();
  await expect(page.getByText("实时连接", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "切换浅色主题" }).click();
  await expect(page.locator("html")).not.toHaveClass(/dark/);
  await page.getByRole("button", { name: "切换深色主题" }).click();
  const mobile = testInfo.project.name === "mobile";
  if (mobile) await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("link", { name: "LNMP 服务" }).click();
  await expect(
    page.getByRole("heading", { name: "LNMP 服务", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("未检测到可管理的服务")).toBeVisible();
  if (mobile) await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("link", { name: "系统信息", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "系统信息", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("主机名称", { exact: true })).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "系统信息", exact: true }),
  ).toBeVisible();
  if (mobile) await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("button", { name: "退出登录" }).click();
  await expect(
    page.getByRole("heading", { name: "登录管理面板" }),
  ).toBeVisible();
  expect(errors).toEqual([]);
});

test("service confirmation and failed operation are visible without touching host services", async ({
  page,
}, testInfo) => {
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "系统概览", exact: true }),
  ).toBeVisible();
  await page.route("**/api/v1/services", (route) =>
    route.fulfill({
      json: [
        {
          name: "nginx",
          state: "running",
          manager: "init",
          actions: ["start", "stop", "restart", "reload"],
          message: "服务运行中",
          checked_at: new Date().toISOString(),
          busy: false,
        },
      ],
    }),
  );
  let called = false;
  await page.route("**/api/v1/services/nginx/actions/reload", (route) => {
    called = true;
    return route.fulfill({
      status: 502,
      json: { error: "Nginx 配置检查失败，未执行重载" },
    });
  });
  await page.goto("/services");
  await page.getByRole("button", { name: "重载", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  expect(called).toBe(false);
  await page.getByRole("button", { name: "确认重载" }).click();
  await expect(
    page.getByText("Nginx 配置检查失败，未执行重载", { exact: true }),
  ).toBeVisible();
  expect(called).toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("service-confirmation.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
});
