import { test, expect, type Page } from "@playwright/test";

const current = (page: Page) =>
  page.locator('.login-slide[data-active="true"]');

async function openLogin(page: Page, reduced = false) {
  await page.emulateMedia({
    reducedMotion: reduced ? "reduce" : "no-preference",
  });
  await page.clock.install();
  await page.route("**/api/v1/auth/session", (route) =>
    route.fulfill({ status: 401, json: { error: "请先登录" } }),
  );
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "登录管理面板" }),
  ).toBeVisible();
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1000));
}

async function leaveShowcase(page: Page) {
  await page.getByLabel("用户名", { exact: true }).focus();
  await page.mouse.move(0, 0);
}

async function select(page: Page, name: string) {
  await page.getByRole("button", { name: `显示${name}`, exact: true }).click();
  await expect(
    page.getByRole("button", { name: `显示${name}`, exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await leaveShowcase(page);
}

test("login slogans and local demos rotate together without moving the form or changing input", async ({
  page,
  isMobile,
}, testInfo) => {
  const errors: string[] = [];
  const requests: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("request", (request) => {
    if (request.url().includes("/api/"))
      requests.push(new URL(request.url()).pathname);
  });
  await openLogin(page);
  await select(page, "资源趋势");
  await page.getByLabel("用户名", { exact: true }).fill("demo-admin");
  await page.getByLabel("密码", { exact: true }).fill("demo-password");
  const card = (await page.locator(".login-card").boundingBox())!;
  const stage = (await page.locator(".login-showcase-stage").boundingBox())!;
  const themes = ["resources", "services", "storage", "network", "logs"];
  for (let index = 0; index < themes.length; index++) {
    await expect(current(page)).toHaveAttribute("data-theme", themes[index]);
    await expect(current(page).locator("h1")).toBeVisible();
    if (isMobile) await expect(page.locator(".login-demo")).toHaveCount(0);
    else {
      await expect(
        current(page).getByText("演示数据", { exact: true }),
      ).toHaveCount(0);
      if (index === 0)
        await expect(
          current(page).locator(".recharts-area-area").first(),
        ).toBeVisible();
      if (index === 1)
        await expect(
          current(page).getByText("运行中", { exact: true }),
        ).toHaveCount(4);
      if (index === 2)
        await expect(current(page).getByRole("progressbar")).toHaveAttribute(
          "aria-valuenow",
          "42",
        );
      if (index === 3)
        await expect(current(page).locator(".chart-header")).toContainText(
          "网络吞吐",
        );
      if (index === 4) {
        await expect(current(page).locator(".log-line")).toHaveCount(5);
        await expect(
          current(page).getByText("10:05:02 GET /api 200", { exact: true }),
        ).toBeVisible();
      }
      expect(
        await current(page)
          .locator('.login-demo > [data-slot="card"]')
          .evaluate((card) => card.scrollHeight <= card.clientHeight + 1),
      ).toBe(true);
    }
    const bounds = (await page.locator(".login-card").boundingBox())!;
    // Compare document positions because focusing mobile inputs may scroll.
    expect(bounds.height).toBe(card.height);
    expect(
      (await page.locator(".login-showcase-stage").boundingBox())!.height,
    ).toBe(stage.height);
    if (!isMobile) expect(Math.abs(bounds.y - card.y)).toBeLessThan(1);
    await page.screenshot({
      path: testInfo.outputPath(`login-${themes[index]}.png`),
      fullPage: true,
      animations: "disabled",
    });
    await page.clock.runFor(4999);
    await expect(current(page)).toHaveAttribute("data-theme", themes[index]);
    await page.clock.runFor(1);
    await expect(current(page)).toHaveAttribute(
      "data-theme",
      themes[(index + 1) % themes.length],
    );
  }
  await expect(page.getByLabel("用户名", { exact: true })).toHaveValue(
    "demo-admin",
  );
  await expect(page.getByLabel("密码", { exact: true })).toHaveValue(
    "demo-password",
  );
  await expect(page.getByLabel("密码", { exact: true })).toBeFocused();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  expect(requests.every((path) => path === "/api/v1/auth/session")).toBe(true);
  expect(errors).toEqual([]);
});

test("demos fit the narrow desktop layout and disappear at the mobile breakpoint", async ({
  page,
  isMobile,
}) => {
  test.skip(isMobile, "Desktop breakpoint coverage uses a resizable viewport");
  await openLogin(page, true);
  for (const width of [721, 1024]) {
    await page.setViewportSize({ width, height: 900 });
    for (const name of [
      "资源趋势",
      "服务状态",
      "磁盘空间",
      "网络吞吐",
      "日志速览",
    ]) {
      await select(page, name);
      const card = current(page).locator('.login-demo > [data-slot="card"]');
      await expect(card).toBeVisible();
      expect(
        await card.evaluate(
          (card) =>
            card.scrollHeight <= card.clientHeight + 1 &&
            card.scrollWidth <= card.clientWidth + 1,
        ),
      ).toBe(true);
    }
    await select(page, "磁盘空间");
    await current(page)
      .getByRole("button", { name: "/demo/media，18.0 GiB", exact: true })
      .click();
    await expect(current(page).locator(".treemap-selection")).toContainText(
      "/demo/media",
    );
    const storageCard = current(page).locator(
      '.login-demo > [data-slot="card"]',
    );
    expect(
      await storageCard.evaluate(
        (card) => card.scrollHeight <= card.clientHeight + 1,
      ),
    ).toBe(true);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
  await page.setViewportSize({ width: 720, height: 900 });
  await expect(page.locator(".login-demo")).toHaveCount(0);
  await page.setViewportSize({ width: 1024, height: 900 });
  await expect(current(page).locator(".login-demo")).toBeVisible();
});

test("carousel controls, focus, hover and visibility pause and restart the full interval", async ({
  page,
  isMobile,
}) => {
  await openLogin(page);
  const lastSlide = page.getByRole("button", {
    name: "显示日志速览",
    exact: true,
  });
  await lastSlide.focus();
  await lastSlide.press("Enter");
  await page.clock.runFor(12000);
  await expect(current(page)).toHaveAttribute("data-theme", "logs");
  await leaveShowcase(page);
  await page.clock.runFor(4999);
  await expect(current(page)).toHaveAttribute("data-theme", "logs");
  await page.clock.runFor(1);
  await expect(current(page)).toHaveAttribute("data-theme", "resources");

  await page.getByRole("button", { name: "暂停自动轮换", exact: true }).click();
  await leaveShowcase(page);
  await page.clock.runFor(12000);
  await expect(current(page)).toHaveAttribute("data-theme", "resources");
  await page.getByRole("button", { name: "继续自动轮换", exact: true }).click();
  await leaveShowcase(page);
  await page.clock.runFor(5000);
  await expect(current(page)).toHaveAttribute("data-theme", "services");

  if (!isMobile) {
    await page.locator(".login-intro").hover({ position: { x: 15, y: 10 } });
    await page.clock.runFor(12000);
    await expect(current(page)).toHaveAttribute("data-theme", "services");
    await page.mouse.move(0, 0);
    await page.clock.runFor(5000);
    await expect(current(page)).toHaveAttribute("data-theme", "storage");
  }

  const before = await current(page).getAttribute("data-theme");
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await page.clock.runFor(60000);
  await expect(current(page)).toHaveAttribute("data-theme", before!);
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", {
      configurable: true,
      value: false,
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await page.clock.runFor(4999);
  await expect(current(page)).toHaveAttribute("data-theme", before!);
  await page.clock.runFor(1);
  await expect(current(page)).not.toHaveAttribute("data-theme", before!);
  await select(page, "磁盘空间");
  await page.clock.runFor(4000);
  await select(page, "磁盘空间");
  await page.clock.runFor(4999);
  await expect(current(page)).toHaveAttribute("data-theme", "storage");
  await page.clock.runFor(1);
  await expect(current(page)).toHaveAttribute("data-theme", "network");
});

test("reduced motion starts paused, supports manual selection and explicit playback in dark mode", async ({
  page,
}, testInfo) => {
  await page.addInitScript(() => localStorage.setItem("theme", "dark"));
  await openLogin(page, true);
  await expect(page.locator("html")).toHaveClass(/dark/);
  await leaveShowcase(page);
  await page.clock.runFor(15000);
  await expect(current(page)).toHaveAttribute("data-theme", "resources");
  await expect(
    page.getByRole("button", { name: "继续自动轮换", exact: true }),
  ).toBeVisible();
  for (const name of [
    "服务状态",
    "磁盘空间",
    "网络吞吐",
    "日志速览",
    "资源趋势",
  ]) {
    await select(page, name);
    await expect(current(page)).toHaveCSS("transition-duration", "0s");
    await page.screenshot({
      path: testInfo.outputPath(`login-dark-${name}.png`),
      fullPage: true,
      animations: "disabled",
    });
  }
  await page.getByRole("button", { name: "继续自动轮换", exact: true }).click();
  await leaveShowcase(page);
  await page.clock.runFor(5000);
  await expect(current(page)).toHaveAttribute("data-theme", "services");
  await expect(current(page)).toHaveCSS("transition-duration", "0s");
});

test("rotating the showcase preserves TOTP and recovery code input", async ({
  page,
}) => {
  await openLogin(page);
  await page.route("**/api/v1/auth/login", (route) =>
    route.fulfill({
      json: {
        totp_required: true,
        challenge_token: "demo-challenge",
        expires_in: 300,
      },
    }),
  );
  await page.getByLabel("密码", { exact: true }).fill("demo-password");
  await page.getByRole("button", { name: "登录面板", exact: true }).click();
  const code = page.getByLabel("动态验证码", { exact: true });
  await code.fill("123456");
  await page.clock.runFor(5000);
  await expect(current(page)).toHaveAttribute("data-theme", "services");
  await expect(code).toHaveValue("123456");
  await expect(code).toBeFocused();
  await page.getByRole("button", { name: "使用恢复码", exact: true }).click();
  const recovery = page.getByLabel("恢复码", { exact: true });
  await recovery.fill("demo-recovery-code");
  await page.clock.runFor(5000);
  await expect(current(page)).toHaveAttribute("data-theme", "storage");
  await expect(recovery).toHaveValue("demo-recovery-code");
  await expect(recovery).toBeFocused();
});

for (const reduced of [false, true]) {
  test(`login step transitions respect reduced motion ${reduced}`, async ({
    page,
  }) => {
    await openLogin(page, reduced);
    await page.route("**/api/v1/auth/login", (route) =>
      route.fulfill({
        json: {
          totp_required: true,
          challenge_token: "demo-challenge",
          expires_in: 300,
        },
      }),
    );
    await page.getByLabel("用户名", { exact: true }).fill("demo-admin");
    await page.getByLabel("密码", { exact: true }).fill("demo-password");
    await page.getByRole("button", { name: "登录面板", exact: true }).click();
    const step = page.locator(".login-step");
    await expect(step).toHaveAttribute("data-step", "totp");
    await expect(step).toHaveCSS(
      "animation-name",
      reduced ? "none" : "login-step-enter",
    );
    await expect(page.getByLabel("动态验证码", { exact: true })).toBeFocused();
    await page.getByRole("button", { name: "使用恢复码", exact: true }).click();
    await page
      .getByLabel("恢复码", { exact: true })
      .fill("unused-recovery-code");
    await page
      .getByRole("button", { name: "返回密码登录", exact: true })
      .click();
    await expect(step).toHaveAttribute("data-step", "password");
    await expect(step).toHaveCSS(
      "animation-name",
      reduced ? "none" : "login-step-enter",
    );
    await expect(page.getByLabel("用户名", { exact: true })).toHaveValue(
      "demo-admin",
    );
    await expect(page.getByLabel("密码", { exact: true })).toHaveValue("");
    await page.getByLabel("密码", { exact: true }).fill("demo-password");
    await page.getByRole("button", { name: "登录面板", exact: true }).click();
    await expect(page.getByLabel("动态验证码", { exact: true })).toHaveValue(
      "",
    );
    await expect(page.getByLabel("恢复码", { exact: true })).toHaveCount(0);
  });
}
