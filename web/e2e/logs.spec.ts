import { test, expect, type Page } from "@playwright/test";

function accessLog(
  status: number,
  request: string,
  agent = "Test browser",
  client = "114.114.114.114",
) {
  return `${client} - - [27/Oct/2026:14:23:45 +0800] "GET ${request} HTTP/1.1" ${status} 123 "-" "${agent}"`;
}

async function chooseOption(page: Page, label: string, option: string) {
  await page.getByRole("combobox", { name: label, exact: true }).click();
  await page.getByRole("option", { name: option, exact: true }).click();
}

test("access log table filters loaded rows, combines rules and resets per file", async ({
  page,
}, testInfo) => {
  const site = "/home/wwwlogs/site.log";
  const managed = "/usr/local/nginx/logs/gopanel/other.log";
  const php = "/usr/local/php/var/log/php_errors.log";
  const mysql = "/var/log/mysql/mariadb-slow.log";
  const rows = [
    {
      offset: "0",
      text: accessLog(200, "/ok?query=404"),
      partial: true,
      ip_location: "中国江苏省南京市 电信",
    },
    {
      offset: "1000",
      text: accessLog(302, "/redirect", "browser", "2001:db8::1").split(
        ' "-" "',
      )[0],
      ip_location: "暂不支持",
    },
    { offset: "2000", text: accessLog(404, "/missing") },
    { offset: "3000", text: accessLog(404, "/secret") },
    {
      offset: "4000",
      text: accessLog(
        500,
        "/failed",
        "<script>window.logInjected=true</script>",
      ),
      truncated: true,
    },
    { offset: "5000", text: "unrecognized access log" },
  ];
  let reads = 0;
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/api/v1/logs", (route) =>
    route.fulfill({
      json: {
        files: [site, managed, php, mysql].map((path) => ({
          path,
          available: true,
          size: 6000,
          modified: "2026-10-27T06:23:45Z",
        })),
        warnings: [],
      },
    }),
  );
  await page.route("**/api/v1/logs/read", (route) => {
    reads++;
    const req = route.request().postDataJSON();
    const after = req.mode === "after";
    const lines = after
      ? []
      : rows.filter(
          (row) =>
            !req.rules.some(
              (rule: { enabled: boolean; action: string; pattern: string }) =>
                rule.enabled &&
                rule.action === "hide" &&
                row.text.includes(rule.pattern),
            ),
        );
    return route.fulfill({
      json: {
        lines,
        from: after ? "6000" : "0",
        to: "6000",
        replace_from: after ? "6000" : "0",
        start: after ? "c6000" : "c0",
        end: "c6000",
        size: 6000,
        modified: "2026-10-27T06:23:45Z",
        has_before: false,
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
  await expect(page.locator(".log-line")).toHaveCount(6);
  await page.getByRole("button", { name: "暂停追踪" }).click();
  const initialReads = reads;
  const format = page.getByRole("checkbox", { name: "格式化为表格" });
  const follow = page.getByRole("checkbox", { name: "自动滚动" });
  const help = page.getByRole("button", { name: "IP 属地说明", exact: true });
  const explanation =
    "IP 地址位置数据由 纯真 CZ88 提供 · 数据版本 20261021 · IPv6 暂不支持";
  const followBounds = (await follow.boundingBox())!;
  const formatBounds = (await format.boundingBox())!;
  expect(formatBounds.y).toBeCloseTo(followBounds.y, 0);
  expect(formatBounds.x).toBeGreaterThan(followBounds.x + followBounds.width);
  await expect(page.getByRole("tooltip")).toHaveCount(0);
  if (testInfo.project.name === "mobile") await help.tap();
  else await help.hover();
  await expect(page.getByRole("tooltip")).toContainText(explanation);
  await expect(format).not.toBeChecked();
  await expect(follow).toBeChecked();
  await page.screenshot({
    path: testInfo.outputPath("log-location-tooltip.png"),
    fullPage: true,
  });
  await page.keyboard.press("Escape");
  await expect(page.getByRole("tooltip")).toHaveCount(0);
  await follow.focus();
  await help.focus();
  await expect(page.getByRole("tooltip")).toContainText(explanation);
  await page.keyboard.press("Escape");
  await expect(page.getByRole("tooltip")).toHaveCount(0);
  await help.click();
  await expect(page.getByRole("tooltip")).toContainText(explanation);
  await page.locator(".log-file-meta").click();
  await expect(page.getByRole("tooltip")).toHaveCount(0);
  const table = page.getByRole("table", { name: "Nginx 访问日志" });
  await expect(format).not.toBeChecked();
  await expect(page.getByRole("group", { name: "HTTP 状态码" })).toHaveCount(0);
  await format.focus();
  await page.keyboard.press("Space");
  await expect(table).toBeVisible();
  await expect(table.getByRole("columnheader")).toHaveCount(9);
  await expect(table.getByRole("columnheader")).toHaveText([
    "字节偏移",
    "客户端 IP",
    "IP 属地",
    "时间",
    "请求",
    "HTTP 状态码",
    "响应字节数",
    "Referer",
    "User-Agent",
  ]);
  await expect(
    table.getByRole("columnheader", { name: "原文", exact: true }),
  ).toHaveCount(0);
  await expect(table.getByRole("button", { name: /原文/ })).toHaveCount(0);
  await expect(
    table.locator('[data-offset="0"]').getByRole("cell").nth(2),
  ).toHaveText("中国江苏省南京市 电信");
  await expect(
    table.locator('[data-offset="1000"]').getByRole("cell").nth(2),
  ).toHaveText("暂不支持");
  await expect(
    table.locator('[data-offset="2000"]').getByRole("cell").nth(2),
  ).toHaveText("未知");
  await expect(table.getByText("[无法解析]", { exact: true })).toBeVisible();
  await expect(table.locator('[data-offset="0"]')).toContainText("尚未换行");
  await expect(table.locator('[data-offset="4000"]')).toContainText("已截断");
  await expect(table.locator('[data-offset="4000"]')).toContainText(
    "<script>window.logInjected=true</script>",
  );
  expect(
    await page.evaluate(
      () => (window as Window & { logInjected?: boolean }).logInjected,
    ),
  ).toBeUndefined();

  const status = (name: string) =>
    page.getByRole("checkbox", { name, exact: true });
  await status("404").click();
  await expect(page.locator(".log-access-row")).toHaveCount(2);
  await expect(page.getByText(/显示 2 \/ 6 行/)).toBeVisible();
  await status("5xx").click();
  await expect(page.locator(".log-access-row")).toHaveCount(3);
  await status("4xx").click();
  await expect(page.locator(".log-access-row")).toHaveCount(3);
  await page.getByRole("button", { name: "清除筛选" }).click();
  await status("1xx").click();
  await expect(page.getByText("当前范围没有可显示的日志行")).toBeVisible();
  await page.getByRole("button", { name: "清除筛选" }).click();
  expect(reads).toBe(initialReads);

  await status("4xx").click();
  await page.getByRole("button", { name: /^筛选规则/ }).click();
  await page.getByRole("button", { name: "添加规则" }).click();
  await page.getByLabel("规则 1 内容").fill("secret");
  await chooseOption(page, "规则 1 操作", "隐藏匹配");
  await page.getByRole("button", { name: "应用规则" }).click();
  await expect(page.locator(".log-access-row")).toHaveCount(1);
  await expect(page.locator(".log-access-row")).toContainText("/missing");
  await expect(page.getByText(/显示 1 \/ 5 行/)).toBeVisible();
  await format.click();
  await expect(table).toHaveCount(0);
  await expect(page.locator(".log-line")).toHaveCount(5);
  await format.click();
  await expect(status("4xx")).not.toBeChecked();
  await expect(page.locator(".log-access-row")).toHaveCount(5);
  await page.screenshot({
    path: testInfo.outputPath("access-log-table.png"),
    fullPage: true,
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);

  await chooseOption(page, "选择日志", managed);
  await expect(format).not.toBeChecked();
  await format.click();
  await status("404").click();
  await chooseOption(page, "选择日志", site);
  await expect(format).not.toBeChecked();
  await format.click();
  await expect(status("404")).not.toBeChecked();
  for (const path of [php, mysql]) {
    await chooseOption(page, "选择日志", path);
    await expect(format).toHaveCount(0);
    await expect(help).toHaveCount(0);
    await expect(table).toHaveCount(0);
  }
  expect(errors).toEqual([]);
});

test("access table virtualizes history, follows matching replacements and recovers rotation", async ({
  page,
}, testInfo) => {
  const path = "/home/wwwlogs/access.log";
  const modified = "2026-10-27T06:23:45Z";
  const long = "long request 中文".repeat(45);
  const longLocation = "中国测试省测试市测试区 运营商 ".repeat(12);
  let appended = false,
    appendNext = false,
    rotateNext = false,
    rotated = false;
  const base = Array.from({ length: 1200 }, (_, i) => ({
    offset: String(100000 + i * 1000),
    text:
      i === 1199
        ? accessLog(500, "/incomplete").slice(0, -1)
        : accessLog(
            i % 2 ? 404 : 200,
            `/entry-${i}`,
            i % 3 === 0 ? long : "browser",
          ),
    partial: i === 1199,
  }));
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/api/v1/logs", (route) =>
    route.fulfill({
      json: {
        files: [{ path, available: true, size: 1300000, modified }],
        warnings: [],
      },
    }),
  );
  await page.route("**/api/v1/logs/read", (route) => {
    const req = route.request().postDataJSON();
    if (rotateNext && req.mode === "after") {
      rotateNext = false;
      rotated = true;
      return route.fulfill({ status: 409, json: { error: "日志已轮转" } });
    }
    if (rotated)
      return route.fulfill({
        json: {
          lines:
            req.mode === "after"
              ? []
              : [
                  {
                    offset: "0",
                    text: accessLog(503, "/rotated"),
                    ip_location: "轮转属地",
                  },
                ],
          from: req.mode === "after" ? "1000" : "0",
          to: "1000",
          replace_from: req.mode === "after" ? "1000" : "0",
          start: req.mode === "after" ? "c1000" : "c0",
          end: "c1000",
          size: 1000,
          modified,
          has_before: false,
          has_after: false,
          limited: false,
        },
      });
    const before = req.mode === "before",
      after = req.mode === "after";
    const replacing = after && appendNext;
    if (replacing) {
      appended = true;
      appendNext = false;
    }
    const end = appended ? 1301000 : 1300000;
    const from = before ? 50000 : after ? Number(req.end.slice(1)) : 100000;
    const replacements = [
      { offset: "1299000", text: accessLog(500, "/completed") },
      { offset: "1300000", text: accessLog(502, "/latest", long) },
    ];
    const lines = before
      ? Array.from({ length: 50 }, (_, i) => ({
          offset: String(50000 + i * 1000),
          text: accessLog(404, `/history-${i}`, long),
        }))
      : after
        ? replacing
          ? replacements
          : []
        : appended
          ? [...base.slice(0, -1), ...replacements]
          : base;
    return route.fulfill({
      json: {
        lines: lines.map((line) => ({
          ...line,
          ip_location: before ? "历史属地" : longLocation,
        })),
        from: String(from),
        to: String(before ? 100000 : end),
        replace_from: String(replacing ? 1299000 : from),
        start: `c${from}`,
        end: `c${before ? 100000 : end}`,
        size: end,
        modified,
        has_before: !before,
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
  await expect(page.locator(".log-line").first()).toBeVisible();
  await page.getByRole("checkbox", { name: "格式化为表格" }).click();
  await page.getByRole("checkbox", { name: "4xx", exact: true }).click();
  const viewport = page.locator(".log-viewport");
  const follow = page.getByRole("checkbox", { name: "自动滚动" });
  const atBottom = () =>
    viewport.evaluate(
      (node) => node.scrollHeight - node.scrollTop - node.clientHeight,
    );
  await expect.poll(atBottom).toBeLessThan(2);
  expect(await page.locator(".log-access-row").count()).toBeLessThan(80);
  await viewport.evaluate((node) => {
    node.scrollTop = node.scrollHeight - node.clientHeight - 160;
  });
  await expect(follow).not.toBeChecked();
  const anchor = await viewport.evaluate((node) => {
    const top = node.getBoundingClientRect().top + node.clientTop;
    const row = [...node.querySelectorAll<HTMLElement>(".log-access-row")].find(
      (row) => row.getBoundingClientRect().bottom > top,
    )!;
    return {
      offset: row.dataset.offset!,
      y: row.getBoundingClientRect().top - top,
    };
  });
  const headerPosition = () =>
    page
      .locator(".log-access-header")
      .evaluate(
        (node) =>
          node.getBoundingClientRect().top -
          node.closest(".log-table-scroll")!.getBoundingClientRect().top,
      );
  await page.getByRole("button", { name: "加载更早日志" }).click();
  await expect(page.getByText(/显示 649 \/ 1250 行/)).toBeVisible();
  await expect
    .poll(() =>
      viewport.evaluate((node, anchor) => {
        const row = node.querySelector<HTMLElement>(
          `[data-offset="${anchor.offset}"]`,
        );
        return row
          ? Math.abs(
              row.getBoundingClientRect().top -
                node.getBoundingClientRect().top -
                node.clientTop -
                anchor.y,
            )
          : Infinity;
      }, anchor),
    )
    .toBeLessThan(2);
  await expect.poll(headerPosition).toBe(1);
  await viewport.evaluate((node) => {
    node.scrollTop = 0;
  });
  await expect(page.locator('[data-offset="50000"]')).toContainText("历史属地");
  await expect
    .poll(() =>
      page
        .locator(".log-access-row")
        .evaluateAll((nodes) =>
          nodes.every(
            (node, i) =>
              i === 0 ||
              node.getBoundingClientRect().top >=
                nodes[i - 1].getBoundingClientRect().bottom - 1,
          ),
        ),
    )
    .toBe(true);

  // Header and rows share their columns before and after horizontal scrolling.
  await page.locator(".log-table-scroll").evaluate((node) => {
    node.scrollLeft = 300;
  });
  const columnDifference = () =>
    page.getByRole("table").evaluate((node) => {
      const header = node.querySelectorAll('[role="columnheader"]');
      const cells = node
        .querySelector(".log-access-row")!
        .querySelectorAll('[role="cell"]');
      return Math.max(
        ...[...header].map((cell, i) =>
          Math.abs(
            cell.getBoundingClientRect().left -
              cells[i].getBoundingClientRect().left,
          ),
        ),
      );
    });
  await expect.poll(columnDifference).toBeLessThan(2);
  const originalSize = page.viewportSize()!;
  await page.setViewportSize({
    width: originalSize.width + 220,
    height: originalSize.height,
  });
  await expect.poll(columnDifference).toBeLessThan(2);
  await page.setViewportSize(originalSize);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("access-log-history.png"),
    fullPage: true,
  });

  await page.getByRole("button", { name: "清除筛选" }).click();
  await page.getByRole("checkbox", { name: "5xx", exact: true }).click();
  await expect(page.getByText("当前范围没有可显示的日志行")).toBeVisible();
  await page.getByRole("button", { name: "回到最新" }).click();
  appendNext = true;
  await expect(page.getByText(/显示 2 \/ 1201 行/)).toBeVisible();
  await expect(page.locator('[data-offset="1300000"]')).toContainText(
    "/latest",
  );
  await expect(
    page.locator('[data-offset="1300000"]').getByRole("cell").nth(2),
  ).toHaveText(longLocation);
  await expect(follow).toBeChecked();
  await expect.poll(atBottom).toBeLessThan(2);
  // A tall final row may virtualize its predecessor out on a small screen.
  await viewport.evaluate((node) => {
    node.scrollTop = 0;
  });
  await expect(page.locator('[data-offset="1299000"]')).toContainText(
    "/completed",
  );
  await follow.check();
  await expect.poll(atBottom).toBeLessThan(2);
  await page.getByRole("checkbox", { name: "502", exact: true }).click();
  rotateNext = true;
  await expect(
    page.getByText("日志已轮转或截断，当前已切换至新文件的最新内容。"),
  ).toBeVisible();
  await expect(page.locator(".log-access-row")).toHaveCount(1);
  await expect(page.locator(".log-access-row")).toContainText("/rotated");
  await expect(page.locator(".log-access-row")).toContainText("轮转属地");
  await expect(
    page.getByRole("checkbox", { name: "502", exact: true }),
  ).toBeChecked();
  await expect(page.getByText(/显示 1 \/ 1 行/)).toBeVisible();
  expect(errors).toEqual([]);
});

for (const theme of ["light", "dark"] as const) {
  test(`custom log controls handle long paths, keyboard selection and empty lists in ${theme} theme`, async ({
    page,
  }, testInfo) => {
    await page.addInitScript(
      (theme) => localStorage.setItem("theme", theme),
      theme,
    );
    const longPath = `/home/wwwlogs/${"long-directory-name/".repeat(10)}site.log`;
    let paths = [
      longPath,
      ...Array.from(
        { length: 20 },
        (_, i) => `/home/wwwlogs/site-${String(i).padStart(2, "0")}.log`,
      ),
    ];
    await page.route("**/api/v1/logs", (route) =>
      route.fulfill({
        json: {
          files: paths.map((path) => ({ path, available: false, size: 0 })),
          warnings: [],
        },
      }),
    );
    await page.goto("/logs");
    await page
      .getByLabel("密码", { exact: true })
      .fill("Test-Only-Panel-Password-2026");
    await page.getByRole("button", { name: "登录面板" }).click();
    const select = page.getByRole("combobox", { name: "选择日志" });
    await expect(select).toContainText(longPath);
    await select.focus();
    await page.keyboard.press("Enter");
    const menu = page.getByRole("listbox");
    await expect(menu).toBeVisible();
    const bounds = (await menu.boundingBox())!;
    const viewport = page.viewportSize()!;
    expect(bounds.x).toBeGreaterThanOrEqual(0);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(viewport.width);
    expect(bounds.y).toBeGreaterThanOrEqual(0);
    expect(bounds.y + bounds.height).toBeLessThanOrEqual(viewport.height);
    await page.screenshot({
      path: testInfo.outputPath(`log-select-${theme}.png`),
    });
    await page.keyboard.press("End");
    await expect(
      page.getByRole("option", {
        name: "/home/wwwlogs/site-19.log（不可读取）",
        exact: true,
      }),
    ).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(select).toContainText("site-19.log");
    await expect(select).toBeFocused();
    // A selected file disappearing must remain identifiable and selectable.
    paths = [longPath];
    await page.getByRole("button", { name: "刷新文件" }).click();
    await expect(select).toContainText("文件已不在列表中");
    await chooseOption(page, "选择日志", `${longPath}（不可读取）`);
    await page.getByRole("button", { name: /^筛选规则/ }).click();
    await page.getByRole("button", { name: "添加规则" }).click();
    await page.getByRole("combobox", { name: "规则 1 类型" }).click();
    await expect(
      page.getByRole("option", { name: "正则", exact: true }),
    ).toBeVisible();
    await page.screenshot({
      path: testInfo.outputPath(`log-controls-${theme}.png`),
    });
    await page.keyboard.press("Escape");
    await expect(
      page.getByRole("dialog", { name: "显示与隐藏规则" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "关闭筛选规则" }).click();
    paths = [];
    await page.getByRole("button", { name: "刷新文件" }).click();
    await expect(select).toBeDisabled();
    await expect(select).toHaveText("没有可读取的日志");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBeTruthy();
  });
}

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
  ).toHaveCount(0);
  await expect(page.getByText("LOG VIEWER", { exact: true })).toHaveCount(0);
  await expect(
    page.getByText("分批浏览历史日志，实时跟踪新增内容。", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByText("实时追踪 · 约每秒更新", { exact: true }),
  ).toHaveCount(0);
  await expect(page.locator(".log-line").first()).toBeVisible();
  expect(await page.locator(".log-line").count()).toBeLessThan(80);
  const drawer = page.getByRole("dialog", { name: "显示与隐藏规则" });
  const filters = page.getByRole("button", { name: /^筛选规则/ });
  await expect(filters).toHaveCSS("font-size", "14px");
  const logCard = page.locator('[data-slot="card"]').filter({
    has: page
      .locator('[data-slot="card-title"]')
      .filter({ hasText: "日志内容" }),
  });
  await expect(logCard.getByLabel("选择日志")).toBeVisible();
  const follow = page.getByRole("checkbox", { name: "自动滚动" });
  const meta = await page.locator(".log-file-meta").boundingBox();
  const followLabel = await page
    .locator(".log-status .log-check")
    .boundingBox();
  expect(followLabel!.x).toBeGreaterThanOrEqual(meta!.x + meta!.width);
  expect(
    Math.abs(
      followLabel!.y + followLabel!.height / 2 - meta!.y - meta!.height / 2,
    ),
  ).toBeLessThan(2);
  await expect(follow).toBeChecked();
  await page.getByText("自动滚动", { exact: true }).click();
  await expect(follow).not.toBeChecked();
  await follow.focus();
  await page.keyboard.press("Space");
  await expect(follow).toBeChecked();
  await expect(drawer).not.toBeVisible();
  await filters.click();
  await expect(drawer).toBeVisible();
  await expect(
    drawer.getByRole("button", { name: "关闭", exact: true }),
  ).toHaveCSS("font-size", "14px");
  const bounds = await drawer.boundingBox();
  expect(bounds!.y).toBe(0);
  await expect
    .poll(async () => {
      const rect = await drawer.boundingBox();
      return Math.abs(rect!.x + rect!.width - page.viewportSize()!.width);
    })
    .toBeLessThan(2);
  await page.getByRole("button", { name: "添加规则" }).click();
  const kind = page.getByRole("combobox", { name: "规则 1 类型" });
  await kind.click();
  await expect(page.getByRole("listbox")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("listbox")).toHaveCount(0);
  await expect(drawer).toBeVisible();
  await expect(kind).toBeFocused();
  const enabled = page.getByRole("checkbox", { name: "启用规则 1" });
  await drawer.getByText("启用", { exact: true }).click();
  await expect(enabled).not.toBeChecked();
  await enabled.focus();
  await page.keyboard.press("Space");
  await expect(enabled).toBeChecked();
  await drawer.getByText("忽略大小写", { exact: true }).click();
  await expect(
    page.getByRole("checkbox", { name: "忽略大小写" }),
  ).toBeChecked();
  await page.getByLabel("规则 1 内容").fill("ERROR");
  await page.keyboard.press("Escape");
  await expect(drawer).not.toBeVisible();
  await expect(filters).toBeFocused();
  await expect(filters).toContainText("未应用");
  await filters.click();
  await expect(page.getByLabel("规则 1 内容")).toHaveValue("ERROR");
  await page.getByRole("button", { name: "添加规则" }).click();
  await page.getByLabel("规则 2 内容").fill("secret");
  await chooseOption(page, "规则 2 操作", "隐藏匹配");
  await page.screenshot({
    path: testInfo.outputPath("log-rules-drawer.png"),
  });
  await page.getByRole("button", { name: "应用规则" }).click();
  await expect(drawer).not.toBeVisible();
  await expect(filters).not.toContainText("未应用");
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
  await filters.click();
  await chooseOption(page, "规则 1 类型", "正则");
  await page.getByLabel("规则 1 内容").fill("[");
  await page.getByRole("button", { name: "应用规则" }).click();
  await expect(page.getByRole("alert")).toContainText("正则无效");
  await expect(drawer).toBeVisible();
  await expect(page.locator(".log-line").first()).toContainText(
    "ERROR visible",
  );
  await page.reload();
  await filters.click();
  await expect(page.getByLabel("规则 1 内容")).toHaveValue("ERROR");
  await expect(page.getByLabel("规则 1 类型")).toHaveText("关键词");
  await expect(
    page.getByRole("checkbox", { name: "忽略大小写" }).first(),
  ).toBeChecked();
  await page.getByRole("button", { name: "关闭筛选规则" }).click();
  await chooseOption(page, "选择日志", php);
  await filters.click();
  await expect(page.getByLabel("规则 1 内容")).toHaveCount(0);
  await page.getByRole("button", { name: "关闭筛选规则" }).click();
  await chooseOption(page, "选择日志", path);
  await filters.click();
  await expect(page.getByLabel("规则 1 内容")).toHaveValue("ERROR");
  await page.getByRole("button", { name: "关闭筛选规则" }).click();
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
  await expect(page.locator(".log-reader-status")).toBeEmpty();
  await expect(page.getByText("读取中…", { exact: true })).toHaveCount(0);
  await chooseOption(page, "选择日志", php);
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

test("wrapped virtual rows preserve history anchors, resize and follow appended content", async ({
  page,
}, testInfo) => {
  const path = "/home/wwwlogs/wrapped.log";
  const modified = "2026-10-26T12:00:00Z";
  const longText =
    "  中文堆栈与无空格日志".repeat(35) +
    "\n    at requestHandler (server.go:42)";
  let append = false,
    end = 1300000;
  await page.route("**/api/v1/logs", (route) =>
    route.fulfill({
      json: {
        files: [{ path, size: end, modified, available: true }],
        warnings: [],
      },
    }),
  );
  await page.route("**/api/v1/logs/read", (route) => {
    const req = route.request().postDataJSON();
    const before = req.mode === "before",
      after = req.mode === "after";
    const replace = after && append;
    const from = before ? 50000 : after ? end : 100000;
    let lines;
    if (before)
      lines = Array.from({ length: 50 }, (_, i) => ({
        offset: String(50000 + i * 1000),
        text: `HISTORY ${i} ${longText}`,
      }));
    else if (!after)
      lines = Array.from({ length: 1200 }, (_, i) => ({
        offset: String(100000 + i * 1000),
        text: `ENTRY ${i} ${i % 3 === 0 || i === 1199 ? longText : "short log"}`,
        partial: i === 1199,
      }));
    else if (replace) {
      lines = [
        { offset: "1299000", text: `UPDATED ${longText.repeat(2)}` },
        { offset: "1300000", text: `LATEST ${longText}` },
      ];
      end = 1301000;
      append = false;
    } else lines = [];
    return route.fulfill({
      json: {
        lines,
        from: String(from),
        to: String(before ? 100000 : end),
        replace_from: String(replace ? 1299000 : from),
        start: `c${from}`,
        end: `c${before ? 100000 : end}`,
        size: end,
        modified,
        has_before: !before,
        has_after: false,
        limited: false,
      },
    });
  });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/logs");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  const viewport = page.locator(".log-viewport");
  const follow = page.getByRole("checkbox", { name: "自动滚动" });
  const atBottom = () =>
    viewport.evaluate(
      (node) => node.scrollHeight - node.scrollTop - node.clientHeight,
    );
  await expect(page.locator('.log-line[data-offset="1299000"]')).toContainText(
    "ENTRY 1199",
  );
  await expect.poll(atBottom).toBeLessThan(2);
  await expect(follow).toBeChecked();
  expect(await page.locator(".log-line").count()).toBeLessThan(80);
  await expect
    .poll(() =>
      viewport.evaluate((node) => node.scrollWidth <= node.clientWidth),
    )
    .toBe(true);
  const assertNoOverlap = async () => {
    await expect
      .poll(() =>
        page
          .locator(".log-line")
          .evaluateAll((nodes) =>
            nodes.every(
              (node, i) =>
                i === 0 ||
                node.getBoundingClientRect().top >=
                  nodes[i - 1].getBoundingClientRect().bottom - 1,
            ),
          ),
      )
      .toBe(true);
  };
  await assertNoOverlap();
  await page.screenshot({
    path: testInfo.outputPath("wrapped-logs.png"),
    fullPage: true,
  });
  // Scrolling into a wrapped row turns follow off and preserves the source offset.
  await viewport.evaluate((node) => {
    node.scrollTop = node.scrollHeight - node.clientHeight - 180;
  });
  await expect(follow).not.toBeChecked();
  const anchor = await viewport.evaluate((node) => {
    const top = node.getBoundingClientRect().top + node.clientTop;
    const row = [...node.querySelectorAll<HTMLElement>(".log-line")].find(
      (row) => row.getBoundingClientRect().bottom > top,
    )!;
    return {
      offset: row.dataset.offset!,
      y: row.getBoundingClientRect().top - top,
    };
  });
  await page.getByRole("button", { name: "加载更早日志" }).click();
  await expect(
    page.getByRole("button", { name: "加载更早日志" }),
  ).toBeDisabled();
  await expect
    .poll(() =>
      viewport.evaluate((node, anchor) => {
        const row = node.querySelector<HTMLElement>(
          `[data-offset="${anchor.offset}"]`,
        );
        if (!row) return Infinity;
        return Math.abs(
          row.getBoundingClientRect().top -
            node.getBoundingClientRect().top -
            node.clientTop -
            anchor.y,
        );
      }, anchor),
    )
    .toBeLessThan(2);
  const originalSize = page.viewportSize()!;
  await page.setViewportSize({
    width: originalSize.width + 220,
    height: originalSize.height,
  });
  await assertNoOverlap();
  await expect
    .poll(() =>
      viewport.evaluate((node) => node.scrollWidth <= node.clientWidth),
    )
    .toBe(true);
  await page.setViewportSize(originalSize);
  await page.getByRole("button", { name: "回到最新" }).click();
  await expect(follow).toBeChecked();
  append = true;
  await expect(page.locator('.log-line[data-offset="1300000"]')).toContainText(
    "LATEST",
  );
  await expect.poll(atBottom).toBeLessThan(2);
  await expect(follow).toBeChecked();
  await assertNoOverlap();
  await viewport.evaluate((node) => {
    node.scrollTop = node.scrollHeight - node.clientHeight - 300;
  });
  await expect(follow).not.toBeChecked();
  await expect(page.locator('.log-line[data-offset="1299000"]')).toContainText(
    "UPDATED",
  );
  expect(errors).toEqual([]);
});
