import { test, expect, type Page } from "./fixtures";

// Authenticate against this test's fresh backend; never reuse another server's cookies.
test.beforeEach(async ({ context, request }) => {
  const response = await request.post("/api/v1/auth/login", {
    data: { username: "admin", password: "Test-Only-Panel-Password-2026" },
  });
  expect(response.ok()).toBe(true);
  await context.addCookies((await request.storageState()).cookies);
});

async function login(page: Page, path: string) {
  await page.goto(path);
  await expect(page.locator(".workspace")).toBeVisible();
}

async function fakeStreams(page: Page) {
  await page.addInitScript(() => {
    type Fake = EventTarget & {
      url: string;
      readyState: number;
      onopen?: () => void;
      close: () => void;
    };
    const globals = window as unknown as {
      __streams: Fake[];
      EventSource: unknown;
    };
    globals.__streams = [];
    globals.EventSource = class extends EventTarget {
      static OPEN = 1;
      static CLOSED = 2;
      readyState = 0;
      onopen?: () => void;
      constructor(public url: string) {
        super();
        globals.__streams.push(this);
        setTimeout(() => {
          if (this.readyState !== 2) {
            this.readyState = 1;
            this.onopen?.();
          }
        }, 0);
      }
      close() {
        this.readyState = 2;
      }
    };
  });
}

async function emit(page: Page, path: string, name: string, data = {}) {
  await page.evaluate(
    ({ path, name, data }) => {
      const streams = (
        window as unknown as {
          __streams: (EventTarget & { url: string; readyState: number })[];
        }
      ).__streams;
      const stream = streams.findLast(
        (source) => source.url.includes(path) && source.readyState !== 2,
      );
      if (!stream) throw new Error(`No active stream: ${path}`);
      stream.dispatchEvent(
        new MessageEvent(name, { data: JSON.stringify(data) }),
      );
    },
    { path, name, data },
  );
}

test("late session refresh cannot restore the workspace after logout", async ({
  page,
}, info) => {
  await fakeStreams(page);
  await login(page, "/system");
  let release!: () => void;
  const ready = new Promise<void>((resolve) => {
    release = resolve;
  });
  let held = false;
  await page.route("**/api/v1/auth/session", async (route) => {
    const response = await route.fetch();
    held = true;
    await ready;
    await route.fulfill({ response }).catch(() => {});
  });
  await page.evaluate(() => {
    const now = Date.now.bind(Date);
    Date.now = () => now() + 61000;
    window.dispatchEvent(new Event("visibilitychange"));
  });
  await expect.poll(() => held).toBe(true);
  if (info.project.name === "mobile")
    await page.getByRole("button", { name: "打开导航", exact: true }).click();
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "登录管理面板", exact: true }),
  ).toBeVisible();
  release();
  await expect(page.locator(".workspace")).toHaveCount(0);
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板", exact: true }).click();
  await expect(page.locator(".workspace")).toBeVisible();
});

test("renewal keeps the latest preview when a closed window's response arrives late", async ({
  page,
}) => {
  const sites = ["a.example", "b.example"].map((domain) => ({
    name: `${domain}.conf`,
    filename: `${domain}.conf`,
    enabled: true,
    domains: [domain],
    listens: ["80"],
    roots: [],
    proxies: [],
    certificates: [],
    warnings: [],
  }));
  await page.route("**/api/v1/vhosts", (route) =>
    route.fulfill({ json: { status: "ready", sites, usage: {} } }),
  );
  await page.route("**/api/v1/vhosts/screenshots", (route) =>
    route.fulfill({ json: { items: {} } }),
  );
  await page.route("**/api/v1/vhosts/jobs", (route) =>
    route.fulfill({ json: [] }),
  );
  let release!: () => void;
  const ready = new Promise<void>((resolve) => {
    release = resolve;
  });
  let calls = 0;
  let sent: { items: { name: string }[] } | undefined;
  await page.route("**/api/v1/vhosts/ssl/renew/preview", async (route) => {
    const names = route.request().postDataJSON().names as string[];
    if (++calls === 1) await ready;
    await route
      .fulfill({
        json: names.map((name) => ({
          name,
          domains: [name],
          eligible: true,
          provider: "letsencrypt",
          email: "review@example.com",
        })),
      })
      .catch(() => {});
  });
  await page.route("**/api/v1/vhosts/ssl/renew", (route) => {
    sent = route.request().postDataJSON();
    return route.fulfill({
      status: 503,
      json: { error: "测试：已捕获续签目标" },
    });
  });
  await login(page, "/vhosts");
  await page.getByRole("button", { name: "表格视图", exact: true }).click();
  await page
    .getByRole("checkbox", { name: "选择站点 a.example.conf", exact: true })
    .check();
  await page
    .getByRole("button", { name: "提前续签 SSL（1）", exact: true })
    .click();
  await expect.poll(() => calls).toBe(1);
  await page.keyboard.press("Escape");
  await page
    .getByRole("checkbox", { name: "选择站点 a.example.conf", exact: true })
    .uncheck();
  await page
    .getByRole("checkbox", { name: "选择站点 b.example.conf", exact: true })
    .check();
  await page
    .getByRole("button", { name: "提前续签 SSL（1）", exact: true })
    .click();
  await expect(page.locator(".renew-config-item")).toContainText(
    "b.example.conf",
  );
  release();
  await page
    .getByRole("button", { name: "开始逐个续签（1）", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("已捕获续签目标");
  expect(sent!.items.map((item) => item.name)).toEqual(["b.example.conf"]);
});

for (const kind of ["create", "renew", "installation"] as const) {
  test(`${kind} output reopens an explicitly closed stream and preserves log continuity`, async ({
    page,
  }) => {
    await fakeStreams(page);
    const id = "11111111-1111-4111-8111-111111111111";
    const job = {
      id,
      name: "test.conf",
      kind: kind === "renew" ? "ssl-renew" : undefined,
      status: "running",
      phase: "ssl",
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
      items: [
        {
          name: "test.conf",
          provider: "letsencrypt",
          email: "test@example.com",
          status: "running",
        },
      ],
      lines: [
        {
          id: 1,
          phase: "ssl",
          text: "before-reconnect",
          time: new Date().toISOString(),
        },
      ],
    };
    let reads = 0;
    await page.route("**/api/v1/vhosts", (route) =>
      route.fulfill({ json: { status: "ready", sites: [], usage: {} } }),
    );
    await page.route("**/api/v1/vhosts/jobs", (route) =>
      route.fulfill({ json: [job] }),
    );
    await page.route("**/api/v1/vhosts/jobs/*", (route) => {
      reads++;
      return route.fulfill({ json: job });
    });
    await page.route("**/api/v1/lnmp-installation", (route) =>
      route.fulfill({ json: { installed: false, blocked: [], job } }),
    );
    await page.route("**/api/v1/lnmp-installation/jobs/*/logs*", (route) => {
      reads++;
      return route.fulfill({
        json: {
          lines: [{ id: 1, text: "before-reconnect" }],
          cursor: 1,
          more: false,
        },
      });
    });
    await login(page, kind === "installation" ? "/lnmp/install" : "/vhosts");
    if (kind !== "installation")
      await page
        .getByRole("button", {
          name: kind === "create" ? "查看建站进度" : "查看续签进度",
          exact: true,
        })
        .click();
    await expect(
      page.getByText("before-reconnect", { exact: false }),
    ).toBeVisible();
    const path = `/jobs/${id}/events`;
    const before = reads;
    await emit(page, path, "auth_expired");
    await expect
      .poll(() =>
        page.evaluate(
          (path) =>
            (
              window as unknown as {
                __streams: { url: string; readyState: number }[];
              }
            ).__streams.filter((s) => s.url.includes(path)).length,
          path,
        ),
      )
      .toBe(2);
    expect(reads).toBeGreaterThan(before);
    if (kind === "installation") {
      expect(
        await page.evaluate(
          (path) =>
            (
              window as unknown as { __streams: { url: string }[] }
            ).__streams.findLast((s) => s.url.includes(path))!.url,
          path,
        ),
      ).toContain("after=1");
    }
    await emit(page, path, "log", {
      id: 2,
      phase: "ssl",
      text: "after-reconnect",
      time: new Date().toISOString(),
    });
    await expect(
      page.getByText("after-reconnect", { exact: false }),
    ).toBeVisible();
    const output = page.locator(
      kind === "installation" ? ".install-terminal" : ".create-task-log",
    );
    expect((await output.innerText()).match(/before-reconnect/g)).toHaveLength(
      1,
    );
    await emit(page, path, "complete");
    await expect
      .poll(() =>
        page.evaluate(
          (path) =>
            (
              window as unknown as {
                __streams: { url: string; readyState: number }[];
              }
            ).__streams.filter(
              (s) => s.url.includes(path) && s.readyState !== 2,
            ).length,
          path,
        ),
      )
      .toBe(0);
  });
}

test("metrics recovers after an auth refresh temporarily returns 503", async ({
  page,
}) => {
  await fakeStreams(page);
  await login(page, "/system");
  let checks = 0;
  await page.route("**/api/v1/auth/session", (route) => {
    if (++checks === 1)
      return route.fulfill({
        status: 503,
        json: { error: "temporary outage" },
      });
    return route.continue();
  });
  await emit(page, "/metrics/stream", "auth_expired");
  await expect.poll(() => checks).toBeGreaterThanOrEqual(2);
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (
            window as unknown as {
              __streams: { url: string; readyState: number }[];
            }
          ).__streams.filter(
            (s) => s.url.includes("/metrics/stream") && s.readyState !== 2,
          ).length,
      ),
    )
    .toBe(1);
  await emit(page, "/metrics/stream", "metrics", {
    timestamp: new Date().toISOString(),
    disks: [],
    errors: {},
  });
  await expect(page.getByText("实时连接", { exact: true })).toBeVisible();
});
