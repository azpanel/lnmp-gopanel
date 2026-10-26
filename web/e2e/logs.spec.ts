import { test, expect } from "@playwright/test";

test("log filters, bounded viewport, history, rotation and per-file preferences", async ({
  page,
}, testInfo) => {
  const path = "/home/wwwlogs/site.log",
    php = "/usr/local/php/var/log/php_errors.log";
  let appended = false,
    rotated = false,
    reads = 0;
  const file = (path: string) => ({
    path,
    size: 500000,
    modified: "2026-10-26T12:00:00Z",
    available: true,
  });
  await page.route("**/api/v1/logs", (route) =>
    route.fulfill({ json: { files: [file(path), file(php)], warnings: [] } }),
  );
  await page.route("**/api/v1/logs/read", async (route) => {
    reads++;
    const req = route.request().postDataJSON();
    if (req.rules.some((r: { pattern: string }) => r.pattern === "[")) {
      await route.fulfill({ status: 400, json: { error: "第 1 条正则无效" } });
      return;
    }
    if (rotated && req.mode === "after") {
      rotated = false;
      await route.fulfill({ status: 409, json: { error: "日志已轮转或截断" } });
      return;
    }
    const before = req.mode === "before",
      after = req.mode === "after";
    const from = before ? 0 : after ? Number(req.end.slice(1)) : 300000;
    const to = before ? 300000 : after ? (appended ? from + 20 : from) : 500000;
    let lines = after
      ? appended
        ? [{ offset: String(from), text: "ERROR appended" }]
        : []
      : Array.from({ length: before ? 20 : 3000 }, (_, i) => ({
          offset: String(from + i * 50),
          text:
            i % 3 === 0
              ? `ERROR secret ${i}`
              : i % 3 === 1
                ? `ERROR visible ${i}`
                : `INFO message ${i}`,
        }));
    if (after) appended = false;
    const enabled = req.rules.filter((r: { enabled: boolean }) => r.enabled);
    lines = lines.filter((line) => {
      const matches = (r: { pattern: string; ignore_case: boolean }) =>
        r.ignore_case
          ? line.text.toLowerCase().includes(r.pattern.toLowerCase())
          : line.text.includes(r.pattern);
      const shows = enabled.filter(
        (r: { action: string }) => r.action === "show",
      );
      return (
        (!shows.length || shows.some(matches)) &&
        !enabled.some(
          (r: { action: string; pattern: string; ignore_case: boolean }) =>
            r.action === "hide" && matches(r),
        )
      );
    });
    await route.fulfill({
      json: {
        lines,
        from: String(from),
        to: String(to),
        replace_from: String(from),
        start: `c${from}`,
        end: `c${to}`,
        size: 500000,
        modified: "2026-10-26T12:00:00Z",
        has_before: from > 0,
        has_after: false,
        limited: false,
      },
    });
  });
  await page.goto("/logs");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "日志读取", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".log-line").first()).toBeVisible();
  expect(await page.locator(".log-line").count()).toBeLessThan(80);
  await page.getByRole("button", { name: "添加规则" }).click();
  await page.getByLabel("规则 1 内容").fill("ERROR");
  await page.getByRole("button", { name: "添加规则" }).click();
  await page.getByLabel("规则 2 内容").fill("secret");
  await page.getByLabel("规则 2 操作").selectOption("hide");
  await page.getByRole("button", { name: "应用规则" }).click();
  await expect(page.getByRole("button", { name: "应用规则" })).toBeDisabled();
  await expect(page.locator(".log-line").first()).toContainText(
    "ERROR visible",
  );
  expect(await page.locator(".log-line").allTextContents()).toEqual(
    expect.arrayContaining([expect.stringContaining("ERROR visible")]),
  );
  expect(
    (await page.locator(".log-line").allTextContents()).every(
      (text) => !text.includes("secret") && !text.includes("INFO"),
    ),
  ).toBeTruthy();
  // Invalid regex leaves the previous result and preferences intact.
  await page.getByLabel("规则 1 类型").selectOption("regex");
  await page.getByLabel("规则 1 内容").fill("[");
  await page.getByRole("button", { name: "应用规则" }).click();
  await expect(page.getByRole("alert")).toContainText("正则无效");
  await expect(page.locator(".log-line").first()).toContainText(
    "ERROR visible",
  );
  await page.reload();
  await expect(page.getByLabel("规则 1 内容")).toHaveValue("ERROR");
  await expect(page.getByLabel("规则 1 类型")).toHaveValue("keyword");
  await page.getByLabel("选择日志").selectOption(php);
  await expect(page.getByLabel("规则 1 内容")).toHaveCount(0);
  await page.getByLabel("选择日志").selectOption(path);
  await expect(page.getByLabel("规则 1 内容")).toHaveValue("ERROR");
  await page.getByRole("button", { name: "加载更早日志" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "已暂停追踪" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "加载更早日志" }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "回到最新" }).click();
  await expect(page.getByRole("button", { name: "暂停追踪" })).toBeEnabled();
  await page.getByRole("button", { name: "暂停追踪" }).click();
  const pausedReads = reads;
  await page.waitForTimeout(1300);
  expect(reads).toBe(pausedReads);
  appended = true;
  await page.getByRole("button", { name: "继续追踪" }).click();
  await expect(
    page.locator(".log-line").filter({ hasText: "ERROR appended" }),
  ).toBeVisible();
  rotated = true;
  await expect(
    page.getByText("日志已轮转或截断，当前已切换至新文件的最新内容。"),
  ).toBeVisible();
  await expect(page.locator(".log-line").first()).toContainText(
    "ERROR visible",
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
  await page.screenshot({
    path: testInfo.outputPath("logs.png"),
    fullPage: true,
  });
});

test("log polling retries, stops in background and cancels stale file reads", async ({
  page,
}) => {
  const site = "/home/wwwlogs/site.log",
    php = "/usr/local/php/var/log/php_errors.log";
  let reads = 0,
    fail = true,
    holdNext = false,
    release: (() => void) | undefined;
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/api/v1/logs", (route) =>
    route.fulfill({
      json: {
        files: [site, php].map((path) => ({
          path,
          size: 10,
          modified: "2026-10-26T12:00:00Z",
          available: true,
        })),
        warnings: [],
      },
    }),
  );
  await page.route("**/api/v1/logs/read", async (route) => {
    reads++;
    const req = route.request().postDataJSON();
    if (fail) {
      fail = false;
      await route.fulfill({ status: 503, json: { error: "测试读取失败" } });
      return;
    }
    if (holdNext && req.path === site) {
      holdNext = false;
      await new Promise<void>((resolve) => {
        release = resolve;
      });
    }
    const after = req.mode === "after";
    await route
      .fulfill({
        json: {
          lines: after
            ? []
            : [
                {
                  offset: "0",
                  text: req.path === site ? "site content" : "php content",
                },
              ],
          from: after ? "10" : "0",
          to: "10",
          replace_from: after ? "10" : "0",
          start: after ? "c10" : "c0",
          end: "c10",
          size: 10,
          modified: "2026-10-26T12:00:00Z",
          has_before: false,
          has_after: false,
          limited: false,
        },
      })
      .catch(() => {});
  });
  await page.goto("/logs");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(page.getByRole("alert")).toContainText("测试读取失败");
  await expect(page.locator(".log-line")).toContainText("site content");
  await page.evaluate(() =>
    Object.defineProperty(document, "hidden", {
      configurable: true,
      get: () => true,
    }),
  );
  const hiddenReads = reads;
  await page.waitForTimeout(1300);
  expect(reads).toBe(hiddenReads);
  holdNext = true;
  await page.evaluate(() =>
    Object.defineProperty(document, "hidden", {
      configurable: true,
      get: () => false,
    }),
  );
  await expect.poll(() => Boolean(release)).toBe(true);
  await page.getByLabel("选择日志").selectOption(php);
  await expect(page.locator(".log-line")).toContainText("php content");
  release!();
  await page.waitForTimeout(200);
  await expect(page.locator(".log-line")).toContainText("php content");
  await page.goto("/system");
  await expect(
    page.getByRole("heading", { name: "系统信息", exact: true }),
  ).toBeVisible();
  const leftReads = reads;
  await page.waitForTimeout(1300);
  expect(reads).toBe(leftReads);
  expect(errors).toEqual([]);
});
