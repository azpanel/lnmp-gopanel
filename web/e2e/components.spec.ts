import { test, expect } from "@playwright/test";

test("programming component versions, changelogs and refresh without services", async ({
  page,
}, testInfo) => {
  const now = new Date().toISOString();
  let componentRefreshes = 0;
  let releaseRefreshes = 0;
  let failed = false;
  const snapshot = () => ({
    status: failed ? "partial" : "ready",
    items: [
      {
        name: "phpmyadmin",
        status: "ready",
        version: "5.2.2",
        path: "/home/wwwroot/default/" + "a".repeat(32),
        access_url: "https://192.0.2.1/" + "a".repeat(32) + "/",
        checked_at: now,
      },
      {
        name: "python3",
        status: "ready",
        version: "3.13.5",
        path: "/usr/bin/python3",
        checked_at: now,
      },
      { name: "go", status: "missing", version: "", path: "", checked_at: now },
      {
        name: "node",
        status: failed ? "failed" : "ready",
        version: "22.23.4",
        path: "/root/.nvm/versions/node/v22.23.4/bin/node",
        error: failed ? "组件版本读取失败或超时" : "",
        checked_at: now,
      },
      {
        name: "codex",
        status: "ready",
        version: "0.164.0",
        path: "/root/.nvm/versions/node/v22.23.4/bin/codex",
        checked_at: now,
      },
      {
        name: "composer",
        status: failed ? "failed" : "ready",
        version: "2.10.2",
        path: "/usr/local/bin/composer",
        error: failed ? "组件版本读取失败或超时" : "",
        checked_at: now,
      },
    ],
  });
  const releases = Object.fromEntries(
    [
      ["python3", "3.15.2"],
      ["go", "1.27.3"],
      ["node", "26.12.1"],
      ["codex", "0.164.0"],
      ["composer", "2.10.3"],
      ["phpmyadmin", "5.2.3"],
    ].map(([name, version]) => [
      name,
      {
        status: "ready",
        version,
        source_url: "https://example.com/releases",
        changelog_url: `https://example.com/releases/${version}`,
        changelog: `Changes for ${version}\n<script>window.componentInjected = true</script>`,
        checked_at: now,
      },
    ]),
  );
  await page.route("**/api/v1/services", (route) =>
    route.fulfill({ json: [] }),
  );
  await page.route("**/api/v1/components", (route) =>
    route.fulfill({ json: snapshot() }),
  );
  await page.route("**/api/v1/components/refresh", (route) => {
    componentRefreshes++;
    failed = true;
    return route.fulfill({ status: 202, json: snapshot() });
  });
  await page.route("**/api/v1/services/releases", (route) =>
    route.fulfill({ json: releases }),
  );
  await page.route("**/api/v1/services/releases/refresh", (route) => {
    releaseRefreshes++;
    return route.fulfill({
      status: 202,
      json: {
        ...releases,
        go: {
          ...releases.go,
          changelog: "",
          changelog_error: "官方发布源返回 HTTP 503",
        },
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
  await page.goto("/services");
  await expect(
    page.getByRole("heading", { name: "编程组件", exact: true }),
  ).toBeVisible();
  await expect(page.locator("[data-component]")).toHaveCount(6);
  const componentGrid = page.locator(".programming-components .service-cards");
  const columnCount = () =>
    componentGrid.evaluate(
      (node) => getComputedStyle(node).gridTemplateColumns.split(" ").length,
    );
  await expect
    .poll(columnCount)
    .toBe(testInfo.project.name === "mobile" ? 1 : 3);
  if (testInfo.project.name === "desktop") {
    await page.setViewportSize({ width: 1000, height: 900 });
    await expect.poll(columnCount).toBe(2);
    await page.setViewportSize({ width: 1440, height: 1000 });
    await expect.poll(columnCount).toBe(3);
  }
  for (const name of [
    "python3",
    "go",
    "node",
    "codex",
    "composer",
    "phpmyadmin",
  ]) {
    const logo = page.locator(`[data-component="${name}"] .component-logo img`);
    await expect(logo).toBeVisible();
    await expect
      .poll(() =>
        logo.evaluate(
          (img: HTMLImageElement) => img.complete && img.naturalWidth > 0,
        ),
      )
      .toBe(true);
  }
  await page.screenshot({
    path: testInfo.outputPath("programming-component-logos.png"),
    fullPage: true,
  });
  const python = page.locator('[data-component="python3"]');
  const go = page.locator('[data-component="go"]');
  const node = page.locator('[data-component="node"]');
  const codex = page.locator('[data-component="codex"]');
  await expect(python).toContainText("3.13.5");
  await expect(python).toContainText("3.15.2");
  await expect(go).toContainText("未安装");
  await expect(node).toContainText("22.23.4");
  await expect(codex).toContainText("0.164.0");
  const phpMyAdmin = page.locator('[data-component="phpmyadmin"]');
  await expect(phpMyAdmin).toContainText("5.2.2");
  await expect(phpMyAdmin).toContainText("5.2.3");
  await expect(
    phpMyAdmin.getByRole("link", { name: "打开 phpMyAdmin" }),
  ).toHaveAttribute("href", "https://192.0.2.1/" + "a".repeat(32) + "/");
  await expect(
    phpMyAdmin.getByRole("link", { name: "打开 phpMyAdmin" }),
  ).toHaveAttribute("target", "_blank");
  const composer = page.locator('[data-component="composer"]');
  await expect(composer).toContainText("2.10.2");
  await expect(composer).toContainText("2.10.3");
  await expect(composer).toContainText("/usr/local/bin/composer");
  await composer.getByText("查看 2.10.3 更新日志").click();
  await expect(composer.locator("pre")).toContainText("Changes for 2.10.3");
  await expect(page.locator(".programming-components button")).toHaveCount(0);
  await python.getByText("查看 3.15.2 更新日志").click();
  await expect(python.locator("pre")).toContainText("Changes for 3.15.2");
  await expect(
    python.getByRole("link", { name: "Changelog 原文" }),
  ).toHaveAttribute("href", "https://example.com/releases/3.15.2");
  expect(await page.evaluate(() => "componentInjected" in window)).toBe(false);
  await page.getByRole("button", { name: "刷新状态", exact: true }).click();
  await expect(node).toContainText("显示上次成功结果");
  await expect(node).toContainText("22.23.4");
  await expect(composer).toContainText("显示上次成功结果");
  await expect(composer).toContainText("2.10.2");
  expect(componentRefreshes).toBe(1);
  await page.getByRole("button", { name: "检查最新版本", exact: true }).click();
  await expect(go).toContainText("Changelog 读取失败");
  await expect(go).toContainText("1.27.3");
  expect(releaseRefreshes).toBe(1);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("component helper and upstream failures display errors", async ({
  page,
}) => {
  await page.route("**/api/v1/components", (route) =>
    route.fulfill({
      json: { status: "failed", items: [], error: "组件查询辅助程序不可用" },
    }),
  );
  await page.route("**/api/v1/services/releases", (route) =>
    route.fulfill({
      json: Object.fromEntries(
        ["python3", "go", "node", "codex", "composer", "phpmyadmin"].map(
          (name) => [
            name,
            { status: "failed", error: "连接官方发布源失败或超时" },
          ],
        ),
      ),
    }),
  );
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "系统概览", exact: true }),
  ).toBeVisible();
  await page.goto("/services");
  await expect(page.getByRole("alert")).toContainText("组件查询辅助程序不可用");
  await expect(page.locator("[data-component]")).toHaveCount(6);
  await expect(page.locator('[data-component="python3"]')).toContainText(
    "读取失败",
  );
  await expect(page.locator('[data-component="python3"]')).toContainText(
    "暂不可用",
  );
});

test("phpMyAdmin saved Basic Auth link and missing location", async ({
  page,
}) => {
  let access = "https://192.0.2.2/phpmyadmin/";
  const snapshot = () => ({
    status: "ready",
    items: [
      {
        name: "phpmyadmin",
        status: "ready",
        version: "5.2.3",
        path: "/home/wwwroot/default/phpmyadmin",
        access_url: access,
        checked_at: new Date().toISOString(),
      },
    ],
  });
  await page.route("**/api/v1/components", (route) =>
    route.fulfill({ json: snapshot() }),
  );
  await page.route("**/api/v1/components/refresh", (route) => {
    access = "";
    return route.fulfill({ status: 202, json: snapshot() });
  });
  await page.route("**/api/v1/services/releases", (route) =>
    route.fulfill({ json: {} }),
  );
  await page.goto("/services");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  const card = page.locator('[data-component="phpmyadmin"]');
  await expect(
    card.getByRole("link", { name: "打开 phpMyAdmin" }),
  ).toHaveAttribute("href", access);
  await page.getByRole("button", { name: "刷新状态", exact: true }).click();
  await expect(card.getByRole("link", { name: "打开 phpMyAdmin" })).toHaveCount(
    0,
  );
  await expect(card).toContainText("未找到已保存的访问地址");
});
