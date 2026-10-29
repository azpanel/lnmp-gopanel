import { test, expect } from "@playwright/test";

test("login log reads real events without the system log helper", async ({
  page,
}, testInfo) => {
  const wrongPassword = `Wrong-Login-${testInfo.project.name}`;
  await page.goto("/logs");
  await page.getByLabel("密码", { exact: true }).fill(wrongPassword);
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByText("用户名或密码错误", { exact: true }),
  ).toBeVisible();
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();

  const select = page.getByRole("combobox", { name: "选择日志", exact: true });
  await expect(select).toContainText("登录日志");
  await expect(page.getByRole("button", { name: "移除日志" })).toHaveCount(0);
  await expect(
    page.locator(".log-line").filter({ hasText: wrongPassword }),
  ).toHaveCount(1);
  await expect(
    page.locator(".log-line").filter({ hasText: '"result":"success"' }).first(),
  ).toBeVisible();
  await expect(page.getByText(/日志辅助程序不可用/)).toBeVisible();
  const success = page
    .locator(".log-line")
    .filter({ hasText: '"result":"success"' });
  for (const text of await success.allTextContents()) {
    expect(text).not.toContain('"password":');
    expect(text).not.toContain("Test-Only-Panel-Password-2026");
  }
  await expect
    .poll(() =>
      page.evaluate(() =>
        (
          performance.getEntriesByName(
            "log-read",
            "measure",
          ) as PerformanceMeasure[]
        ).some(
          ({ detail }) =>
            detail.mode === "tail" &&
            detail.requestId &&
            detail.encodedBytes > 0 &&
            detail.decodedBytes > detail.encodedBytes &&
            detail.serverTiming.includes("read;dur="),
        ),
      ),
    )
    .toBe(true);

  const format = page.getByRole("checkbox", { name: "格式化为表格" });
  await expect(format).not.toBeChecked();
  await format.check();
  const table = page.getByRole("table", { name: "登录日志", exact: true });
  await expect(table).toBeVisible();
  await expect(table.getByRole("columnheader")).toHaveCount(10);
  await expect(page.getByRole("group", { name: "HTTP 状态码" })).toHaveCount(0);
  const failedRow = table
    .locator(".log-line")
    .filter({ hasText: wrongPassword });
  await expect(failedRow.getByRole("cell").nth(2)).toHaveText("127.0.0.1");
  await expect(failedRow.getByRole("cell").nth(3)).not.toBeEmpty();
  await expect(failedRow.getByRole("cell").nth(5)).toHaveText("失败");
  await expect(failedRow.getByRole("cell").nth(8)).toHaveText(wrongPassword);

  // A second client creates a real event while the reader is following.
  const extra = await page.request.post("/api/v1/auth/login", {
    data: { username: "admin", password: `${wrongPassword}-follow` },
  });
  expect(extra.status()).toBe(401);
  await expect(
    page.locator(".log-line").filter({ hasText: `${wrongPassword}-follow` }),
  ).toHaveCount(1);
  await page.getByRole("button", { name: "暂停追踪" }).click();
  await page.getByRole("button", { name: /^筛选规则/ }).click();
  await page.getByRole("button", { name: "添加规则" }).click();
  await page.getByLabel("规则 1 内容").fill(wrongPassword);
  await page.getByRole("button", { name: "应用规则" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator(".log-line")).toHaveCount(2);

  // Reload preserves file rules, while the file remains automatically listed.
  await page.reload();
  await expect(select).toContainText("登录日志");
  await expect(page.locator(".log-line")).toHaveCount(2);
  await expect(format).not.toBeChecked();
  await format.check();
  await expect(table).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("login-log.png"),
    fullPage: true,
  });
});

test("login table renders fields, fallback lines and custom file metadata", async ({
  page,
}, testInfo) => {
  const path = "/opt/custom-state/login.log";
  const nginxPath = "/usr/local/nginx/logs/gopanel/login.log";
  const time = "2026-10-29T04:00:00Z";
  const event = {
    time,
    ip: "114.114.114.114",
    ua: "Chrome/130.0 Safari/537.36",
    username: "admin",
    result: "success",
    stage: "password",
    reason: "authenticated",
  };
  const injection = "<script>window.loginLogInjected=true</script>";
  const rows = [
    {
      offset: "0",
      text: JSON.stringify(event),
      ip_location: "中国江苏省南京市 电信",
    },
    {
      offset: "1000",
      text: JSON.stringify({
        ...event,
        ip: "2001:db8::1",
        result: "failure",
        reason: "invalid_credentials",
        password: "",
      }),
      ip_location: "暂不支持",
    },
    {
      offset: "2000",
      text: JSON.stringify({
        ...event,
        ip: "invalid-ip",
        ua: "curl/8.10.1",
        username: injection,
        result: "failure",
        reason: "invalid_credentials",
        password: injection,
      }),
      ip_location: "未知",
    },
    {
      offset: "3000",
      text: JSON.stringify({
        ...event,
        result: "failure",
        stage: "totp",
        reason: "invalid_second_factor",
      }),
      partial: true,
    },
    { offset: "4000", text: "broken JSON", truncated: true },
    {
      offset: "5000",
      text: JSON.stringify({
        ...event,
        result: "future-result",
        stage: "future-stage",
        reason: "toString",
        username: "",
        ua: "",
      }),
    },
  ];
  await page.route("**/api/v1/logs", (route) =>
    route.fulfill({
      json: {
        files: [path, nginxPath].map((path) => ({
          path,
          name: "登录日志",
          format: "login",
          available: true,
          size: 6000,
          modified: time,
        })),
        warnings: [],
      },
    }),
  );
  await page.route("**/api/v1/logs/read", (route) => {
    const input = route.request().postDataJSON();
    const after = input.mode === "after";
    return route.fulfill({
      json: {
        lines: after ? [] : rows,
        from: after ? "6000" : "0",
        to: "6000",
        replace_from: after ? "6000" : "0",
        start: "start",
        end: "end",
        size: 6000,
        modified: time,
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
  const format = page.getByRole("checkbox", { name: "格式化为表格" });
  await expect(format).not.toBeChecked();
  await page.getByRole("button", { name: "暂停追踪" }).click();
  await format.check();
  const table = page.getByRole("table", { name: "登录日志", exact: true });
  await expect(table.getByRole("columnheader")).toHaveText([
    "字节偏移",
    "时间",
    "客户端 IP",
    "IP 属地",
    "用户名",
    "结果",
    "验证阶段",
    "原因",
    "失败密码",
    "User-Agent",
  ]);
  await page.getByRole("checkbox", { name: "自动滚动" }).uncheck();
  await table.locator(".log-viewport").evaluate((node) => {
    node.scrollTop = 0;
  });
  const row = (offset: string) =>
    table.locator(`.log-line[data-offset="${offset}"]`);
  const cell = (offset: string, column: number) =>
    row(offset).getByRole("cell").nth(column);
  await expect(cell("0", 1)).toHaveAttribute("title", time);
  await expect(cell("0", 3)).toHaveText("中国江苏省南京市 电信");
  await expect(cell("0", 5)).toHaveText("成功");
  await expect(cell("0", 6)).toHaveText("密码验证");
  await expect(cell("0", 7)).toHaveText("认证通过");
  await expect(cell("0", 8)).toHaveText("—");
  await expect(
    row("0").getByRole("img", { name: "Chrome", exact: true }),
  ).toHaveAttribute("src", "/brands/chrome.svg");
  await expect(cell("1000", 3)).toHaveText("暂不支持");
  await expect(cell("1000", 8)).toHaveText("（空密码）");
  await expect(cell("2000", 3)).toHaveText("未知");
  await expect(cell("2000", 8)).toHaveText(injection);
  await expect(
    row("2000").getByRole("img", { name: "curl", exact: true }),
  ).toHaveAttribute("src", "/brands/curl.svg");
  await expect(cell("3000", 6)).toHaveText("二步验证");
  await expect(cell("3000", 7)).toContainText("二步验证失败或挑战已失效");
  await expect(row("3000")).toContainText("尚未换行");
  await expect(row("4000")).toContainText("[无法解析] broken JSON");
  await expect(row("4000")).toContainText("已截断");
  await expect(cell("5000", 4)).toHaveText("—");
  await expect(cell("5000", 5)).toHaveText("future-result");
  await expect(cell("5000", 6)).toHaveText("future-stage");
  await expect(cell("5000", 7)).toHaveText("toString");
  await expect(page.getByRole("group", { name: "HTTP 状态码" })).toHaveCount(0);
  expect(
    await page.evaluate(
      () =>
        (window as Window & { loginLogInjected?: boolean }).loginLogInjected,
    ),
  ).toBeUndefined();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  const horizontal = page.getByLabel("登录日志表格横向滚动");
  expect(
    await horizontal.evaluate((node) => node.scrollWidth > node.clientWidth),
  ).toBe(true);
  await horizontal.evaluate((node) => {
    node.scrollLeft = node.scrollWidth;
  });
  await page.screenshot({
    path: testInfo.outputPath("login-table.png"),
    fullPage: true,
  });
  await format.uncheck();
  await expect(row("0")).toHaveCount(0);
  await expect(page.locator('.log-line[data-offset="0"] .log-text')).toHaveText(
    rows[0].text,
  );
  await format.check();
  await page.getByRole("combobox", { name: "选择日志", exact: true }).click();
  await page
    .getByRole("option", { name: `登录日志（${nginxPath}）`, exact: true })
    .click();
  await expect(format).not.toBeChecked();
  await format.check();
  await expect(table).toBeVisible();
  await expect(page.getByRole("table", { name: "Nginx 访问日志" })).toHaveCount(
    0,
  );
  await expect(page.getByRole("group", { name: "HTTP 状态码" })).toHaveCount(0);
});

test("login table virtualizes history and follows appended rows", async ({
  page,
}) => {
  const path = "/opt/custom-state/login.log";
  const modified = "2026-10-29T04:00:00Z";
  let appended = false;
  let history = false;
  const rows = (start: number, end: number) =>
    Array.from({ length: end - start }, (_, index) => {
      const value = start + index;
      return {
        offset: String(value * 1000),
        ip_location: "中国江苏省南京市 电信",
        text: JSON.stringify({
          time: modified,
          ip: "114.114.114.114",
          ua: "curl/8.10.1",
          username: `user-${value}`,
          result: "success",
          stage: "password",
          reason: "authenticated",
        }),
      };
    });
  await page.route("**/api/v1/logs", (route) =>
    route.fulfill({
      json: {
        files: [
          {
            path,
            name: "登录日志",
            format: "login",
            available: true,
            size: 400000,
            modified,
          },
        ],
        warnings: [],
      },
    }),
  );
  await page.route("**/api/v1/logs/read", (route) => {
    const input = route.request().postDataJSON();
    const before = input.mode === "before";
    if (before) history = true;
    const size = appended ? 401000 : 400000;
    const from = before
      ? 0
      : input.mode === "tail"
        ? 200000
        : Number(input.end.slice(1));
    const to = before ? 200000 : size;
    return route.fulfill({
      json: {
        lines: rows(from / 1000, to / 1000),
        from: String(from),
        to: String(to),
        replace_from: String(from),
        start: `c${from}`,
        end: `c${to}`,
        size,
        modified,
        has_before: from > 0,
        has_after: to < size,
        limited: false,
      },
    });
  });
  await page.goto("/logs");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await page.getByRole("checkbox", { name: "格式化为表格" }).check();
  const table = page.getByRole("table", { name: "登录日志", exact: true });
  await expect(table.locator('.log-line[data-offset="399000"]')).toContainText(
    "user-399",
  );
  expect(await table.locator(".log-line").count()).toBeLessThan(80);
  appended = true;
  await expect(table.locator('.log-line[data-offset="400000"]')).toContainText(
    "user-400",
  );
  await page.getByRole("button", { name: "加载更早日志" }).click();
  await expect.poll(() => history).toBe(true);
  await expect(table).toHaveAttribute("aria-rowcount", "402");
  await table.locator(".log-viewport").evaluate((node) => {
    node.scrollTop = 0;
  });
  await expect(table.locator('.log-line[data-offset="0"]')).toContainText(
    "user-0",
  );
  expect(await table.locator(".log-line").count()).toBeLessThan(80);
  await expect(page.getByRole("button", { name: "继续追踪" })).toBeVisible();
});
