import { test, expect } from "@playwright/test";

test("custom logs validate paths, survive refresh and can be removed from the catalog", async ({
  page,
}, testInfo) => {
  const builtin = "/home/wwwlogs/site.log";
  const custom = "/var/log/syslog";
  const paths = new Set<string>();
  let adds = 0;
  let deletes = 0;
  const modified = "2026-10-29T01:00:00Z";
  const file = (path: string) => ({
    path,
    custom: paths.has(path),
    available: true,
    size: 100,
    modified,
  });
  await page.route("**/api/v1/logs", (route) =>
    route.fulfill({
      json: { files: [builtin, ...paths].map(file), warnings: [] },
    }),
  );
  await page.route("**/api/v1/logs/custom", (route) => {
    const path = route.request().postDataJSON().path as string;
    if (route.request().method() === "DELETE") {
      deletes++;
      paths.delete(path);
      return route.fulfill({ status: 204 });
    }
    adds++;
    if (path.startsWith("/etc/")) {
      return route.fulfill({
        status: 400,
        json: { error: "日志请求无效：禁止读取敏感目录中的文件" },
      });
    }
    paths.add(path);
    return route.fulfill({ json: file(path) });
  });
  await page.route("**/api/v1/logs/read", (route) => {
    const input = route.request().postDataJSON();
    const tail = input.mode === "tail";
    return route.fulfill({
      json: {
        lines: tail ? [{ offset: "0", text: `content of ${input.path}` }] : [],
        from: tail ? "0" : "100",
        to: "100",
        replace_from: tail ? "0" : "100",
        start: "start",
        end: "end",
        size: 100,
        modified,
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
  await expect(
    page.getByRole("combobox", { name: "选择日志", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".log-line")).toContainText(builtin);
  await expect(
    page.getByRole("button", { name: "回到最新" }).locator("svg"),
  ).toHaveCount(1);
  const toolbarFont = await page
    .getByRole("button", { name: "添加日志", exact: true })
    .evaluate((node) => getComputedStyle(node).fontSize);
  expect(toolbarFont).toBe("14px");
  await expect(page.getByRole("button", { name: "移除日志" })).toHaveCount(0);
  await page.getByRole("button", { name: "添加日志", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "添加日志文件" });
  await expect(
    dialog.getByRole("button", { name: "取消", exact: true }),
  ).toHaveCSS("font-size", toolbarFont);
  await expect(
    dialog.getByRole("button", { name: "添加并读取", exact: true }),
  ).toHaveCSS("font-size", toolbarFont);
  const input = dialog.getByLabel("日志文件绝对路径");
  await input.fill("relative.log");
  await dialog.getByRole("button", { name: "添加并读取" }).click();
  await expect(
    dialog.locator(".custom-log-input").getByRole("alert"),
  ).toHaveText("请输入日志文件的绝对路径");
  expect(adds).toBe(0);
  await input.fill("/etc/shadow");
  await dialog.getByRole("button", { name: "添加并读取" }).click();
  await expect(
    dialog.locator(".custom-log-input").getByRole("alert"),
  ).toContainText("禁止读取敏感目录");
  expect(paths.size).toBe(0);
  expect(adds).toBe(1);
  await page.screenshot({
    path: testInfo.outputPath("custom-log-rejected.png"),
    fullPage: true,
  });
  await input.fill(custom);
  await dialog.getByRole("button", { name: "添加并读取" }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator(".log-line")).toContainText(custom);
  await expect(page.getByRole("button", { name: "移除日志" })).toBeVisible();
  expect(adds).toBe(2);
  await page.reload();
  await page.getByRole("combobox", { name: "选择日志", exact: true }).click();
  await page.getByRole("option", { name: custom, exact: true }).click();
  await expect(page.locator(".log-line")).toContainText(custom);
  await page.getByRole("button", { name: "移除日志" }).click();
  await expect(page.locator(".log-line")).toContainText(builtin);
  expect(deletes).toBe(1);
  expect(paths.size).toBe(0);
  await page.getByRole("combobox", { name: "选择日志", exact: true }).click();
  await expect(
    page.getByRole("option", { name: custom, exact: true }),
  ).toHaveCount(0);
  await page.keyboard.press("Escape");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});
