import { test, expect, type Locator, type Page } from "./fixtures";

async function navigate(page: Page, name: string) {
  const menu = page.getByRole("button", { name: "打开导航", exact: true });
  if (await menu.isVisible()) await menu.click();
  await page.getByRole("link", { name, exact: true }).click();
}

async function finishAnimations(locator: Locator) {
  await locator.evaluate(async (element) => {
    await Promise.all(
      element.getAnimations({ subtree: true }).map((animation) =>
        animation.finished.catch(() => {
          // Rapid toggles may legitimately cancel an animation.
        }),
      ),
    );
  });
}

for (const reducedMotion of ["no-preference", "reduce"] as const) {
  test(`motion respects ${reducedMotion}, keeps live updates stable and releases overlays`, async ({
    page,
  }, testInfo) => {
    await page.emulateMedia({ reducedMotion });
    const reduced = reducedMotion === "reduce";
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.addInitScript(() => {
      let starts = 0;
      document.addEventListener("animationstart", (event) => {
        if (
          event.animationName === "page-enter" &&
          event.target instanceof Element &&
          event.target.matches(".page-transition > *")
        ) {
          document.documentElement.dataset.pageAnimationStarts = String(
            ++starts,
          );
        }
      });
    });
    await page.route("**/api/v1/services", (route) =>
      route.fulfill({
        json: [
          {
            name: "nginx",
            state: "running",
            manager: "init",
            actions: ["reload"],
            message: "服务运行中",
            checked_at: new Date().toISOString(),
            busy: false,
          },
        ],
      }),
    );
    await page.route("**/api/v1/logs", (route) =>
      route.fulfill({
        json: {
          files: ["/home/wwwlogs/first.log", "/home/wwwlogs/second.log"].map(
            (path) => ({ path, available: false, size: 0, modified: "" }),
          ),
          warnings: [],
        },
      }),
    );
    await page.route("**/api/v1/logs/read", (route) =>
      route.fulfill({
        json: {
          lines: [],
          from: "0",
          to: "0",
          replace_from: "0",
          start: "c0",
          end: "c0",
          size: 0,
          modified: "",
          has_before: false,
          has_after: false,
          limited: false,
        },
      }),
    );
    await page.goto("/");
    await expect(page.locator(".login-card")).toHaveCSS(
      "animation-name",
      reduced ? "none" : "page-enter",
    );
    await page
      .getByLabel("密码", { exact: true })
      .fill("Test-Only-Panel-Password-2026");
    await page.getByRole("button", { name: "登录面板" }).click();
    const content = page.locator(".page-transition");
    await expect(page.locator(".stat-value").first()).not.toContainText("—");
    await finishAnimations(content);
    const entries = () =>
      page.evaluate(() =>
        Number(document.documentElement.dataset.pageAnimationStarts ?? 0),
      );
    const initialEntries = await entries();
    expect(initialEntries > 0).toBe(!reduced);
    const updateTime = page.locator(".heading-time > span").first();
    const initialTime = await updateTime.textContent();
    await expect(updateTime).not.toHaveText(initialTime!);
    await page.getByRole("button", { name: "切换深色主题" }).click();
    await page.getByRole("button", { name: "暂停实时更新" }).click();
    expect(await entries()).toBe(initialEntries);
    expect(
      await content.evaluate((element) =>
        element
          .getAnimations({ subtree: true })
          .some(
            (animation) =>
              animation instanceof CSSAnimation &&
              animation.animationName === "page-enter",
          ),
      ),
    ).toBe(false);
    await page.screenshot({
      path: testInfo.outputPath("dashboard-motion.png"),
    });

    await navigate(page, "LNMP 服务");
    const reload = page.getByRole("button", { name: "重载", exact: true });
    await reload.click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toHaveCSS(
      "animation-name",
      reduced ? "none" : "surface-enter",
    );
    await finishAnimations(dialog);
    const bounds = (await dialog.boundingBox())!;
    const viewport = page.viewportSize()!;
    expect(
      Math.abs(bounds.x + bounds.width / 2 - viewport.width / 2),
    ).toBeLessThan(2);
    expect(
      Math.abs(bounds.y + bounds.height / 2 - viewport.height / 2),
    ).toBeLessThan(2);
    await page.screenshot({ path: testInfo.outputPath("dialog-motion.png") });
    await page.getByRole("button", { name: "取消", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.locator('[data-slot="dialog-overlay"]')).toHaveCount(0);
    await expect(reload).toBeFocused();
    await reload.click();
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(reload).toBeFocused();

    await navigate(page, "日志读取");
    const select = page.getByRole("combobox", {
      name: "选择日志",
      exact: true,
    });
    await select.click();
    const options = page.getByRole("listbox");
    await expect(options).toHaveCSS(
      "animation-name",
      reduced ? "none" : "surface-enter",
    );
    await page.getByRole("option", { name: /second\.log/ }).click();
    await expect(options).toHaveCount(0);
    await expect(select).toBeFocused();
    await expect(select).toContainText("second.log");
    const rules = page.getByRole("button", { name: "筛选规则", exact: true });
    await rules.click();
    await expect(dialog).toHaveCSS(
      "animation-name",
      reduced ? "none" : "log-drawer-in",
    );
    await finishAnimations(dialog);
    const drawerBounds = (await dialog.boundingBox())!;
    expect(
      Math.abs(drawerBounds.x + drawerBounds.width - viewport.width),
    ).toBeLessThan(2);
    expect(drawerBounds.y).toBe(0);
    await page.getByRole("button", { name: "关闭筛选规则" }).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.locator('[data-slot="dialog-overlay"]')).toHaveCount(0);
    await expect(rules).toBeFocused();
    await select.click();
    await page.keyboard.press("Escape");
    await expect(options).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(errors).toEqual([]);
  });
}
