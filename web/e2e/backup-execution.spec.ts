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
    rejection = false,
    detailFailure = false;
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
    if (detailFailure)
      return route.fulfill({
        status: 500,
        json: { error: "进度服务暂不可用" },
      });
    return route.fulfill({ json: run });
  });
  return {
    get current() {
      return run!;
    },
    failDetails(value: boolean) {
      detailFailure = value;
    },
    progress(
      percent?: number,
      kind: "prepare" | "compress" | "verify" | "deliver" = "compress",
    ) {
      if (!run) throw new Error("No execution to update");
      const progress =
        percent == null
          ? undefined
          : {
              total_bytes: kind === "prepare" ? undefined : 1073741824,
              processed_bytes: (1073741824 * percent) / 100,
              percent: kind === "prepare" ? undefined : percent,
              eta_seconds: kind === "prepare" ? undefined : 75,
              updated_at: new Date().toISOString(),
              estimated: kind === "compress" || kind === "verify",
            };
      run = {
        ...run,
        phase: kind === "deliver" ? "delivery" : "files",
        steps: [
          { id: "queued", kind: "queued", status: "succeeded" },
          { id: "preflight", kind: "preflight", status: "succeeded" },
          {
            id: "files-prepare",
            kind: "prepare",
            category: "files",
            status: kind === "prepare" ? "running" : "succeeded",
            progress: kind === "prepare" ? progress : undefined,
          },
          {
            id: "files-compress",
            kind: "compress",
            category: "files",
            status:
              kind === "prepare"
                ? "pending"
                : kind === "compress"
                  ? "running"
                  : "succeeded",
            progress:
              kind === "prepare"
                ? undefined
                : kind === "compress"
                  ? progress
                  : {
                      ...progress!,
                      percent: 100,
                      processed_bytes: 1073741824,
                      estimated: true,
                    },
          },
          {
            id: "files-verify",
            kind: "verify",
            category: "files",
            status: ["prepare", "compress"].includes(kind)
              ? "pending"
              : kind === "verify"
                ? "running"
                : "succeeded",
            progress: kind === "verify" ? progress : undefined,
          },
          {
            id: "files-deliver-0",
            kind: "deliver",
            category: "files",
            destination: { type: "local", path: "/backups/site" },
            status: kind === "deliver" ? "running" : "pending",
            progress: kind === "deliver" ? progress : undefined,
          },
          {
            id: "files-cleanup-0",
            kind: "cleanup",
            category: "files",
            destination: { type: "local", path: "/backups/site" },
            status: "pending",
          },
          { id: "finished", kind: "finished", status: "pending" },
        ],
      };
    },
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
        steps: run.steps?.map((step) => ({
          ...step,
          status:
            step.kind === "finished"
              ? "partial"
              : step.kind === "cleanup"
                ? "failed"
                : "succeeded",
          error: step.kind === "cleanup" ? "旧备份已被修改，未删除" : undefined,
          progress: step.progress
            ? {
                ...step.progress,
                percent: 100,
                processed_bytes: step.progress.total_bytes,
                eta_seconds: undefined,
              }
            : undefined,
        })),
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
  await page.getByRole("button", { name: "备份运行环境", exact: true }).click();
  const runtime = page.getByRole("dialog", {
    name: "备份运行环境",
    exact: true,
  });
  await expect(runtime.getByRole("region", { name: "依赖工具" })).toContainText(
    "7-Zip",
  );
  await expect(runtime.getByRole("region", { name: "依赖工具" })).toContainText(
    "未安装或程序权限不安全",
  );
  await runtime.getByRole("button", { name: "关闭", exact: true }).click();
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

test("backup runtime loads on demand, refreshes and restores trigger focus", async ({
  page,
}, testInfo) => {
  await executionFixture(page);
  let reads = 0;
  let release!: () => void;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/v1/backups/runtime", async (route) => {
    reads++;
    if (reads === 1) await pending;
    return route.fulfill({
      json: {
        scheduler: reads > 1,
        scheduler_error: "受管 Cron 文件缺失",
        dependencies: [
          { name: "7zip", ready: true },
          { name: "dump", ready: false, error: "数据库导出程序权限不安全" },
          { name: "gdrive", ready: false },
          { name: "rclone", ready: true },
        ],
      },
    });
  });
  await openBackupExecution(page);
  const refresh = page.getByRole("button", { name: "刷新政策", exact: true });
  await expect(refresh).toHaveText("");
  await expect(refresh.locator("svg")).toHaveCount(1);
  const heading = page.locator(".policy-list-header");
  await expect(heading.locator('[data-slot="card-title"]')).toHaveText(
    "备份政策",
  );
  await expect(
    heading.getByRole("button", { name: "刷新政策", exact: true }),
  ).toBeVisible();
  expect(
    await heading.evaluate((el) => getComputedStyle(el).justifyContent),
  ).toBe("space-between");
  const refreshed = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/v1/backups/policies",
  );
  await refresh.click();
  expect((await refreshed).ok()).toBe(true);
  const trigger = page.getByRole("button", {
    name: "备份运行环境",
    exact: true,
  });
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(reads).toBe(0);
  await trigger.click();
  const dialog = page.getByRole("dialog", {
    name: "备份运行环境",
    exact: true,
  });
  await expect(dialog.getByRole("status")).toHaveText("正在检查调度与工具…");
  await expect(dialog.getByRole("button", { name: "重新检测" })).toBeDisabled();
  await expect.poll(() => reads).toBe(1);
  release();
  await expect(dialog.getByRole("region", { name: "定时调度" })).toContainText(
    "受管 Cron 文件缺失",
  );
  await expect(dialog.getByRole("region", { name: "依赖工具" })).toContainText(
    "数据库导出程序权限不安全",
  );
  const help = dialog.locator(".backup-runtime-help");
  const helpToggle = help.getByRole("button", {
    name: "使用说明",
    exact: true,
  });
  await expect(helpToggle).toHaveAttribute("aria-expanded", "false");
  await expect(help.locator("ul")).not.toBeVisible();
  const samples = await help.evaluate(async (element) => {
    const scroller = element.closest<HTMLElement>(".backup-runtime-scroll")!;
    const content = element.querySelector<HTMLElement>(
      ".backup-runtime-help-content",
    )!;
    const sample = () => ({
      height: content.getBoundingClientRect().height,
      width: scroller.clientWidth,
      scrollWidth: scroller.scrollWidth,
      overflow: scroller.scrollHeight > scroller.clientHeight + 1,
      horizontalOverflow: scroller.scrollWidth > scroller.clientWidth,
    });
    const frames = [sample()];
    element.querySelector<HTMLButtonElement>("button")!.click();
    const started = performance.now();
    await new Promise<void>((resolve) => {
      const next = () => {
        frames.push(sample());
        if (performance.now() - started < 400) requestAnimationFrame(next);
        else resolve();
      };
      requestAnimationFrame(next);
    });
    return frames;
  });
  await testInfo.attach("help-animation-frames", {
    body: JSON.stringify(samples),
    contentType: "application/json",
  });
  const expandedHeight = samples.at(-1)!.height;
  expect(expandedHeight).toBeGreaterThan(0);
  expect(
    samples.some(
      (frame) => frame.height > 1 && frame.height < expandedHeight - 1,
    ),
  ).toBe(true);
  expect(
    Math.max(...samples.map((frame) => frame.width)) -
      Math.min(...samples.map((frame) => frame.width)),
  ).toBeLessThanOrEqual(1);
  expect(samples.some((frame) => frame.horizontalOverflow)).toBe(false);
  if (!samples[0].overflow && !samples.at(-1)!.overflow)
    expect(samples.some((frame) => frame.overflow)).toBe(false);
  await expect(helpToggle).toHaveAttribute("aria-expanded", "true");
  await expect(help.locator("ul")).toBeVisible();
  await expect(help).toContainText("gdrive 和 rclone 仅在使用对应云存储时需要");
  expect(reads).toBe(1);
  await helpToggle.focus();
  await page.keyboard.press("Enter");
  await expect(help.locator("ul")).not.toBeVisible();
  await page.emulateMedia({ reducedMotion: "reduce" });
  await helpToggle.press("Space");
  await expect(help.locator("ul")).toBeVisible();
  await expect(help.locator(".backup-runtime-help-content")).toHaveCSS(
    "transition-duration",
    "0s",
  );
  await helpToggle.press("Space");
  await expect(help.locator("ul")).not.toBeVisible();
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await expect(dialog).toHaveCSS("background-color", "rgb(255, 255, 255)");
  expect(await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(
    true,
  );
  await page.screenshot({
    path: testInfo.outputPath("backup-runtime-dialog.png"),
    animations: "disabled",
  });
  await dialog.getByRole("button", { name: "重新检测" }).click();
  await expect(dialog.getByRole("region", { name: "定时调度" })).toContainText(
    "已就绪",
  );
  expect(reads).toBe(2);
  await helpToggle.click();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await trigger.click();
  await expect.poll(() => reads).toBe(3);
  await expect(helpToggle).toHaveAttribute("aria-expanded", "false");
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await trigger.click();
  await expect.poll(() => reads).toBe(4);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("backup runtime failures can be retried", async ({ page }) => {
  await executionFixture(page);
  let failed = true;
  await page.route("**/api/v1/backups/runtime", (route) =>
    route.fulfill(
      failed
        ? { status: 502, json: { error: "运行环境暂时不可用" } }
        : {
            json: {
              scheduler: true,
              dependencies: [{ name: "7zip", ready: true }],
            },
          },
    ),
  );
  await openBackupExecution(page);
  await page.getByRole("button", { name: "备份运行环境", exact: true }).click();
  const dialog = page.getByRole("dialog", {
    name: "备份运行环境",
    exact: true,
  });
  await expect(dialog.getByRole("alert")).toContainText("运行环境暂时不可用");
  failed = false;
  await dialog.getByRole("button", { name: "重新检测" }).click();
  await expect(dialog.getByRole("region", { name: "定时调度" })).toContainText(
    "已就绪",
  );
  await expect(dialog.getByRole("alert")).toHaveCount(0);
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

test("history timeline shows live byte progress, unknown values and refresh errors", async ({
  page,
}, testInfo) => {
  const fixture = await executionFixture(page);
  await openBackupExecution(page);
  await page.getByRole("button", { name: "立即备份", exact: true }).click();
  const dialog = page.getByRole("dialog");
  fixture.progress(undefined, "prepare");
  const preparation = dialog.getByRole("progressbar", {
    name: "准备备份文件",
  });
  await expect(preparation).toBeVisible();
  await expect(preparation).not.toHaveAttribute("aria-valuenow");
  fixture.progress(25, "prepare");
  await expect(dialog.locator('[aria-current="step"]')).toContainText(
    "已复制 256.0 MB / 总量 —",
  );
  await expect(preparation).not.toHaveAttribute("aria-valuenow");
  fixture.progress();
  const expectLatestCentered = async (finished = false) => {
    const step = finished
      ? dialog.locator(".backup-timeline-step").last()
      : dialog.locator('[aria-current="step"]');
    await expect
      .poll(() =>
        step.evaluate((element) => {
          const scroll = element.closest(".backup-history-scroll")!;
          const rect = element.getBoundingClientRect();
          const desired =
            scroll.scrollTop +
            rect.top -
            scroll.getBoundingClientRect().top -
            (scroll.clientHeight - rect.height) / 2;
          const reachable = Math.max(
            0,
            Math.min(desired, scroll.scrollHeight - scroll.clientHeight),
          );
          return Math.abs(scroll.scrollTop - reachable);
        }),
      )
      .toBeLessThan(2);
  };
  const compression = dialog.getByRole("progressbar", { name: "压缩文件" });
  await expect(compression).toBeVisible();
  await expectLatestCentered();
  // Polling must resume following progress even when the payload is unchanged.
  await dialog.locator(".backup-history-scroll").evaluate((element) => {
    element.scrollTop = 0;
  });
  await expectLatestCentered();
  await expect(compression).not.toHaveAttribute("aria-valuenow");
  await expect(dialog).toContainText("计算中");
  fixture.progress(25);
  await expect(compression).toHaveAttribute("aria-valuenow", "25");
  await expect(dialog).toContainText("已处理（估算）");
  await expect(dialog).toContainText("预计剩余（ETA）：1 分 15 秒");
  fixture.progress(70);
  await expect(compression).toHaveAttribute("aria-valuenow", "70");
  await expectLatestCentered();
  fixture.progress(30, "verify");
  const verification = dialog.getByRole("progressbar", {
    name: "校验文件备份",
  });
  await expect(verification).toHaveAttribute("aria-valuenow", "30");
  await expectLatestCentered();
  fixture.progress(40, "deliver");
  const delivery = dialog.getByRole("progressbar", { name: /写入本地/ });
  await expect(delivery).toHaveAttribute("aria-valuenow", "40");
  await expect(compression).toHaveAttribute("aria-valuenow", "100");
  await expectLatestCentered();
  await expect(dialog.getByRole("list", { name: "执行步骤" })).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("backup-history-timeline.png"),
    fullPage: true,
  });
  fixture.failDetails(true);
  await expect(dialog.getByRole("alert")).toContainText("进度更新失败", {
    timeout: 15000,
  });
  await expect(delivery).toHaveAttribute("aria-valuenow", "40");
  fixture.failDetails(false);
  fixture.progress(60, "deliver");
  await dialog.getByRole("button", { name: "重试", exact: true }).click();
  await expect(delivery).toHaveAttribute("aria-valuenow", "60");
  fixture.complete();
  await expect(
    dialog.getByRole("heading", { name: "执行详情 · 部分成功" }),
  ).toBeVisible();
  await expect(delivery).toHaveAttribute("aria-valuenow", "100");
  await expect(dialog.locator('[aria-current="step"]')).toHaveCount(0);
  await expectLatestCentered(true);
  const reads = fixture.reads;
  await page.waitForTimeout(1500);
  expect(fixture.reads).toBe(reads);
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await page.waitForTimeout(1200);
  expect(fixture.reads).toBe(reads);
});

test("history Select merges 100 records, preserves selection and stays beside refresh", async ({
  page,
}, testInfo) => {
  const fixture = await executionFixture(page);
  await openBackupExecution(page);
  await page.getByRole("button", { name: "立即备份", exact: true }).click();
  const records: BackupRun[] = Array.from({ length: 100 }, (_, index) => ({
    ...fixture.current,
    id: (index + 1).toString(16).padStart(32, "0"),
    status: "succeeded",
    phase: "finished",
    revision: index + 1,
    created_at: new Date(
      Date.UTC(2026, 10, 6, 12, 0, 0) - index * 60000,
    ).toISOString(),
  }));
  const pages = new Set<number>();
  await page.route("**/api/v1/backups/policies/*/runs?*", (route) => {
    const number = Number(
      new URL(route.request().url()).searchParams.get("page"),
    );
    pages.add(number);
    return route.fulfill({
      json: {
        items: records.slice((number - 1) * 20, number * 20),
        total: 100,
        page: number,
        page_size: 20,
      },
    });
  });
  await page.route("**/api/v1/backups/runs/*", (route) =>
    route.fulfill({
      json:
        records.find((run) => route.request().url().endsWith(run.id)) ??
        fixture.current,
    }),
  );
  const dialog = page.getByRole("dialog");
  const refresh = dialog.getByRole("button", { name: "刷新历史", exact: true });
  await refresh.click();
  await expect.poll(() => pages.size).toBe(5);
  const select = dialog.getByRole("combobox", { name: "执行任务" });
  const selectBox = await select.boundingBox(),
    refreshBox = await refresh.boundingBox();
  expect(selectBox).toBeTruthy();
  expect(refreshBox).toBeTruthy();
  expect(Math.abs(selectBox!.y - refreshBox!.y)).toBeLessThan(2);
  expect(refreshBox!.x).toBeGreaterThan(selectBox!.x);
  expect(await select.evaluate((el) => getComputedStyle(el).fontSize)).toBe(
    "14px",
  );
  await select.click();
  await expect(page.getByRole("option")).toHaveCount(100);
  expect(
    await page
      .getByRole("option")
      .first()
      .evaluate((el) => getComputedStyle(el).fontSize),
  ).toBe("14px");
  await page.keyboard.press("End");
  await expect(page.getByRole("option").last()).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(dialog).toContainText("政策版本 100");
  const selectedText = await select.textContent();
  await refresh.click();
  await expect(select).toHaveText(selectedText!);
  await expect(dialog).toContainText("政策版本 100");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await expect(dialog.getByRole("button", { name: "上一页" })).toHaveCount(0);
  await page.screenshot({
    path: testInfo.outputPath("backup-history-select.png"),
    fullPage: true,
  });
});

test("history polling keeps a selected run across page boundaries and failed refreshes", async ({
  page,
}) => {
  const fixture = await executionFixture(page);
  await openBackupExecution(page);
  await page.getByRole("button", { name: "立即备份", exact: true }).click();
  const records: BackupRun[] = Array.from({ length: 40 }, (_, index) => ({
    ...fixture.current,
    id: (index + 1).toString(16).padStart(32, "0"),
    status: index === 0 ? "running" : "succeeded",
    phase: index === 0 ? "files" : "finished",
    revision: index + 1,
    created_at: new Date(
      Date.UTC(2026, 10, 6, 12) - index * 60000,
    ).toISOString(),
  }));
  let holdOlder = false,
    failOlder = false,
    olderReads = 0;
  let releaseOlder!: () => void;
  const pendingOlder = new Promise<void>((resolve) => {
    releaseOlder = resolve;
  });
  await page.route("**/api/v1/backups/policies/*/runs?*", async (route) => {
    const number = Number(
      new URL(route.request().url()).searchParams.get("page"),
    );
    if (number > 1) {
      olderReads++;
      if (holdOlder) await pendingOlder;
      if (failOlder)
        return route.fulfill({
          status: 503,
          json: { error: "后续历史页暂不可用" },
        });
    }
    return route.fulfill({
      json: {
        items: records.slice((number - 1) * 20, number * 20),
        total: records.length,
        page: number,
        page_size: 20,
      },
    });
  });
  await page.route("**/api/v1/backups/runs/*", (route) =>
    route.fulfill({
      json:
        records.find((run) => route.request().url().endsWith(run.id)) ??
        fixture.current,
    }),
  );
  const dialog = page.getByRole("dialog");
  const refresh = dialog.getByRole("button", { name: "刷新历史", exact: true });
  const select = dialog.getByRole("combobox", { name: "执行任务" });
  await refresh.click();
  await select.click();
  await expect(page.getByRole("option")).toHaveCount(40);
  await page.getByRole("option").nth(19).click();
  await expect(dialog).toContainText("政策版本 20");
  const selectedText = await select.textContent();

  // A scheduled skip pushes the selected first-page boundary run onto page 2.
  // Delay that page to ensure the UI never publishes a partial refresh.
  holdOlder = true;
  records.splice(1, 0, {
    ...records[1],
    id: "f".repeat(32),
    status: "skipped",
    revision: 999,
    created_at: "2026-11-06T12:01:00Z",
  });
  records.pop();
  const before = olderReads;
  await expect.poll(() => olderReads).toBeGreaterThan(before);
  await expect(dialog).toContainText("政策版本 20");
  await expect(select).toHaveText(selectedText!);
  holdOlder = false;
  releaseOlder();
  await expect(dialog.getByRole("status")).toHaveCount(0);
  await expect(select).toHaveText(selectedText!);
  await expect(dialog).toContainText("政策版本 20");
  await select.click();
  await expect(page.getByRole("option")).toHaveCount(40);
  await expect(page.getByRole("option").first()).toContainText("已跳过");
  await page.keyboard.press("Escape");

  failOlder = true;
  await refresh.click();
  await expect(dialog.getByRole("alert")).toContainText("后续历史页暂不可用", {
    timeout: 15000,
  });
  await expect(select).toHaveText(selectedText!);
  await expect(dialog).toContainText("政策版本 20");
  failOlder = false;
  await expect(refresh).toBeEnabled();
  await refresh.click();
  await expect(dialog.getByRole("alert")).toHaveCount(0);
  await expect(select).toHaveText(selectedText!);
});
