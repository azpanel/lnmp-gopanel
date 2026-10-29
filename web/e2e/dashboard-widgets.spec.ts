import { test, expect } from "@playwright/test";
import type { Snapshot } from "../src/lib/types";

const ids = [
  "disk_io",
  "load",
  "swap",
  "memory_breakdown",
  "processes",
  "nginx",
  "php_fpm",
  "database",
];
const titles = [
  "磁盘读写速率",
  "系统负载趋势",
  "Swap 使用趋势",
  "内存构成",
  "进程资源排行",
  "Nginx 活跃连接数",
  "PHP-FPM 进程与队列",
  "数据库状态",
];

async function login(page: import("@playwright/test").Page) {
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "系统概览", exact: true }),
  ).toBeVisible();
}
function fixture(timestamp: number): Snapshot {
  return {
    timestamp: new Date(timestamp).toISOString(),
    cpu: 30,
    uptime: 100,
    memory: {
      total: 8192,
      used: 4096,
      percent: 50,
      available: 4096,
      free: 2048,
      buffers: 512,
      cached: 1536,
      other: 4096,
    },
    swap: { total: 4096, used: 1024, percent: 25 },
    load: { one: 1, five: 2, fifteen: 3 },
    network: { read: 0, write: 100 },
    disk_io: { read: 1024, write: 2048 },
    disks: [],
    errors: {},
    nginx: { active: 0 },
    php_fpm: { active: 2, idle: 3, queue: 0 },
    database: { connections: 4, slow_queries: 7 },
    processes: {
      cpu: [
        {
          pid: 123,
          name: "cpu-worker",
          cpu: 50,
          rss: 1000,
          memory_percent: 12.2,
        },
      ],
      memory: [
        {
          pid: 456,
          name: "memory-worker",
          cpu: 1,
          rss: 2000,
          memory_percent: 24.4,
        },
      ],
      partial: true,
    },
  };
}

test("optional widgets persist on the server and render on desktop and mobile", async ({
  page,
  context,
  browser,
}, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/api/v1/metrics/stream", (route) => {
    const now = Date.now();
    return route.fulfill({
      contentType: "text/event-stream",
      body: [fixture(now - 2000), fixture(now)]
        .map((value) => `event:metrics\ndata:${JSON.stringify(value)}\n\n`)
        .join(""),
    });
  });
  await login(page);
  const session = await (await page.request.get("/api/v1/auth/session")).json();
  const saved = await (
    await page.request.get("/api/v1/dashboard/widgets")
  ).json();
  try {
    await expect(page.locator(".optional-widget")).toHaveCount(0);
    await page
      .getByRole("button", { name: "添加状态指标", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await dialog.getByRole("checkbox", { name: /磁盘读写速率/ }).check();
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(page.locator(".optional-widget")).toHaveCount(0);
    await page
      .getByRole("button", { name: "添加状态指标", exact: true })
      .click();
    for (const checkbox of await dialog.getByRole("checkbox").all())
      await checkbox.check();
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).not.toBeVisible();
    await expect(page.locator(".optional-widget")).toHaveCount(8);
    for (const [index, id] of ids.entries())
      await expect(page.locator(`[data-widget="${id}"]`)).toContainText(
        titles[index],
      );
    await expect(
      page.locator('[data-widget="load"] .recharts-reference-line-line'),
    ).toHaveAttribute("stroke-dasharray", "5 4");
    await expect(
      page.locator('[data-widget="load"]').getByText(/^\d+ 核$/),
    ).toBeVisible();
    await expect(
      page.locator('[data-widget="memory_breakdown"] .recharts-bar'),
    ).toHaveCount(4);
    await expect(page.locator('[data-widget="nginx"]')).toContainText(
      "当前活跃连接：0",
    );
    await expect(page.locator('[data-widget="database"]')).toContainText(
      "慢查询累计 7",
    );
    await expect(page.locator('[data-widget="processes"]')).toContainText(
      "cpu-worker",
    );
    await page
      .locator('[data-widget="processes"]')
      .getByRole("button", { name: "内存", exact: true })
      .click();
    await expect(page.locator('[data-widget="processes"]')).toContainText(
      "memory-worker",
    );
    await expect(page.locator('[data-widget="processes"]')).toContainText(
      "排行可能不完整",
    );
    await page.reload();
    await expect(page.locator(".optional-widget")).toHaveCount(8);
    const other = await browser.newContext({
      storageState: await context.storageState(),
      baseURL: testInfo.project.use.baseURL,
      ignoreHTTPSErrors: true,
    });
    try {
      const otherPage = await other.newPage();
      await otherPage.goto("/");
      await expect(otherPage.locator(".optional-widget")).toHaveCount(8);
    } finally {
      await other.close();
    }
    await expect(page.getByText("资源使用趋势", { exact: true })).toBeVisible();
    await expect(page.getByText("网络吞吐", { exact: true })).toBeVisible();
    const clipped = await page
      .locator(".optional-widget .recharts-cartesian-axis-tick text")
      .evaluateAll((ticks) =>
        ticks
          .filter((tick) => {
            const bounds = tick.getBoundingClientRect();
            const svg = tick.closest("svg")!.getBoundingClientRect();
            const card = tick
              .closest(".optional-widget")!
              .getBoundingClientRect();
            return (
              bounds.left < Math.max(svg.left, card.left) - 1 ||
              bounds.right > Math.min(svg.right, card.right) + 1
            );
          })
          .map((tick) => tick.textContent),
      );
    expect(clipped).toEqual([]);
    await page.screenshot({
      path: testInfo.outputPath("dashboard-widgets.png"),
      fullPage: true,
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();

    await page.route("**/api/v1/dashboard/widgets", async (route) => {
      if (route.request().method() === "PUT")
        await route.fulfill({ status: 503, json: { error: "测试保存失败" } });
      else await route.continue();
    });
    await page
      .getByRole("button", { name: "移除磁盘读写速率", exact: true })
      .click();
    await expect(page.getByText("测试保存失败", { exact: true })).toBeVisible();
    await expect(page.locator('[data-widget="disk_io"]')).toBeVisible();
    await page.unroute("**/api/v1/dashboard/widgets");
    await page
      .getByRole("button", { name: "移除磁盘读写速率", exact: true })
      .click();
    await expect(page.locator(".optional-widget")).toHaveCount(7);
    await page.reload();
    await expect(page.locator(".optional-widget")).toHaveCount(7);
    expect(errors).toEqual([]);
  } finally {
    await page.request.put("/api/v1/dashboard/widgets", {
      headers: { "X-CSRF-Token": session.csrf_token },
      data: saved,
    });
  }
});

test("widget loading failures prevent overwrites and unconfigured probes show hints", async ({
  page,
}) => {
  await login(page);
  const session = await (await page.request.get("/api/v1/auth/session")).json();
  const saved = await (
    await page.request.get("/api/v1/dashboard/widgets")
  ).json();
  try {
    await page.route("**/api/v1/dashboard/widgets", (route) =>
      route.fulfill({ status: 503, json: { error: "测试配置不可读" } }),
    );
    await page.reload();
    await page
      .getByRole("button", { name: "添加状态指标", exact: true })
      .click();
    const dialog = page.getByRole("dialog");
    await expect(
      dialog.getByRole("button", { name: "保存", exact: true }),
    ).toBeDisabled();
    await expect(dialog.getByRole("checkbox").first()).toBeDisabled();
    await page.unroute("**/api/v1/dashboard/widgets");
    await dialog.getByRole("button", { name: "重试", exact: true }).click();
    await expect(
      dialog.getByRole("button", { name: "保存", exact: true }),
    ).toBeEnabled();
    for (const name of [/Nginx 活跃连接数/, /PHP-FPM 进程与队列/, /数据库状态/])
      await dialog.getByRole("checkbox", { name }).check();
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.locator('[data-widget="nginx"]')).toContainText(
      "metrics.nginx_status_url",
    );
    await expect(page.locator('[data-widget="php_fpm"]')).toContainText(
      "metrics.php_fpm_status_url",
    );
    await expect(page.locator('[data-widget="database"]')).toContainText(
      "metrics.mysql",
    );
  } finally {
    await page.request.put("/api/v1/dashboard/widgets", {
      headers: { "X-CSRF-Token": session.csrf_token },
      data: saved,
    });
  }
});
