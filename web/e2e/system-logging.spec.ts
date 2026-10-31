import { test, expect, type Page } from "./fixtures";

function snapshot() {
  return {
    php: {
      kind: "php",
      service: "php-fpm",
      path: "/usr/local/php/var/log/php_errors.log",
      available: true,
      configured: false,
      message: "开启后将重启对应服务",
    },
    database: {
      kind: "database",
      service: "mysql",
      path: "/var/log/mysql/mariadb-slow.log",
      available: true,
      configured: false,
      message: "开启后将重启对应服务",
    },
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

test("logging enable confirms restart, prevents duplicates and refreshes state", async ({
  page,
}) => {
  const state = snapshot();
  const calls: string[] = [];
  let release: (() => void) | undefined;
  await page.route("**/api/v1/system/logging", (route) =>
    route.fulfill({ json: state }),
  );
  await page.route("**/api/v1/system/logging/*/enable", async (route) => {
    const kind = route.request().url().includes("/php/") ? "php" : "database";
    calls.push(kind);
    expect(route.request().method()).toBe("POST");
    expect(route.request().headers()["x-csrf-token"]).toBeTruthy();
    expect(route.request().postData()).toBeNull();
    await new Promise<void>((resolve) => {
      release = resolve;
    });
    state[kind].configured = true;
    state[kind].message = "已配置，可在日志页面查看";
    await route.fulfill({ json: { message: "日志已开启，服务已重启" } });
  });
  await login(page);
  const php = page.getByRole("button", {
    name: "打开PHP错误日志",
    exact: true,
    includeHidden: true,
  });
  const database = page.getByRole("button", {
    name: "打开DB慢日志",
    exact: true,
    includeHidden: true,
  });
  await expect(php).toBeEnabled();
  await php.click();
  await expect(page.getByRole("dialog")).toContainText("重启 php-fpm");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  expect(calls).toEqual([]);
  for (const [button, kind] of [
    [php, "php"],
    [database, "database"],
  ] as const) {
    await button.click();
    if (kind === "database")
      await expect(page.getByRole("dialog")).toContainText("未配置时设为 2 秒");
    await page.getByRole("button", { name: "确认打开", exact: true }).click();
    await expect.poll(() => calls.length).toBe(kind === "php" ? 1 : 2);
    await expect(
      page.getByRole("button", { name: "执行中…", exact: true }),
    ).toBeDisabled();
    await expect(php).toBeDisabled();
    await expect(database).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toBeVisible();
    release!();
    await expect(page.getByRole("dialog")).toBeHidden();
    await expect(button).toBeDisabled();
    await expect(
      page.getByRole("group", {
        name: kind === "php" ? "PHP错误日志" : "DB慢日志",
        exact: true,
      }),
    ).toContainText("已配置");
  }
  expect(calls).toEqual(["php", "database"]);
  await page.reload();
  await expect(php).toBeDisabled();
  await expect(database).toBeDisabled();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
});

test("logging failures and unavailable services remain visible and recoverable", async ({
  page,
}) => {
  const state = snapshot();
  state.database.available = false;
  state.database.message = "未配置对应服务";
  let statusFailure = false;
  await page.route("**/api/v1/system/logging", (route) =>
    route.fulfill(
      statusFailure
        ? { status: 503, json: { error: "请同步升级辅助程序和 sudoers 权限" } }
        : { json: state },
    ),
  );
  await page.route("**/api/v1/system/logging/php/enable", (route) =>
    route.fulfill({
      status: 409,
      json: { error: "服务重启失败；已恢复原配置及权限" },
    }),
  );
  await login(page);
  const php = page.getByRole("button", {
    name: "打开PHP错误日志",
    exact: true,
    includeHidden: true,
  });
  const database = page.getByRole("button", {
    name: "打开DB慢日志",
    exact: true,
    includeHidden: true,
  });
  await expect(database).toBeDisabled();
  await expect(
    page.getByRole("group", { name: "DB慢日志", exact: true }),
  ).toContainText("未配置对应服务");
  await php.click();
  await page.getByRole("button", { name: "确认打开", exact: true }).click();
  await expect(
    page.getByText("服务重启失败；已恢复原配置及权限", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "确认打开", exact: true }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(php).toBeEnabled();
  statusFailure = true;
  await page.reload();
  await expect(page.getByRole("alert")).toContainText("sudoers");
  await expect(php).toBeDisabled();
  await expect(database).toBeDisabled();
  statusFailure = false;
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await expect(php).toBeEnabled();
  await expect(
    page.getByRole("heading", { name: "系统信息", exact: true }),
  ).toBeVisible();
});
