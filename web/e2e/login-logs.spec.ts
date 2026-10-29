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
