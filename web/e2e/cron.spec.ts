import { test, expect, type BrowserContext } from "@playwright/test";

test.use({ timezoneId: "America/Los_Angeles" });

const command = `/root/backup  'directory with spaces' %data # literal ${"long-path/".repeat(40)}`;
const raw = `SHELL=/bin/bash\n# backup\n0 2 * * * ${command}\n@reboot /root/start\nunknown entry\n`;
const snapshot = {
  tasks: [
    { line: 3, schedule: "0 2 * * *", command },
    { line: 4, schedule: "@reboot", command: "/root/start" },
  ],
  raw,
  warnings: ["第 5 行未识别为定时任务，请查看 crontab 原文。"],
  updated_at: "2026-10-26T01:02:03Z",
};

// Reuse this spec's session so UI scenarios do not exhaust the real login limiter.
let sessionCookies: Awaited<ReturnType<BrowserContext["cookies"]>> | undefined;

async function login(page: import("@playwright/test").Page) {
  if (sessionCookies) {
    await page.context().addCookies(sessionCookies);
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "系统概览", exact: true }),
    ).toBeVisible();
    return;
  }
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "系统概览", exact: true }),
  ).toBeVisible();
  sessionCookies = await page.context().cookies();
}

test("cron navigation, source, refresh failures, retry and empty state", async ({
  page,
}, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  let mode: "ready" | "failed" | "empty" = "ready";
  let calls = 0;
  await page.route("**/api/v1/cron", (route) => {
    calls++;
    expect(route.request().method()).toBe("GET");
    if (mode === "failed")
      return route.fulfill({ status: 503, json: { error: "辅助程序不可用" } });
    return route.fulfill({
      json:
        mode === "empty"
          ? { ...snapshot, tasks: [], raw: "", warnings: [] }
          : snapshot,
    });
  });
  await login(page);
  if (testInfo.project.name === "mobile")
    await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("link", { name: "定时任务", exact: true }).click();
  await expect(page).toHaveURL(/\/cron$/);
  await expect(
    page.getByRole("heading", { name: "定时任务", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("2 项任务", { exact: false })).toBeVisible();
  const table = page.getByRole("table", { name: "定时任务列表" });
  await expect(table.getByText(command, { exact: true })).toBeVisible();
  await expect(table.getByText("@reboot", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("status").filter({ hasText: "第 5 行" }),
  ).toBeVisible();
  await page.getByText("查看 crontab 原文", { exact: true }).click();
  await expect(page.locator(".cron-source pre")).toHaveText(raw);
  await expect(page.locator(".cron-source .token.schedule")).toHaveCount(2);
  await expect(
    page.locator(".cron-source .token.comment").first(),
  ).toBeVisible();
  expect(await page.locator(".cron-source pre").textContent()).toBe(raw);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
  expect(
    await table.evaluate((element) => {
      const container = element.parentElement!;
      return container.scrollWidth <= container.clientWidth;
    }),
  ).toBeTruthy();
  await page.screenshot({
    path: testInfo.outputPath("cron.png"),
    fullPage: true,
  });

  mode = "failed";
  await page.getByRole("button", { name: "刷新", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("可能已过期");
  await expect(table.getByText(command, { exact: true })).toBeVisible();
  mode = "empty";
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveCount(0);
  await expect(
    page.getByText("root 用户尚未配置定时任务。", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("0 项任务", { exact: false })).toBeVisible();
  await expect(page.locator(".cron-source pre")).toHaveText("（空）");
  expect(calls).toBe(3);
  expect(errors).toEqual([]);
});

test("cron previews five server-zone times on hover, focus and touch", async ({
  page,
}, testInfo) => {
  let previewCalls = 0;
  await page.route("**/api/v1/cron", (route) =>
    route.fulfill({ json: snapshot }),
  );
  await page.route("**/api/v1/cron/preview?*", (route) => {
    previewCalls++;
    const reboot =
      new URL(route.request().url()).searchParams.get("line") === "4";
    return route.fulfill({
      json: {
        line: reboot ? 4 : 3,
        schedule: reboot ? "@reboot" : "0 2 * * *",
        timezone: reboot ? "" : "Asia/Shanghai",
        times: reboot
          ? []
          : [27, 28, 29, 30, 31].map((day) => `2026-10-${day}T02:00:00+08:00`),
        message: reboot ? "系统启动时执行，无法预测执行时间。" : "",
      },
    });
  });
  await login(page);
  await page.goto("/cron");
  const trigger = page.getByRole("button", {
    name: "查看 0 2 * * * 的执行时间",
    exact: true,
  });
  await expect(trigger).toBeVisible();
  expect(previewCalls).toBe(0);
  if (testInfo.project.name === "mobile") await trigger.tap();
  else await trigger.hover();
  const tooltip = page.getByRole("tooltip");
  await expect(tooltip).toContainText("接下来 5 次预计执行时间");
  await expect(tooltip).toContainText("Asia/Shanghai");
  await expect(tooltip.locator("time")).toHaveCount(5);
  await expect(tooltip.locator("time").first()).toHaveText(
    "2026-10-27 02:00:00 +08:00",
  );
  await expect(tooltip.locator("time").first()).toHaveAttribute(
    "datetime",
    "2026-10-27T02:00:00+08:00",
  );
  if (testInfo.project.name === "desktop") {
    await tooltip.hover();
    await page.waitForTimeout(250);
    await expect(tooltip).toBeVisible();
  }
  await page.screenshot({
    path: testInfo.outputPath("cron-preview.png"),
    fullPage: true,
  });
  await page.keyboard.press("Escape");
  await expect(tooltip).toHaveCount(0);
  await page.mouse.move(0, 0);
  await page.getByRole("heading", { name: "定时任务", exact: true }).click();
  await trigger.focus();
  await expect(tooltip.locator("time")).toHaveCount(5);
  expect(previewCalls).toBe(2);
  await page.keyboard.press("Escape");
  await page
    .getByRole("button", { name: "查看 @reboot 的执行时间", exact: true })
    .click();
  await expect(tooltip).toContainText("系统启动时执行");
  await expect(tooltip.locator("time")).toHaveCount(0);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
});

test("cron preview failures retry on reopen and changed schedules require refresh", async ({
  page,
}) => {
  let failed = true;
  await page.route("**/api/v1/cron", (route) =>
    route.fulfill({ json: snapshot }),
  );
  await page.route("**/api/v1/cron/preview?*", (route) =>
    route.fulfill(
      failed
        ? {
            status: 503,
            json: { error: "读取失败" },
          }
        : {
            json: {
              line: 3,
              schedule: "0 3 * * *",
              timezone: "UTC",
              times: ["2026-10-27T03:00:00Z"],
            },
          },
    ),
  );
  await login(page);
  await page.goto("/cron");
  const trigger = page.getByRole("button", {
    name: "查看 0 2 * * * 的执行时间",
    exact: true,
  });
  await trigger.focus();
  await expect(page.getByRole("tooltip").getByRole("alert")).toContainText(
    "读取失败",
  );
  await page.keyboard.press("Escape");
  await page.getByRole("heading", { name: "定时任务", exact: true }).click();
  failed = false;
  await trigger.focus();
  await expect(page.getByRole("tooltip")).toContainText(
    "任务配置已变化，请刷新列表。",
  );
  await expect(page.getByRole("tooltip").locator("time")).toHaveCount(0);
});

test("cron direct route, initial loading and failed read recovery", async ({
  page,
}) => {
  await login(page);
  let mode: "loading" | "failed" | "ready" = "loading";
  let release: () => void = () => {};
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/v1/cron", async (route) => {
    if (mode === "loading") await pending;
    if (mode !== "ready")
      return route.fulfill({
        status: 503,
        json: { error: "本机未安装 crontab" },
      });
    return route.fulfill({ json: snapshot });
  });
  await page.goto("/cron");
  await expect(
    page.getByText("正在读取定时任务…", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "读取中…", exact: true }),
  ).toBeDisabled();
  mode = "failed";
  release();
  await expect(page.getByRole("alert")).toContainText("本机未安装 crontab");
  await expect(
    page.getByText("root 用户尚未配置定时任务。", { exact: true }),
  ).toHaveCount(0);
  mode = "ready";
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await expect(
    page.getByRole("table").getByText("@reboot", { exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "定时任务", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("table").getByText("@reboot", { exact: true }),
  ).toBeVisible();
});
