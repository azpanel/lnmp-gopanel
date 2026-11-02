import { test, expect, type Page } from "./fixtures";
import { newPolicy, type PolicySummary } from "../src/lib/backup-policies";
import type { BackupRun } from "../src/lib/backup-runs";

async function openBackupExecution(page: Page) {
  await page.goto("/backups");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "网站备份", exact: true }),
  ).toBeVisible();
}

async function executionFixture(page: Page) {
  const policyId = "a".repeat(32),
    runId = "b".repeat(32);
  const draft = newPolicy();
  const policy: PolicySummary = {
    ...draft,
    id: policyId,
    revision: 7,
    name: "每日网站备份",
    enabled: false,
    destinations: [{ type: "local", path: "/backups/site" }],
    database_enabled: true,
    database_mode: "all",
    database_count: 0,
    file_count: 1,
  };
  let run: BackupRun | undefined;
  let submissions = 0,
    reads = 0,
    rejection = false;
  await page.route("**/api/v1/backups/components", (route) =>
    route.fulfill({ json: { items: [] } }),
  );
  await page.route("**/api/v1/backups/runtime", (route) =>
    route.fulfill({
      json: {
        scheduler: true,
        dependencies: [
          { name: "7zip", ready: false },
          { name: "dump", ready: true },
        ],
      },
    }),
  );
  await page.route("**/api/v1/backups/policies", (route) =>
    route.fulfill({ json: [{ ...policy, last_run: run }] }),
  );
  await page.route(`**/api/v1/backups/policies/${policyId}/run`, (route) => {
    expect(route.request().postDataJSON()).toEqual({ revision: 7 });
    submissions++;
    if (rejection)
      return route.fulfill({
        status: 409,
        json: { error: "该政策已有备份正在排队或执行，请等待完成" },
      });
    run = {
      id: runId,
      policy_id: policyId,
      policy_name: policy.name,
      revision: 7,
      trigger: "manual",
      status: "running",
      phase: "files",
      created_at: "2026-11-02T02:00:00+08:00",
      updated_at: "2026-11-02T02:00:00+08:00",
      started_at: "2026-11-02T02:00:00+08:00",
      categories: [],
    };
    return route.fulfill({ status: 202, json: run });
  });
  await page.route(`**/api/v1/backups/policies/${policyId}/runs?*`, (route) =>
    route.fulfill({
      json: {
        items: run ? [run] : [],
        total: run ? 1 : 0,
        page: 1,
        page_size: 20,
      },
    }),
  );
  await page.route(`**/api/v1/backups/runs/${runId}`, (route) => {
    reads++;
    return route.fulfill({ json: run });
  });
  return {
    get submissions() {
      return submissions;
    },
    get reads() {
      return reads;
    },
    reject() {
      rejection = true;
    },
    complete() {
      if (!run) throw new Error("No execution to complete");
      run = {
        ...run,
        status: "partial",
        phase: "finished",
        finished_at: "2026-11-02T02:01:00+08:00",
        categories: [
          {
            category: "files",
            status: "partial",
            bytes: 2048,
            name: "site-files.zip",
            sha256: "c".repeat(64),
            deliveries: [
              {
                destination: { type: "local", path: "/backups/site" },
                status: "succeeded",
                object: "/backups/site/site-files.zip",
                cleanup_error: "旧备份已被修改，未删除",
              },
              {
                destination: { type: "rclone", remote: "cloud", path: "site" },
                status: "failed",
                error: "远程网络不可用",
              },
            ],
          },
        ],
      };
    },
  };
}

test("manual backup restores running state after refresh and shows partial delivery history", async ({
  page,
}) => {
  const fixture = await executionFixture(page);
  await openBackupExecution(page);
  await expect(page.getByLabel("备份运行环境")).toContainText(
    "7-Zip：未安装或权限不安全",
  );
  await page.getByRole("button", { name: "立即备份", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText(
    "执行历史 · 每日网站备份",
  );
  await expect(
    page.getByRole("heading", { name: "执行详情 · 备份中" }),
  ).toBeVisible();
  expect(fixture.submissions).toBe(1);
  await page.reload();
  await expect(
    page.getByRole("button", { name: "立即备份", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "执行历史", exact: true }).click();
  fixture.complete();
  await expect(
    page.getByRole("heading", { name: "执行详情 · 部分成功" }),
  ).toBeVisible({ timeout: 12000 });
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("远程网络不可用");
  await expect(dialog).toContainText("旧版本清理：旧备份已被修改，未删除");
  await expect(dialog).toContainText("政策版本 7");
  await expect(dialog).toContainText("SHA-256");
  const reads = fixture.reads;
  await page.waitForTimeout(3500);
  expect(fixture.reads).toBe(reads);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("manual backup surfaces duplicate execution conflict without submitting twice", async ({
  page,
}) => {
  const fixture = await executionFixture(page);
  fixture.reject();
  await openBackupExecution(page);
  await page.getByRole("button", { name: "立即备份", exact: true }).click();
  await expect(
    page.getByText("该政策已有备份正在排队或执行，请等待完成", { exact: true }),
  ).toBeVisible();
  expect(fixture.submissions).toBe(1);
  await expect(page.getByRole("dialog")).toHaveCount(0);
});
