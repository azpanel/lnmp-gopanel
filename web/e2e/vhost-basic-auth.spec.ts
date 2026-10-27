import { test, expect } from "@playwright/test";
import type { VhostBasicAuth } from "../src/lib/types";

test("vhost authentication supports lifecycle, failures and read-only states", async ({
  page,
}, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const site = {
    name: "example.conf",
    filename: "example.conf",
    enabled: true,
    domains: ["example.com"],
    listens: ["80"],
    roots: [],
    proxies: [],
    certificates: [],
    warnings: [],
    modified: new Date().toISOString(),
  };
  let auth: VhostBasicAuth = {
    name: site.name,
    status: "disabled",
    managed: false,
    site_enabled: true,
  };
  let writes = 0;
  let deletes = 0;
  let fail = false;
  let release: (() => void) | undefined;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/v1/vhosts", (route) =>
    route.fulfill({
      json: { status: "ready", sites: [site], usage: {}, updated_at: null },
    }),
  );
  await page.route("**/api/v1/vhosts/count", (route) =>
    route.fulfill({ json: { count: 1 } }),
  );
  await page.route("**/api/v1/vhosts/screenshots", (route) =>
    route.fulfill({ json: { items: {}, browser_available: false } }),
  );
  await page.route("**/api/v1/vhosts/basic-auth**", async (route) => {
    const method = route.request().method();
    if (method === "GET") return route.fulfill({ json: auth });
    expect(route.request().headers()["x-csrf-token"]).toBeTruthy();
    if (method === "DELETE") {
      deletes++;
      auth = {
        ...auth,
        status: "disabled",
        managed: false,
        username: undefined,
      };
      return route.fulfill({ json: auth });
    }
    writes++;
    if (writes === 1) await gate;
    if (fail)
      return route.fulfill({
        status: 502,
        json: { error: "Nginx 配置检查失败，已恢复原配置" },
      });
    const input = route.request().postDataJSON();
    expect(input.name).toBe(site.name);
    expect(input.password).toBe(
      writes === 1 ? "first-password" : "replacement-password",
    );
    auth = {
      ...auth,
      status: "enabled",
      managed: true,
      username: input.username,
    };
    return route.fulfill({ json: auth });
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
  await page.getByRole("link", { name: "虚拟主机" }).click();
  const open = async () => {
    await page.getByRole("button", { name: "访问认证", exact: true }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
  };
  await open();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("未开启");
  await page.getByLabel("认证用户名", { exact: true }).fill("first-user");
  await page.getByLabel("认证密码", { exact: true }).fill("first-password");
  await page.getByRole("button", { name: "开启认证", exact: true }).click();
  await expect(page.getByLabel("认证用户名", { exact: true })).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
  release!();
  await expect(dialog).toContainText("当前用户名：first-user");
  await expect(page.getByLabel("新认证密码", { exact: true })).toHaveValue("");
  await page.getByLabel("认证用户名", { exact: true }).fill("replacement-user");
  await page
    .getByLabel("新认证密码", { exact: true })
    .fill("replacement-password");
  fail = true;
  await page.getByRole("button", { name: "修改凭证", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("已恢复原配置");
  await expect(page.getByLabel("新认证密码", { exact: true })).toHaveValue(
    "replacement-password",
  );
  await expect(page.getByLabel("认证用户名", { exact: true })).toHaveValue(
    "replacement-user",
  );
  fail = false;
  await page.getByRole("button", { name: "修改凭证", exact: true }).click();
  await expect(dialog).toContainText("当前用户名：replacement-user");
  await page.getByRole("button", { name: "关闭窗口", exact: true }).click();
  await page.getByRole("button", { name: "表格视图", exact: true }).click();
  await open();
  await expect(page.getByLabel("新认证密码", { exact: true })).toHaveValue("");
  await expect(page.getByLabel("认证用户名", { exact: true })).toHaveValue(
    "replacement-user",
  );
  await page.getByRole("button", { name: "关闭认证", exact: true }).click();
  expect(deletes).toBe(0);
  await page.getByRole("button", { name: "确认关闭认证", exact: true }).click();
  await expect(dialog).toContainText("未开启");
  expect(deletes).toBe(1);
  await page.keyboard.press("Escape");
  for (const state of ["external", "unknown", "disabled-site"] as const) {
    auth = {
      name: site.name,
      status: state === "disabled-site" ? "enabled" : state,
      managed: state === "disabled-site",
      site_enabled: state !== "disabled-site",
      message:
        state === "external"
          ? "检测到已有 auth_basic 配置"
          : "无法确认配置状态",
    };
    await open();
    await expect(dialog).toContainText(
      state === "external"
        ? "外部配置"
        : state === "unknown"
          ? "无法确认"
          : "站点已停用",
    );
    await expect(
      page.getByRole("button", { name: "开启认证", exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "修改凭证", exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "关闭认证", exact: true }),
    ).toHaveCount(0);
    await page.keyboard.press("Escape");
  }
  expect(errors).toEqual([]);
});
