import { test, expect, type Page } from "@playwright/test";

async function login(page: Page) {
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(page.locator(".workspace")).toBeVisible();
}

test("desktop navigation remembers visibility and stays independent of the mobile drawer", async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  await login(page);
  const sidebar = page.locator(".sidebar");
  const shell = page.locator(".main-shell");
  await expect(sidebar).toBeVisible();
  const originalWidth = (await shell.boundingBox())!.width;
  await page.getByRole("button", { name: "隐藏导航", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "显示导航", exact: true }),
  ).toHaveAttribute("aria-expanded", "false");
  await expect(sidebar).toHaveCSS("visibility", "hidden");
  await expect(sidebar).toHaveAttribute("inert", "");
  await expect(
    page.getByRole("link", { name: "日志读取", exact: true }),
  ).toHaveCount(0);
  await expect(shell).toHaveCSS("margin-left", "0px");
  expect((await shell.boundingBox())!.width).toBeGreaterThan(originalWidth);
  await page.goto("/logs");
  await expect(sidebar).toHaveCSS("visibility", "hidden");
  await page.reload();
  await expect(sidebar).toHaveCSS("visibility", "hidden");
  await page.screenshot({
    path: testInfo.outputPath("sidebar-hidden.png"),
    fullPage: true,
  });

  await page.setViewportSize({ width: 390, height: 844 });
  const mobileToggle = page.getByRole("button", {
    name: "打开导航",
    exact: true,
  });
  await expect(mobileToggle).toHaveAttribute("aria-expanded", "false");
  await mobileToggle.click();
  await expect(mobileToggle).toHaveAttribute("aria-expanded", "true");
  await expect(sidebar).not.toHaveAttribute("inert", "");
  await expect(
    page.getByRole("link", { name: "日志读取", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "系统信息", exact: true }).click();
  await expect(page.locator(".mobile-overlay")).toBeHidden();
  await mobileToggle.click();
  await page.setViewportSize({ width: 1100, height: 900 });
  await expect(page.locator(".mobile-overlay")).toBeHidden();
  await expect(sidebar).toHaveCSS("visibility", "hidden");
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(mobileToggle).toHaveAttribute("aria-expanded", "false");

  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByRole("button", { name: "显示导航", exact: true }).click();
  await expect(sidebar).toBeVisible();
  await expect(shell).toHaveCSS("margin-left", "280px");
  await page.reload();
  await expect(
    page.getByRole("button", { name: "隐藏导航", exact: true }),
  ).toHaveAttribute("aria-expanded", "true");
  expect(
    await page.evaluate(() => localStorage.getItem("sidebar-hidden")),
  ).toBe("false");

  // Keyboard activation can reverse transitions while the toolbar is moving.
  for (let i = 0; i < 2; i++) {
    await page
      .getByRole("button", { name: "隐藏导航", exact: true })
      .press("Enter");
    await expect(sidebar).toHaveAttribute("inert", "");
    await page
      .getByRole("button", { name: "显示导航", exact: true })
      .press("Enter");
  }
  await expect(sidebar).toHaveCSS("transform", "none");
  await expect(sidebar).not.toHaveAttribute("inert", "");
  await expect(shell).toHaveCSS("margin-left", "280px");

  await page.setViewportSize({ width: 390, height: 844 });
  for (let i = 0; i < 2; i++) {
    await mobileToggle.click();
    await page.locator(".mobile-overlay").click({
      position: { x: 380, y: 700 },
      force: true,
    });
    await expect(sidebar).toHaveAttribute("inert", "");
    await expect(page.locator(".mobile-overlay")).toHaveCSS(
      "pointer-events",
      "none",
    );
  }
  await expect(page.locator(".mobile-overlay")).toBeHidden();
  await expect(sidebar).toHaveCSS("visibility", "hidden");
  await page.getByRole("button", { name: "切换深色主题" }).click();
  await expect(page.locator("html")).toHaveClass(/dark/);
});

test("navigation still toggles when browser storage is unavailable", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1100, height: 900 });
  await page.addInitScript(() => {
    Storage.prototype.getItem = () => {
      throw new Error("Storage unavailable");
    };
    Storage.prototype.setItem = () => {
      throw new Error("Storage unavailable");
    };
  });
  await login(page);
  await page.getByRole("button", { name: "隐藏导航", exact: true }).click();
  await expect(page.locator(".sidebar")).toHaveCSS("visibility", "hidden");
  await page.getByRole("button", { name: "显示导航", exact: true }).click();
  await expect(page.locator(".sidebar")).toBeVisible();
  await expect(page.locator(".main-shell")).toHaveCSS("margin-left", "210px");
});

test("page and card header action buttons use the log page font size", async ({
  page,
}) => {
  await login(page);
  await page.route("**/api/v1/vhosts/options", (route) =>
    route.fulfill({ status: 503, json: { error: "测试未配置建站" } }),
  );
  await page.route("**/api/v1/vhosts/jobs", (route) =>
    route.fulfill({ json: [] }),
  );
  for (const path of [
    "/",
    "/vhosts",
    "/services",
    "/docker",
    "/storage",
    "/cron",
    "/logs",
    "/system",
  ]) {
    await page.goto(path);
    await expect(page.locator(".workspace")).toBeVisible();
    const actions = page.locator(
      '.page-heading [data-variant][data-size], [data-slot="card-header"] [data-variant][data-size], .vhost-toolbar [data-variant][data-size]',
    );
    if (
      [
        "/vhosts",
        "/services",
        "/docker",
        "/storage",
        "/cron",
        "/logs",
      ].includes(path)
    ) {
      await expect(actions.first()).toBeVisible();
    }
    const sizes = await actions.evaluateAll((nodes) =>
      nodes.map((node) => getComputedStyle(node).fontSize),
    );
    expect(
      sizes.every((size) => size === "14px"),
      `${path}: ${sizes}`,
    ).toBe(true);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
});
