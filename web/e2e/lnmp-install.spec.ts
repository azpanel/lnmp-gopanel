import { test, expect } from "@playwright/test";

test("installation defaults, module choices, restart recovery and polling output", async ({
  page,
}, testInfo) => {
  const id = "11111111-1111-4111-8111-111111111111";
  const revision = "a".repeat(40);
  let job: Record<string, unknown> | undefined;
  let submission: Record<string, unknown> | undefined;
  const logQueries: string[] = [];
  const streamCursors: string[] = [];
  let latest = 100;
  let failNextTail = false;
  const wrappedOutput = [
    "  https://example.com/source/" +
      "very-long-source-archive-name".repeat(80),
    "\t--add-module=/root/modules/" + "long-build-argument".repeat(100),
    "中文编译输出".repeat(120),
    ...Array.from({ length: 60 }, (_, i) => `    compiling source_${i}.c`),
  ].join("\n");
  const options = {
    revision,
    architecture: "amd64",
    ipv4: "192.0.2.1",
    default_database: 11,
    default_php: 14,
    default_allocator: 1,
    databases: [
      { id: 0, name: "不安装数据库" },
      { id: 11, name: "MySQL 8.4 LTS" },
    ],
    php: [
      { id: 14, name: "PHP 8.3.36" },
      { id: 15, name: "PHP 8.4.27" },
    ],
    modules: [
      {
        id: "vts",
        name: "VTS 流量统计",
        repository: "https://github.com/vozlt/nginx-module-vts.git",
      },
      {
        id: "geoip2",
        name: "GeoIP2 地理信息",
        repository: "https://github.com/leev/ngx_http_geoip2_module.git",
      },
      {
        id: "fancyindex",
        name: "FancyIndex 目录索引",
        repository: "https://github.com/aperezdc/ngx-fancyindex.git",
      },
      {
        id: "substitutions_filter",
        name: "Substitutions Filter 内容替换",
        repository:
          "https://github.com/yaoweibin/ngx_http_substitutions_filter_module.git",
      },
    ],
  };
  await page.route("**/api/v1/lnmp-installation", (route) =>
    route.fulfill({
      json: { installed: job?.status === "succeeded", blocked: [], job },
    }),
  );
  await page.route("**/api/v1/lnmp-installation/prepare", (route) => {
    job = {
      id,
      status: "ready",
      phase: "等待安装选项",
      created_at: new Date(Date.now() - 7_200_000).toISOString(),
      updated_at: new Date(Date.now() - 3_600_000).toISOString(),
      options,
    };
    return route.fulfill({ status: 202, json: job });
  });
  await page.route(`**/api/v1/lnmp-installation/jobs/${id}/start`, (route) => {
    submission = route.request().postDataJSON();
    const startedAt = new Date().toISOString();
    job = {
      ...job,
      status: "running",
      phase: "Nginx 编译",
      started_at: startedAt,
      updated_at: startedAt,
    };
    return route.fulfill({ status: 202, json: job });
  });
  await page.route(
    `**/api/v1/lnmp-installation/jobs/${id}/events*`,
    (route) => {
      streamCursors.push(
        new URL(route.request().url()).searchParams.get("after") || "0",
      );
      return route.abort();
    },
  );
  await page.route(`**/api/v1/lnmp-installation/jobs/${id}/logs*`, (route) => {
    const url = new URL(route.request().url());
    logQueries.push(url.search);
    const tail = url.searchParams.get("tail") === "1";
    const after = Number(url.searchParams.get("after") || "0");
    if (tail && failNextTail) {
      failNextTail = false;
      return route.fulfill({
        status: 503,
        json: { error: "temporary log read failure" },
      });
    }
    if (!tail && after === 0) {
      return route.fulfill({
        json: {
          lines: [{ id: 50, text: "old compilation output\n" + wrappedOutput }],
          cursor: 50,
          more: true,
        },
      });
    }
    return route.fulfill({
      json: {
        lines:
          tail || after < latest
            ? [
                {
                  id: latest,
                  time: new Date().toISOString(),
                  phase: "编译",
                  text:
                    (latest === 100
                      ? "make: compiling nginx with selected modules"
                      : "make: latest compilation progress") +
                    "\n" +
                    wrappedOutput,
                },
              ]
            : [],
        cursor: latest,
        more: false,
      },
    });
  });
  await page.goto("/services");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await page.getByRole("link", { name: "安装 LNMP" }).click();
  await expect(page.getByRole("heading", { name: "安装配置" })).toBeVisible();
  await page.getByRole("button", { name: "获取源码与安装选项" }).click();
  await expect(page.getByLabel("安装耗时", { exact: true })).toHaveText(
    "尚未开始安装",
  );
  await expect(page.getByRole("combobox")).toHaveCount(5);
  for (const label of [
    "数据库",
    "数据库安装方式",
    "PHP 版本",
    "内存分配器",
    "phpMyAdmin 保护",
  ]) {
    await expect(
      page.getByRole("combobox", { name: label, exact: true }),
    ).toHaveCSS("font-size", "14px");
  }
  const php = page.getByRole("combobox", { name: "PHP 版本", exact: true });
  await php.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("option", { name: "PHP 8.3.36" })).toHaveCSS(
    "font-size",
    "14px",
  );
  await expect(
    page.getByRole("option", { name: "PHP 8.3.36", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("End");
  await expect(
    page.getByRole("option", { name: "PHP 8.4.27", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(php).toContainText("PHP 8.4.27");
  await page
    .getByRole("combobox", { name: "phpMyAdmin 保护", exact: true })
    .click();
  await page
    .getByRole("option", { name: "固定路径 + Basic Auth", exact: true })
    .click();
  await expect(page.getByLabel("Basic Auth 用户名", { exact: true })).toHaveCSS(
    "font-size",
    "14px",
  );
  await page
    .getByRole("combobox", { name: "phpMyAdmin 保护", exact: true })
    .click();
  await page.getByRole("option", { name: "随机访问路径", exact: true }).click();
  await expect(
    page.getByLabel("Basic Auth 用户名", { exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "随机生成强密码", exact: true })
    .click();
  const passwordInput = page.getByLabel("数据库 root 密码", { exact: true });
  const confirmationInput = page.getByLabel("确认数据库密码", { exact: true });
  const generated = await passwordInput.inputValue();
  expect(generated).toMatch(/^[a-zA-Z0-9!@#$%^&*()_+=.,:?-]{24}$/);
  expect(generated).toMatch(/[A-Z]/);
  expect(generated).toMatch(/[a-z]/);
  expect(generated).toMatch(/[0-9]/);
  expect(generated).toMatch(/[!@#$%^&*()_+=.,:?-]/);
  await expect(confirmationInput).toHaveValue(generated);
  await expect(passwordInput).toHaveCSS("font-size", "14px");
  await page
    .getByRole("button", { name: "显示数据库密码", exact: true })
    .click();
  await expect(passwordInput).toHaveAttribute("type", "text");
  await page
    .getByRole("button", { name: "隐藏数据库密码", exact: true })
    .click();
  await expect(passwordInput).toHaveAttribute("type", "password");
  const passwordBox = await passwordInput.boundingBox();
  const confirmationBox = await confirmationInput.boundingBox();
  const generateBox = await page
    .getByRole("button", { name: "随机生成强密码", exact: true })
    .boundingBox();
  expect(passwordBox && confirmationBox && generateBox).toBeTruthy();
  if (testInfo.project.name === "desktop") {
    expect(Math.abs(passwordBox!.y - confirmationBox!.y)).toBeLessThan(2);
    expect(Math.abs(passwordBox!.y - generateBox!.y)).toBeLessThan(4);
  } else {
    expect(confirmationBox!.y).toBeGreaterThan(
      passwordBox!.y + passwordBox!.height,
    );
    expect(generateBox!.y).toBeGreaterThan(confirmationBox!.y);
  }
  const modules = page.locator(".install-modules").getByRole("checkbox");
  await expect(modules).toHaveCount(4);
  for (let n = 0; n < 4; n++) await expect(modules.nth(n)).toBeChecked();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth && scrollX === 0,
    ),
  ).toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("installation-form.png"),
    fullPage: true,
    animations: "disabled",
  });
  await page
    .getByLabel("数据库 root 密码", { exact: true })
    .fill("Good-Pass#2026");
  await page
    .getByLabel("确认数据库密码", { exact: true })
    .fill("Good-Pass#2026");
  expect(
    await page
      .getByLabel("数据库 root 密码", { exact: true })
      .evaluate((input: HTMLInputElement) => input.validity.valid),
  ).toBe(true);
  await page.getByRole("combobox", { name: "数据库", exact: true }).click();
  await page.getByRole("option", { name: "不安装数据库", exact: true }).click();
  await expect(page.getByLabel("确认数据库密码", { exact: true })).toHaveCount(
    0,
  );
  await modules.nth(1).click();
  await page.getByRole("button", { name: "开始安装 LNMP" }).click();
  await expect
    .poll(() => submission?.nginx_modules)
    .toEqual(["vts", "fancyindex", "substitutions_filter"]);
  expect(submission?.database_password).toBe("");
  expect(submission?.revision).toBe(revision);
  expect(submission?.php).toBe(15);
  const elapsed = page.getByLabel("安装耗时", { exact: true });
  await expect(elapsed).toHaveText(/安装耗时 0 分 [0-5] 秒/);
  const liveOutput = page.getByLabel("安装编译输出");
  await expect(liveOutput).toContainText(wrappedOutput);
  expect(await liveOutput.textContent()).toContain(wrappedOutput);
  await expect(liveOutput).toHaveCSS("white-space", "pre-wrap");
  await expect(liveOutput).toHaveCSS("overflow-wrap", "anywhere");
  await expect
    .poll(() =>
      liveOutput.evaluate((node) => ({
        horizontal: node.scrollWidth > node.clientWidth,
        vertical: node.scrollHeight > node.clientHeight,
      })),
    )
    .toEqual({ horizontal: false, vertical: true });
  await expect
    .poll(() =>
      liveOutput.evaluate((node) =>
        Math.abs(node.scrollHeight - node.clientHeight - node.scrollTop),
      ),
    )
    .toBeLessThan(2);

  const toolbar = page.locator(".install-log-toolbar");
  expect(await toolbar.locator(":scope > *").allTextContents()).toEqual([
    "读取历史日志",
    "跟随最新输出",
    "下载完整日志",
  ]);
  await expect(page.getByLabel("安装编译输出")).toContainText(
    "compiling nginx",
  );
  expect(logQueries).toContain("?tail=1");
  expect(logQueries).not.toContain("?after=0");
  await expect.poll(() => streamCursors).toContain("100");
  const elapsedBeforeReload = Number(
    (await elapsed.textContent())!.match(/分 (\d+) 秒/)![1],
  );
  logQueries.length = 0;
  streamCursors.length = 0;
  failNextTail = true;
  await page.reload();
  await expect(page.getByText("Nginx 编译", { exact: true })).toBeVisible();
  await expect(elapsed).toHaveText(/安装耗时 0 分 \d+ 秒/);
  expect(
    Number((await elapsed.textContent())!.match(/分 (\d+) 秒/)![1]),
  ).toBeGreaterThanOrEqual(elapsedBeforeReload);

  await expect(page.getByLabel("安装编译输出")).toContainText(
    "compiling nginx",
  );
  expect(logQueries[0]).toBe("?tail=1");
  expect(logQueries.filter((query) => query === "?tail=1")).toHaveLength(2);
  expect(logQueries).not.toContain("?after=0");
  await expect.poll(() => streamCursors).toContain("100");
  expect(streamCursors).not.toContain("0");
  await expect(page.getByLabel("安装编译输出")).not.toContainText(
    "old compilation output",
  );
  const follow = page.getByRole("checkbox", { name: "跟随最新输出" });
  await follow.uncheck();
  await liveOutput.evaluate((node) => {
    node.scrollTop = 0;
  });
  latest = 200;
  await expect(page.getByLabel("安装编译输出")).toContainText(
    "latest compilation progress",
  );
  expect(logQueries).toContain("?after=100");
  await expect
    .poll(() => liveOutput.evaluate((node) => node.scrollTop))
    .toBe(0);
  await follow.check();
  await expect
    .poll(() =>
      liveOutput.evaluate((node) =>
        Math.abs(node.scrollHeight - node.clientHeight - node.scrollTop),
      ),
    )
    .toBeLessThan(2);

  await page.getByRole("button", { name: "读取历史日志" }).click();
  await expect(page.locator(".install-history")).toContainText(
    "old compilation output",
  );
  await expect(page.locator(".install-history")).toContainText(wrappedOutput);
  await expect(page.locator(".install-history")).toHaveCSS(
    "white-space",
    "pre-wrap",
  );
  await expect
    .poll(() =>
      page
        .locator(".install-history")
        .evaluate((node) => node.scrollWidth <= node.clientWidth),
    )
    .toBe(true);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.getByRole("button", { name: "下一页历史" }).click();
  await expect(page.locator(".install-history")).toContainText(
    "latest compilation progress",
  );
  await expect(page.getByLabel("安装编译输出")).not.toContainText(
    "old compilation output",
  );
  await expect(page.getByRole("button", { name: "开始安装 LNMP" })).toHaveCount(
    0,
  );
  job = {
    ...job,
    status: "succeeded",
    phase: "安装完成",
    result: "LNMP 与模块安装成功",
    updated_at: new Date(
      Date.parse(job!.started_at as string) + 302_000,
    ).toISOString(),
  };
  await expect(page.getByText("LNMP 与模块安装成功")).toBeVisible({
    timeout: 15000,
  });
  await expect(elapsed).toHaveText("安装耗时 5 分 2 秒");
  await page.screenshot({
    path: testInfo.outputPath("installation-completed.png"),
    fullPage: true,
  });
});

test("installation timer distinguishes preparation and legacy jobs without a start time", async ({
  page,
}) => {
  const id = "22222222-2222-4222-8222-222222222222";
  let status = "preparing";
  await page.route("**/api/v1/lnmp-installation", (route) =>
    route.fulfill({
      json: {
        installed: status === "succeeded",
        blocked: [],
        job: {
          id,
          status,
          phase: "测试任务阶段",
          created_at: "2026-10-29T08:00:00Z",
          updated_at: "2026-10-29T10:00:00Z",
        },
      },
    }),
  );
  await page.route(`**/api/v1/lnmp-installation/jobs/${id}/events*`, (route) =>
    route.abort(),
  );
  await page.route(`**/api/v1/lnmp-installation/jobs/${id}/logs*`, (route) =>
    route.fulfill({ json: { lines: [], cursor: 0, more: false } }),
  );
  await page.goto("/lnmp/install");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  const elapsed = page.getByLabel("安装耗时", { exact: true });
  await expect(elapsed).toHaveText("尚未开始安装");
  for (const next of [
    "ready",
    "running",
    "succeeded",
    "failed",
    "interrupted",
  ]) {
    status = next;
    await page.reload();
    await expect(elapsed).toHaveText(
      next === "ready" ? "尚未开始安装" : "安装耗时未知",
    );
  }
});
