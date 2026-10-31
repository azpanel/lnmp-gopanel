import { test, expect, type Page } from "./fixtures";

async function openBackups(page: Page) {
  await page.goto("/backups");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "网站备份", exact: true }),
  ).toBeVisible();
}

test("backup batch installation restores progress and shows actual versions", async ({
  page,
}, testInfo) => {
  let complete = false;
  let job: Record<string, unknown> | undefined;
  let submissions = 0;
  await page.route("**/api/v1/backups/components", (route) =>
    route.fulfill({
      json: {
        items: ["gdrive", "rclone"].map((name) => ({
          name,
          status: complete ? "ready" : "missing",
          supported: true,
          version: complete ? (name === "gdrive" ? "3.9.1" : "1.73.2") : "",
        })),
        job,
      },
    }),
  );
  await page.route("**/api/v1/backups/components/install", (route) => {
    expect(route.request().postDataJSON()).toEqual({
      components: ["gdrive", "rclone"],
    });
    submissions++;
    job = {
      id: "test-job",
      status: "running",
      items: [
        { name: "gdrive", status: "running" },
        { name: "rclone", status: "pending" },
      ],
    };
    return route.fulfill({ status: 202, json: job });
  });
  await openBackups(page);
  if (testInfo.project.name === "mobile")
    await page.getByRole("button", { name: "打开导航" }).click();
  await expect(
    page.getByRole("link", { name: "网站备份", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "网站备份", exact: true }).click();
  await page.getByRole("button", { name: "安装组件", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("button", { name: "开始安装" })).toBeDisabled();
  await dialog.getByRole("checkbox", { name: "安装 gdrive" }).check();
  await dialog.getByRole("checkbox", { name: "安装 rclone" }).check();
  await dialog.getByRole("button", { name: "开始安装" }).click();
  await expect(dialog.getByRole("button", { name: "安装中…" })).toBeDisabled();
  await expect(
    dialog.getByRole("checkbox", { name: "安装 gdrive" }),
  ).toBeDisabled();
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "网站备份", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "安装组件", exact: true }).click();
  await expect(dialog.getByText("等待安装", { exact: true })).toBeVisible();
  complete = true;
  job = {
    ...job,
    status: "succeeded",
    items: [
      { name: "gdrive", status: "succeeded", version: "3.9.1" },
      { name: "rclone", status: "succeeded", version: "1.73.2" },
    ],
  };
  await expect(
    dialog.getByText("安装成功 · 3.9.1", { exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByText("安装成功 · 1.73.2", { exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("checkbox", { name: "安装 rclone" }),
  ).toBeDisabled();
  await page.screenshot({
    path: testInfo.outputPath("backup-install-dialog.png"),
    fullPage: true,
  });
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(page.getByText("已安装 · 3.9.1", { exact: true })).toBeVisible();
  await expect(
    page.getByText("已安装 · 1.73.2", { exact: true }),
  ).toBeVisible();
  expect(submissions).toBe(1);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
});

test("backup component architecture restrictions and failed installation retry", async ({
  page,
}) => {
  let job: Record<string, unknown> | undefined;
  let submissions = 0;
  await page.route("**/api/v1/backups/components", (route) =>
    route.fulfill({
      json: {
        items: [
          {
            name: "gdrive",
            status: "missing",
            supported: false,
            version: "",
            error: "当前架构没有受支持的安装包",
          },
          { name: "rclone", status: "missing", supported: true, version: "" },
        ],
        job,
      },
    }),
  );
  await page.route("**/api/v1/backups/components/install", (route) => {
    expect(route.request().postDataJSON()).toEqual({ components: ["rclone"] });
    submissions++;
    job = {
      id: "retry-job",
      status: "failed",
      items: [
        { name: "rclone", status: "failed", error: "组件下载失败（HTTP 503）" },
      ],
    };
    return route.fulfill({ status: 202, json: job });
  });
  await openBackups(page);
  await page.getByRole("button", { name: "安装组件", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("checkbox", { name: "安装 gdrive" }),
  ).toBeDisabled();
  await expect(dialog.getByText("当前架构没有受支持的安装包")).toBeVisible();
  await dialog.getByRole("checkbox", { name: "安装 rclone" }).check();
  await dialog.getByRole("button", { name: "开始安装" }).click();
  await expect(dialog.getByText("组件下载失败（HTTP 503）")).toBeVisible();
  await dialog.getByRole("checkbox", { name: "安装 rclone" }).check();
  await dialog.getByRole("button", { name: "开始安装" }).click();
  await expect.poll(() => submissions).toBe(2);
});
