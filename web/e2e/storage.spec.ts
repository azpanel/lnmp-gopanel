import { test, expect } from "@playwright/test";

test("storage mounts, asynchronous file ranking, treemap and retry", async ({
  page,
}, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const mountedAt = new Date().toISOString();
  const mounts = [
    {
      path: "/",
      device: "/dev/vda3",
      fs_type: "ext4",
      options: ["rw"],
      total: 50 * 1024 ** 3,
      used: 12 * 1024 ** 3,
      free: 38 * 1024 ** 3,
      percent: 24,
    },
    {
      path: "/proc",
      device: "proc",
      fs_type: "proc",
      options: ["rw"],
      total: 0,
      used: 0,
      free: 0,
      percent: 0,
    },
    {
      path: "/tmp",
      device: "tmpfs",
      fs_type: "tmpfs",
      options: ["rw"],
      total: 1024 ** 3,
      used: 1024,
      free: 1024 ** 3 - 1024,
      percent: 0.01,
    },
    {
      path: "/unreadable",
      device: "/dev/missing",
      fs_type: "ext4",
      options: ["rw"],
      total: null,
      used: null,
      free: null,
      percent: null,
      error: "容量暂不可读取",
    },
    {
      path: "/empty",
      device: "/dev/vdb1",
      fs_type: "ext4",
      options: ["rw"],
      total: 1024 ** 3,
      used: 0,
      free: 1024 ** 3,
      percent: 0,
    },
  ];
  const files = [
    { path: "/var/lib/mysql/database with spaces.ibd", size: 4 * 1024 ** 3 },
    { path: "/home/wwwlogs/access.log", size: 2 * 1024 ** 3 },
    { path: "/root/backup archive.tar.gz", size: 1024 ** 3 },
  ];
  let starts = 0,
    polls = 0;
  const base = {
    root: "/",
    started_at: mountedAt,
    finished_at: null,
    message: "正在扫描主文件系统…",
    files: [],
    scanned: 0,
    total_bytes: 0,
    complete: false,
    partial: false,
  };
  await page.route("**/api/v1/storage/mounts", (route) =>
    route.fulfill({ json: { timestamp: mountedAt, mounts } }),
  );
  await page.route("**/api/v1/storage/scan", (route) => {
    if (route.request().method() === "POST") {
      starts++;
      polls = 0;
      return route.fulfill({
        status: 202,
        json: { ...base, status: "running" },
      });
    }
    if (!starts) return route.fulfill({ json: { ...base, status: "idle" } });
    if (++polls <= 2)
      return route.fulfill({
        json: { ...base, status: "running", scanned: polls * 1000 },
      });
    if (starts > 1)
      return route.fulfill({
        json: { ...base, status: "failed", message: "测试扫描失败，请重试" },
      });
    return route.fulfill({
      json: {
        ...base,
        status: "completed",
        files,
        scanned: 8000,
        total_bytes: 9 * 1024 ** 3,
        complete: true,
        finished_at: new Date().toISOString(),
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
  await page.getByRole("link", { name: "硬盘空间", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "硬盘空间", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("正在扫描文件，完成后自动更新")).toBeVisible();
  await expect(page.getByText("/proc", { exact: true })).toBeVisible();
  await expect(page.getByText("不提供容量", { exact: true })).toBeVisible();
  await expect(page.getByText("容量暂不可读取", { exact: true })).toBeVisible();
  const capacitySwitch = page.getByRole("switch", { name: "隐藏无容量挂载" });
  await expect(capacitySwitch).not.toBeChecked();
  await capacitySwitch.click();
  await expect(capacitySwitch).toBeChecked();
  await expect(page.locator(".mounts-table tbody tr")).toHaveCount(3);
  await expect(page.getByText("/proc", { exact: true })).not.toBeVisible();
  await expect(
    page.getByText("/unreadable", { exact: true }),
  ).not.toBeVisible();
  await expect(page.getByText("/empty", { exact: true })).toBeVisible();
  await page.getByRole("textbox", { name: "筛选挂载" }).fill("proc");
  await expect(page.getByText("没有匹配的挂载", { exact: true })).toBeVisible();
  await capacitySwitch.focus();
  await page.keyboard.press("Space");
  await expect(capacitySwitch).not.toBeChecked();
  await expect(page.getByText("/proc", { exact: true })).toBeVisible();
  await capacitySwitch.click();
  await page.getByRole("textbox", { name: "筛选挂载" }).fill("tmpfs");
  await expect(page.locator(".mounts-table tbody tr")).toHaveCount(1);
  await page.getByRole("textbox", { name: "筛选挂载" }).fill("");
  await expect(page.locator(".mounts-table tbody tr")).toHaveCount(3);
  await capacitySwitch.click();
  await expect(page.locator(".mounts-table tbody tr")).toHaveCount(5);
  await expect(page.getByText("扫描完成", { exact: true })).toBeVisible({
    timeout: 12000,
  });
  await expect(page.getByTestId("file-treemap")).toBeVisible();
  const tile = page.getByRole("button", { name: /database with spaces.ibd/ });
  await tile.click();
  await expect(page.locator(".treemap-selection")).toContainText(
    "database with spaces.ibd",
  );
  await expect(page.locator(".file-ranking-table tbody tr")).toHaveCount(3);
  await expect(
    page.locator(".file-ranking-table tbody tr").first(),
  ).toContainText("57.14%");
  expect(starts).toBe(1);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: testInfo.outputPath("storage.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "重新扫描", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("测试扫描失败，请重试", {
    timeout: 12000,
  });
  await expect(
    page.getByRole("button", { name: "重新扫描", exact: true }),
  ).toBeEnabled();
  expect(errors).toEqual([]);
});
