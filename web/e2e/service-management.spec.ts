import { test, expect } from "./fixtures";

test("unmanaged nginx blocks actions and repairs only after preview and confirmation", async ({
  page,
}) => {
  const revision = "a".repeat(64);
  let fixed = false;
  let failRepair = true;
  let previewAvailable = true;
  let repairs = 0;
  let actions = 0;
  await page.route("**/api/v1/services", (route) =>
    route.fulfill({
      json: [
        {
          name: "nginx",
          state: fixed ? "running" : "unmanaged",
          manager: fixed ? "init" : "systemd",
          actions: ["start", "stop", "restart", "reload"],
          message: fixed
            ? "服务进程运行中"
            : "管理状态异常：Nginx 正在配置的系统服务之外运行",
          checked_at: new Date().toISOString(),
          busy: false,
          version: "1.31.8",
        },
      ],
    }),
  );
  await page.route("**/api/v1/services/nginx/actions/*", (route) => {
    actions++;
    return route.fulfill({ json: { message: "unexpected operation" } });
  });
  await page.route("**/api/v1/services/nginx/management-repair", (route) => {
    if (route.request().method() === "GET") {
      if (!previewAvailable)
        return route.fulfill({
          status: 409,
          json: { error: "PID 文件与实际主进程不一致，请通过 SSH 检查" },
        });
      return route.fulfill({
        json: {
          revision,
          current_manager: "systemd",
          proposed_manager: "init",
          master_pid: 123,
          restore_pid: true,
          message: "修复管理方式",
        },
      });
    }
    repairs++;
    expect(route.request().postDataJSON()).toEqual({ revision });
    expect(route.request().headers()["x-csrf-token"]).toBeTruthy();
    if (failRepair)
      return route.fulfill({
        status: 409,
        json: { error: "修复失败，已恢复原配置和 PID 记录" },
      });
    fixed = true;
    return route.fulfill({
      json: { state: "running", manager: "init", message: "管理方式已修复" },
    });
  });
  await page.goto("/services");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "LNMP 服务", exact: true }),
  ).toBeVisible();
  const card = page
    .locator(".service-cards > div")
    .filter({ has: page.getByRole("heading", { name: "Nginx", exact: true }) });
  await expect(card.getByText("管理状态异常", { exact: true })).toBeVisible();
  for (const name of ["启动", "停止", "重启", "重载"])
    await expect(
      card.getByRole("button", { name, exact: true }),
    ).toBeDisabled();
  await card.getByRole("button", { name: "修复管理方式" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("PID 123");
  expect(repairs).toBe(0);
  await dialog.getByRole("button", { name: "取消" }).click();
  expect(repairs).toBe(0);
  previewAvailable = false;
  await card.getByRole("button", { name: "修复管理方式" }).click();
  await expect(dialog.getByRole("alert")).toContainText(
    "PID 文件与实际主进程不一致",
  );
  await expect(dialog.getByRole("button", { name: "确认修复" })).toBeDisabled();
  expect(repairs).toBe(0);
  await dialog.getByRole("button", { name: "取消" }).click();
  previewAvailable = true;
  await card.getByRole("button", { name: "修复管理方式" }).click();
  await dialog.getByRole("button", { name: "确认修复" }).click();
  await expect(dialog.getByRole("alert")).toContainText(
    "已恢复原配置和 PID 记录",
  );
  expect(repairs).toBe(1);
  failRepair = false;
  await dialog.getByRole("button", { name: "确认修复" }).click();
  await expect(dialog).toHaveCount(0);
  await expect(card.getByText("运行中", { exact: true })).toBeVisible();
  await expect(
    card.getByRole("button", { name: "重启", exact: true }),
  ).toBeEnabled();
  await expect(card.getByRole("button", { name: "修复管理方式" })).toHaveCount(
    0,
  );
  expect(repairs).toBe(2);
  expect(actions).toBe(0);
});
