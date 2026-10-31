import { test, expect } from "./fixtures";

function release(range: string, version: string, status = "ready") {
  return {
    range,
    status,
    version,
    source_url: "https://nginx.org/en/download.html",
    changelog_url: "",
    changelog: "",
    checked_at: null,
    attempted_at: null,
  };
}

async function login(page: import("@playwright/test").Page) {
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "系统概览", exact: true }),
  ).toBeVisible();
  await page.goto("/services");
}

async function mockServices(page: import("@playwright/test").Page) {
  await page.route("**/api/v1/services", (route) =>
    route.fulfill({
      json: ["nginx", "php-fpm", "mariadb", "mysql", "redis"].map((name) => ({
        name,
        state: "running",
        manager: "init",
        actions: [],
        busy: false,
        version: name === "nginx" ? "1.26.3" : "5.0.0",
        checked_at: new Date().toISOString(),
        message: "运行中",
      })),
    }),
  );
  await page.route("**/api/v1/components", (route) =>
    route.fulfill({ json: { status: "ready", items: [] } }),
  );
}

test("service release ranges validate, save, reset and preserve failed drafts", async ({
  page,
}, testInfo) => {
  await mockServices(page);
  const releases = {
    nginx: release("1.28", "1.28.3"),
    "php-fpm": release("8.3", "8.3.36"),
    mariadb: release("11.4", "11.4.13"),
  };
  const writes: string[] = [];
  let fail = false;
  let refreshedRange = "";
  await page.route("**/api/v1/services/releases", (route) =>
    route.fulfill({ json: releases }),
  );
  await page.route("**/api/v1/services/releases/nginx/range", (route) => {
    const range = route.request().postDataJSON().range as string;
    writes.push(range);
    if (fail)
      return route.fulfill({
        status: 503,
        json: { error: "版本范围配置保存失败" },
      });
    releases.nginx = release(range, "", "loading");
    return route.fulfill({ status: 202, json: releases.nginx });
  });
  await page.route("**/api/v1/services/releases/refresh", (route) => {
    refreshedRange = releases.nginx.range;
    return route.fulfill({ status: 202, json: releases });
  });
  await login(page);
  const trigger = page.getByRole("button", {
    name: "检查最新版本",
    exact: true,
  });
  await expect(page.locator(".service-cards input")).toHaveCount(0);
  await expect(page.locator(".service-release-range")).toHaveCount(0);
  await trigger.click();
  const dialog = page.getByRole("dialog", { name: "版本检查与设置" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("textbox")).toHaveCount(5);
  await expect(
    dialog.getByLabel("Node.js 版本范围", { exact: true }),
  ).toHaveValue("");
  await expect(
    dialog.getByLabel("Python 3 版本范围", { exact: true }),
  ).toHaveValue("");
  expect(refreshedRange).toBe("");
  await page.screenshot({
    path: testInfo.outputPath("release-range-dialog.png"),
  });
  expect(
    await dialog.evaluate((node) => {
      const bounds = node.getBoundingClientRect();
      return bounds.top >= 0 && bounds.bottom <= window.innerHeight;
    }),
  ).toBe(true);
  await expect(page.locator('[data-service="mysql"]')).not.toContainText(
    "保存并检查",
  );
  const card = page.locator('[data-service="nginx"]');
  const form = dialog.locator('[data-release-service="nginx"]');
  const input = form.getByLabel("Nginx 版本范围", { exact: true });
  const save = form.getByRole("button", { name: "保存并检查", exact: true });
  await expect(input).toHaveValue("1.28");
  await expect(
    dialog.getByLabel("PHP-FPM 版本范围", { exact: true }),
  ).toHaveValue("8.3");
  await expect(
    dialog.getByLabel("MariaDB 版本范围", { exact: true }),
  ).toHaveValue("11.4");
  await input.fill("1.28.1");
  await save.click();
  await expect(form.getByRole("alert")).toContainText("请填写大版本");
  expect(writes).toEqual([]);
  await input.fill("1.29");
  await save.click();
  await expect(card).toContainText("范围 1.29 内最新版本");
  await expect(card.locator(".service-release")).not.toContainText("1.28.3");
  releases.nginx = release("1.29", "1.29.10");
  await expect(card).toContainText("1.29.10");
  await page.reload();
  await trigger.click();
  await expect(input).toHaveValue("1.29");
  fail = true;
  await input.fill("1.27");
  await save.click();
  await expect(form.getByRole("alert")).toContainText("保存失败");
  await expect(input).toHaveValue("1.27");
  await expect(card).toContainText("范围 1.29 内最新版本");
  await dialog
    .getByRole("button", { name: "检查全部版本", exact: true })
    .click();
  await expect.poll(() => refreshedRange).toBe("1.29");
  fail = false;
  await input.fill("");
  await save.click();
  await expect(card).toContainText("最新稳定版");
  releases.nginx = release("", "1.30.6");
  await expect(card).toContainText("1.30.6");
  await page.reload();
  await trigger.click();
  await expect(input).toHaveValue("");
  expect(writes).toEqual(["1.29", "1.27", ""]);
  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(trigger).toBeFocused();
  await expect(page.locator(".service-cards input")).toHaveCount(0);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("range polling syncs saved settings without replacing an editing draft", async ({
  page,
}) => {
  await mockServices(page);
  let current = release("8.3", "", "loading");
  let reads = 0;
  await page.route("**/api/v1/services/releases", (route) => {
    reads++;
    return route.fulfill({ json: { "php-fpm": current } });
  });
  await login(page);
  const card = page.locator('[data-service="php-fpm"]');
  await page.getByRole("button", { name: "检查最新版本", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "版本检查与设置" });
  const input = dialog.getByLabel("PHP-FPM 版本范围", { exact: true });
  await expect(input).toHaveValue("8.3");
  current = release("8.4", "", "loading");
  await expect(input).toHaveValue("8.4");
  await input.fill("8.2");
  const previousReads = reads;
  current = release("8.5", "8.5.12");
  await expect.poll(() => reads).toBeGreaterThan(previousReads);
  await expect(card).toContainText("范围 8.5 内最新版本");
  await expect(input).toHaveValue("8.2");
});

for (const component of [
  {
    name: "node",
    title: "Node.js",
    major: "22",
    minor: "22.23",
    version: "22.23.10",
    latest: "26.13.0",
  },
  {
    name: "python3",
    title: "Python 3",
    major: "3",
    minor: "3.13",
    version: "3.13.18",
    latest: "3.15.2",
  },
]) {
  test(`${component.title} ranges work without LNMP services and persist across reopen`, async ({
    page,
  }) => {
    await mockServices(page);
    await page.route("**/api/v1/services", (route) =>
      route.fulfill({ json: [] }),
    );
    let current = release("", component.latest);
    let refreshed = "";
    const writes: string[] = [];
    await page.route("**/api/v1/services/releases", (route) =>
      route.fulfill({ json: { [component.name]: current } }),
    );
    await page.route(
      `**/api/v1/services/releases/${component.name}/range`,
      (route) => {
        const range = route.request().postDataJSON().range as string;
        writes.push(range);
        current = release(range, range ? component.version : component.latest);
        return route.fulfill({ status: 202, json: current });
      },
    );
    await page.route("**/api/v1/services/releases/refresh", (route) => {
      refreshed = current.range;
      return route.fulfill({
        status: 202,
        json: { [component.name]: current },
      });
    });
    await login(page);
    const trigger = page.getByRole("button", {
      name: "检查最新版本",
      exact: true,
    });
    await trigger.click();
    const dialog = page.getByRole("dialog", { name: "版本检查与设置" });
    await expect(dialog.getByRole("textbox")).toHaveCount(2);
    const form = dialog.locator(`[data-release-service="${component.name}"]`);
    const input = form.getByLabel(`${component.title} 版本范围`, {
      exact: true,
    });
    const save = form.getByRole("button", { name: "保存并检查", exact: true });
    await input.fill(component.minor + ".1");
    await save.click();
    await expect(form.getByRole("alert")).toContainText("请填写大版本");
    expect(writes).toEqual([]);
    for (const range of [component.major, component.minor]) {
      await input.fill(range);
      await save.click();
      await expect(
        page.locator(`[data-component="${component.name}"]`),
      ).toContainText(`范围 ${range} 内最新版本`);
    }
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    await trigger.click();
    await expect(input).toHaveValue(component.minor);
    await dialog
      .getByRole("button", { name: "检查全部版本", exact: true })
      .click();
    await expect.poll(() => refreshed).toBe(component.minor);
    await page.reload();
    await trigger.click();
    await expect(input).toHaveValue(component.minor);
    await input.fill("");
    await save.click();
    const card = page.locator(`[data-component="${component.name}"]`);
    await expect(card).toContainText("最新稳定版");
    await expect(card).toContainText(component.latest);
    expect(writes).toEqual([component.major, component.minor, ""]);
    expect(
      await dialog.evaluate((node) => node.scrollWidth <= node.clientWidth),
    ).toBe(true);
  });
}
