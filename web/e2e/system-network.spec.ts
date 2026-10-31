import { test, expect, type Page } from "./fixtures";
import type { Snapshot } from "../src/lib/types";

function snapshot(): Snapshot {
  return {
    timestamp: new Date().toISOString(),
    cpu: 5,
    uptime: 432000,
    memory: { total: 8589934592, used: 2147483648, percent: 25 },
    swap: { total: 2147483648, used: 1024, percent: 0.1 },
    load: { one: 0.34, five: 0.56, fifteen: 0.47 },
    network: { read: 4096, write: 8192 },
    network_info: {
      name: "eth0",
      up: true,
      ipv4: ["192.0.2.13"],
      ipv6: ["2001:db8:1234:5678::13"],
      rates: { read: 1024, write: 2048 },
    },
    disk_io: { read: 0, write: 0 },
    disks: [],
    errors: {},
  };
}
async function login(page: Page) {
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(page.locator(".workspace")).toBeVisible();
  await page.goto("/system");
}
function field(page: Page, label: string) {
  return page
    .locator(".system-network-fields > div")
    .filter({ has: page.locator("dt").getByText(label, { exact: true }) })
    .locator("dd");
}
async function mockMetrics(page: Page, current: () => Snapshot) {
  await page.route("**/api/v1/metrics/stream", (route) =>
    route.fulfill({
      contentType: "text/event-stream",
      body: `event:metrics\ndata:${JSON.stringify({ ...current(), timestamp: new Date().toISOString() })}\n\n`,
    }),
  );
  await page.route("**/api/v1/host", (route) =>
    route.fulfill({
      json: {
        hostname: "test-host",
        os: "linux",
        platform: "debian",
        version: "13.2",
        kernel: "6.12.107+deb13-amd64",
        arch: "amd64",
        cores: 2,
        cpu_model: "AMD EPYC 7K62 48-Core Processor",
      },
    }),
  );
  await page.route("**/api/v1/system/logging", (route) =>
    route.fulfill({
      json: {
        php: {
          kind: "php",
          service: "php-fpm",
          path: "/usr/local/php/var/log/php_errors.log",
          available: true,
          configured: true,
          message: "已配置，可在日志页面查看",
        },
        database: {
          kind: "database",
          service: "mariadb",
          path: "/var/log/mysql/mariadb-slow.log",
          available: true,
          configured: true,
          message: "已配置，可在日志页面查看",
        },
      },
    }),
  );
}

test("compact network card shows primary NIC, addresses and its own rates in both themes", async ({
  page,
}, testInfo) => {
  const value = snapshot();
  await mockMetrics(page, () => value);
  await login(page);
  const card = page.getByRole("region", { name: "网络信息", exact: true });
  await expect(card).toBeVisible();
  await expect(
    page.locator(".system-left > .system-network-card"),
  ).toBeVisible();
  await expect(card.getByText("已连接", { exact: true })).toBeVisible();
  await expect(field(page, "主网卡")).toHaveText("eth0");
  await expect(field(page, "IPv4")).toHaveText("192.0.2.13");
  await expect(field(page, "IPv6")).toHaveText("2001:db8:1234:5678::13");
  await expect(card.getByTitle("接收速率")).toContainText("1.0 KiB/s");
  await expect(card.getByTitle("发送速率")).toContainText("2.0 KiB/s");
  await expect(card).not.toContainText("4.0 KiB");
  for (const theme of ["light", "dark"]) {
    if (theme === "dark")
      await page.getByRole("button", { name: "切换深色主题" }).click();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBeTruthy();
    expect(
      await card
        .locator("dd")
        .evaluateAll((nodes) =>
          nodes.every((node) => node.scrollWidth <= node.clientWidth),
        ),
    ).toBeTruthy();
    const box = await card.boundingBox();
    expect(box?.height).toBeLessThan(220);
    await page.screenshot({
      path: testInfo.outputPath(`system-network-${theme}.png`),
      fullPage: true,
      animations: "disabled",
    });
  }
});

test("network card handles disconnected NIC, missing IPv6 and unavailable samples", async ({
  page,
}) => {
  let value = snapshot();
  value.network_info = {
    ...value.network_info!,
    name: "eth0",
    up: false,
    ipv6: [],
    rates: null,
  };
  await mockMetrics(page, () => value);
  await login(page);
  const card = page.getByRole("region", { name: "网络信息", exact: true });
  await expect(card.getByText("未连接", { exact: true })).toBeVisible();
  await expect(field(page, "IPv6")).toHaveText("未分配");
  await expect(card.getByTitle("接收速率")).toContainText("—/s");
  await expect(card.getByTitle("发送速率")).toContainText("—/s");
  value = {
    ...snapshot(),
    network_info: undefined,
    errors: { network_info: "网络接口读取失败" },
  };
  await page.reload();
  await expect(card.getByRole("status")).toHaveText("网络接口读取失败");
  await expect(field(page, "主网卡")).toHaveText("—");
  await expect(card.getByText("未连接", { exact: true })).toHaveCount(0);
  value = snapshot();
  value.network_info!.ipv6 = [
    "2001:db8:1234:5678:90ab:cdef:1234:5678",
    "fe80::70c1:c89a:f3b6:7d26%eth0",
  ];
  value.network_info!.rates = { read: 0, write: 0 };
  await page.reload();
  await expect(field(page, "IPv6")).toContainText(
    "fe80::70c1:c89a:f3b6:7d26%eth0",
  );
  await expect(card.getByTitle("接收速率")).toContainText("0 B/s");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
});
