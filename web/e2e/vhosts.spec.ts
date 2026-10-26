import { test, expect } from "@playwright/test";
import type { WebsiteScreenshot } from "../src/lib/types";

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
  let screenshot: WebsiteScreenshot | null = null;
  let screenshotPolls = 0;
  let screenshotCaptures = 0;
  await page.route("**/api/v1/vhosts/screenshots", (route) => {
    if (
      screenshot &&
      (screenshot.status === "queued" || screenshot.status === "running")
    ) {
      screenshotPolls++;
      if (screenshotPolls < 2) screenshot.status = "running";
      else if (screenshotCaptures > 1) {
        screenshot.status = "failed";
        screenshot.error = "网站认证失败，请检查 Basic Auth 信息";
      } else {
        screenshot.status = "ready";
        screenshot.image_version = "image-1";
        screenshot.captured_at = now;
        screenshot.captured_url = screenshot.url;
      }
    }
    return route.fulfill({
      json: {
        browser_available: true,
        items: screenshot ? { [site.name]: screenshot } : {},
      },
    });
  });
  await page.route("**/api/v1/vhosts/screenshot?*", (route) => {
    if (route.request().method() === "DELETE") {
      screenshot = null;
      return route.fulfill({ status: 204 });
    }
    const input = route.request().postDataJSON();
    expect(input).toEqual({
      url: "http://127.0.0.1:8080/private",
      auth_enabled: true,
      username: "preview-user",
      password: "test-screenshot-password",
    });
    screenshotCaptures++;
    screenshotPolls = 0;
    screenshot = {
      name: site.name,
      url: input.url,
      auth_enabled: true,
      username: input.username,
      password_saved: true,
      status: "queued",
      captured_at: null,
    };
    return route.fulfill({ status: 202, json: screenshot });
  });
  await page.route("**/api/v1/vhosts/screenshot/refresh?*", (route) => {
    screenshotCaptures++;
    screenshotPolls = 0;
    screenshot!.status = "queued";
    return route.fulfill({ status: 202, json: screenshot });
  });
  await page.route("**/api/v1/vhosts/screenshot/image?*", (route) =>
    route.fulfill({
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="1440" height="900"><rect width="1440" height="900" fill="#153e37"/><text x="80" y="150" fill="#fff" font-size="64">Website preview</text></svg>',
    }),
  );
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
  await page.getByRole("button", { name: "截图设置", exact: true }).click();
  await page
    .getByLabel("截图地址", { exact: true })
    .fill("http://127.0.0.1:8080/private");
  await page.getByRole("switch", { name: "使用 Basic Auth" }).click();
  await page
    .getByLabel("Basic Auth 用户名", { exact: true })
    .fill("preview-user");
  await page
    .getByLabel("Basic Auth 密码", { exact: true })
    .fill("test-screenshot-password");
  await page.getByRole("button", { name: "保存并截图", exact: true }).click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await expect(
    page.getByAltText("example.com 的网站截图", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "放大网站截图 example.com", exact: true })
    .click();
  await expect(
    page
      .getByRole("dialog")
      .getByAltText("example.conf 的完整网站截图", { exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "截图设置", exact: true }).click();
  await expect(page.getByLabel("Basic Auth 密码", { exact: true })).toHaveValue(
    "",
  );
  await expect(
    page.getByLabel("Basic Auth 密码", { exact: true }),
  ).toHaveAttribute("placeholder", "密码已保存，留空保留");
  await page.keyboard.press("Escape");
  await page
    .getByRole("textbox", { name: "筛选虚拟主机" })
    .fill("missing.example");
  await expect(
    page.getByRole("heading", { name: "没有匹配的站点" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "表格视图", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "表格视图", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("textbox", { name: "筛选虚拟主机" })).toHaveValue(
    "missing.example",
  );
  await expect(
    page.getByRole("heading", { name: "没有匹配的站点" }),
  ).toBeVisible();
  await page.getByRole("textbox", { name: "筛选虚拟主机" }).fill("");
  const table = page.getByRole("table", { name: "虚拟主机列表" });
  await expect(table).toBeVisible();
  await expect(table.locator("tbody tr")).toHaveCount(1);
  await expect(page.locator(".vhost-card")).toHaveCount(0);
  await expect(table).toContainText("www.example.com");
  await expect(table).toContainText("2.0 GiB");
  await expect(table).toContainText("/home/wwwroot/example");
  await expect(table).toContainText("有效 · 剩余 60 天");
  await expect(
    table.getByAltText("example.com 的网站截图", { exact: true }),
  ).toBeVisible();
  await table.getByRole("button", { name: "查看配置", exact: true }).click();
  await expect(page.locator(".vhost-config-source")).toContainText(
    "# original configuration",
  );
  await page.keyboard.press("Escape");
  await table.locator(".vhost-table-certificates > details > summary").click();
  await expect(
    table.getByText("CN=Test Issuer,O=Certificate Authority", { exact: true }),
  ).toBeVisible();
  await page.getByRole("textbox", { name: "筛选虚拟主机" }).fill("example");
  await page.getByRole("button", { name: "卡片视图", exact: true }).click();
  await expect(page.locator(".vhost-card")).toHaveCount(1);
  await expect(page.getByRole("textbox", { name: "筛选虚拟主机" })).toHaveValue(
    "example",
  );
  await page.getByRole("button", { name: "表格视图", exact: true }).click();
  await page.getByRole("textbox", { name: "筛选虚拟主机" }).fill("");
  await page.screenshot({
    path: testInfo.outputPath("vhosts-table.png"),
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
  await page
    .getByRole("button", { name: "重新截图 example.com", exact: true })
    .click();
  await expect(
    page.getByText("网站认证失败，请检查 Basic Auth 信息（保留上次成功截图）", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    table.getByAltText("example.com 的网站截图", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "截图设置", exact: true }).click();
  await page.getByRole("button", { name: "清除设置", exact: true }).click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await expect(
    page.getByAltText("example.com 的网站截图", { exact: true }),
  ).not.toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  expect(errors).toEqual([]);
});
