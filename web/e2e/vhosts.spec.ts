import { test, expect } from "@playwright/test";

test("virtual hosts show certificates, public parent usage, configuration and guarded toggles", async ({
  page,
}, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const now = new Date().toISOString();
  const site = {
    name: "example.conf",
    filename: "example.conf",
    enabled: true,
    domains: ["example.com", "www.example.com"],
    listens: ["80", "443 ssl"],
    roots: [
      {
        configured: "/home/wwwroot/example/public",
        path: "/home/wwwroot/example",
        public_parent: true,
      },
    ],
    proxies: [],
    certificates: [
      {
        path: "/etc/nginx/ssl/example/fullchain.pem",
        issuer: "CN=Test Issuer,O=Certificate Authority",
        subject: "CN=example.com",
        dns_names: ["example.com", "www.example.com"],
        not_before: now,
        not_after: "2027-01-01T00:00:00Z",
        days_remaining: 60,
        serial: "42",
        fingerprint: "a".repeat(64),
        status: "valid",
      },
    ],
    warnings: [],
    modified: now,
  };
  const state = {
    status: "ready",
    sites: [site],
    usage: {
      "/home/wwwroot/example": {
        path: "/home/wwwroot/example",
        bytes: 1024 ** 3 * 2,
        updated_at: now,
      },
    },
    updated_at: now,
  };
  let calls = 0;
  await page.route("**/api/v1/vhosts", (route) =>
    route.fulfill({ json: state }),
  );
  await page.route("**/api/v1/vhosts/config?*", (route) =>
    route.fulfill({
      json: {
        filename: site.filename,
        content:
          "# original configuration\nserver { root /home/wwwroot/example/public; }\n",
      },
    }),
  );
  await page.route("**/api/v1/vhosts/toggle", (route) => {
    calls++;
    if (calls === 2)
      return route.fulfill({
        status: 502,
        json: { error: "Nginx 配置检查失败，已恢复原文件名，未执行重载" },
      });
    const input = route.request().postDataJSON();
    expect(input).toEqual({ name: "example.conf", enabled: false });
    site.enabled = false;
    site.filename = "example.conf.disabled";
    return route.fulfill({
      json: {
        name: site.name,
        enabled: false,
        changed: true,
        message: "站点已停用，Nginx 已重载",
      },
    });
  });
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "系统概览", exact: true }),
  ).toBeVisible();
  if (testInfo.project.name === "mobile")
    await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("link", { name: "虚拟主机", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "虚拟主机", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("CN=Test Issuer,O=Certificate Authority", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("2.0 GiB", { exact: true })).toBeVisible();
  await expect(
    page.getByText("入口 /home/wwwroot/example/public，统计上一级项目目录", {
      exact: true,
    }),
  ).toBeVisible();
  await page.getByRole("button", { name: "查看配置", exact: true }).click();
  await expect(page.locator(".vhost-config-source")).toContainText(
    "# original configuration",
  );
  await page.keyboard.press("Escape");
  await page
    .getByRole("textbox", { name: "筛选虚拟主机" })
    .fill("missing.example");
  await expect(
    page.getByRole("heading", { name: "没有匹配的站点" }),
  ).toBeVisible();
  await page.getByRole("textbox", { name: "筛选虚拟主机" }).fill("");
  await page.screenshot({
    path: testInfo.outputPath("vhosts.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "停用", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText(
    "example.conf → example.conf.disabled",
  );
  expect(calls).toBe(0);
  await page.getByRole("button", { name: "确认停用", exact: true }).click();
  await expect(
    page.getByText("example.conf.disabled", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "启用", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "启用", exact: true }).click();
  await page.getByRole("button", { name: "确认启用", exact: true }).click();
  await expect(page.getByRole("dialog").getByRole("alert")).toContainText(
    "已恢复原文件名",
  );
  expect(calls).toBe(2);
  await page.getByRole("button", { name: "取消", exact: true }).click();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  expect(errors).toEqual([]);
});
