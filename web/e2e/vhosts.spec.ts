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
        issuer: "CN=ZeroSSL RSA Domain Secure Site CA,O=ZeroSSL,C=AT",
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
  await page.route("**/api/v1/vhosts/count", (route) =>
    route.fulfill({ json: { count: state.sites.length } }),
  );
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
      language: "en-US",
      load_timeout_seconds: 120,
      auth_enabled: true,
      username: "preview-user",
      password: "test-screenshot-password",
    });
    screenshotCaptures++;
    screenshotPolls = 0;
    screenshot = {
      name: site.name,
      url: input.url,
      language: input.language,
      load_timeout_seconds: input.load_timeout_seconds,
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
  await expect(
    page
      .getByRole("link", { name: "虚拟主机", exact: true })
      .locator(".nav-count"),
  ).toHaveText("1");
  await page.getByRole("link", { name: "虚拟主机", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "虚拟主机", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("CN=ZeroSSL RSA Domain Secure Site CA,O=ZeroSSL,C=AT", {
      exact: true,
    }),
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
  await expect(page.getByLabel("浏览器语言", { exact: true })).toHaveValue(
    "zh-CN",
  );
  await expect(
    page.getByLabel("最长加载时间（秒）", { exact: true }),
  ).toHaveValue("30");
  await page.getByLabel("浏览器语言", { exact: true }).fill("en-US");
  await page.getByLabel("最长加载时间（秒）", { exact: true }).fill("120");
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
  await expect(page.getByLabel("浏览器语言", { exact: true })).toHaveValue(
    "en-US",
  );
  await expect(
    page.getByLabel("最长加载时间（秒）", { exact: true }),
  ).toHaveValue("120");
  await expect(
    page.getByLabel("Basic Auth 密码", { exact: true }),
  ).toHaveAttribute("placeholder", "密码已保存，留空保留");
  await page.keyboard.press("Escape");
  await page
    .getByRole("textbox", { name: "筛选虚拟主机" })
    .fill("missing.example");
  await expect(
    page
      .getByRole("link", { name: "虚拟主机", exact: true })
      .locator(".nav-count"),
  ).toHaveText("1");
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
  await expect(table.getByRole("columnheader")).toHaveText([
    "站点图片",
    "域名 / 配置",
    "状态 / 监听",
    "站点目录 / 占用",
    "SSL 证书",
    "操作",
  ]);
  await expect(table.locator("tbody tr")).toHaveCount(1);
  await expect(
    table.getByRole("list", { name: "监听端口" }).getByRole("listitem"),
  ).toHaveText(["80", "443SSL"]);
  await expect(table.locator(".listen-endpoint-tls")).toHaveAttribute(
    "title",
    "443 ssl",
  );
  await expect(page.locator(".vhost-card")).toHaveCount(0);
  await expect(table).toContainText("www.example.com");
  await expect(table).toContainText("2.0 GiB");
  await expect(table).toContainText("/home/wwwroot/example");
  const certificateStatus = table.getByText("有效 · 剩余 60 天", {
    exact: true,
  });
  await expect(certificateStatus).toHaveCount(1);
  await expect(certificateStatus).toBeVisible();
  const row = table.locator("tbody tr").first();
  const cells = row.getByRole("cell");
  const imageCell = cells.nth(0);
  const siteCell = cells.nth(1);
  await expect(siteCell).toContainText("example.conf");
  await expect(siteCell).toContainText("www.example.com");
  await expect(
    imageCell.getByAltText("example.com 的网站截图", { exact: true }),
  ).toBeVisible();
  await expect(imageCell).not.toContainText("example.conf");
  await expect(cells.nth(2)).toContainText("已启用");
  await expect(
    cells.nth(2).getByRole("list", { name: "监听端口" }),
  ).toBeVisible();
  await expect(cells.nth(3)).toHaveText("/home/wwwroot/example（2.0 GiB）");
  await expect(table).not.toContainText("统计上一级项目目录");
  await expect(table).not.toContainText("按实际磁盘块统计");
  const previewBounds = await imageCell
    .locator(".website-preview")
    .boundingBox();
  const nameBounds = await siteCell.locator(".vhost-table-name").boundingBox();
  expect(previewBounds).not.toBeNull();
  expect(nameBounds).not.toBeNull();
  expect(previewBounds!.x + previewBounds!.width).toBeLessThan(nameBounds!.x);
  expect((await row.boundingBox())!.height).toBeLessThanOrEqual(110);
  await expect(cells.nth(4).locator('[data-slot="badge"]')).toHaveText([
    "有效 · 剩余 60 天",
    "ZeroSSL",
  ]);
  await expect(cells.nth(4).locator("details")).toHaveCount(0);
  await expect(cells.nth(4)).not.toContainText("生效时间");
  await expect(cells.nth(4)).not.toContainText("证书详情");
  await table.getByRole("button", { name: "查看配置", exact: true }).click();
  await expect(page.locator(".vhost-config-source")).toContainText(
    "# original configuration",
  );
  await page.keyboard.press("Escape");
  await page.getByRole("textbox", { name: "筛选虚拟主机" }).fill("example");
  await page.getByRole("button", { name: "卡片视图", exact: true }).click();
  await expect(page.locator(".vhost-card")).toHaveCount(1);
  const certificateDetails = page.locator(".vhost-card .site-certificate");
  await expect(
    certificateDetails.getByText("生效时间", { exact: true }),
  ).toBeVisible();
  await expect(
    certificateDetails.getByText("到期时间", { exact: true }),
  ).toBeVisible();
  await certificateDetails.getByText("证书详情", { exact: true }).click();
  await expect(
    certificateDetails.getByText("/etc/nginx/ssl/example/fullchain.pem", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    certificateDetails.getByText("42", { exact: true }),
  ).toBeVisible();
  await expect(
    page.locator(".vhost-card").getByText("有效 · 剩余 60 天", { exact: true }),
  ).toBeVisible();
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
