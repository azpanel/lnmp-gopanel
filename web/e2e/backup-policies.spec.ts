import { test, expect, type Page } from "./fixtures";
import type { Policy, PolicySave } from "../src/lib/backup-policies";

async function choosePolicyOption(page: Page, label: string, option: string) {
  await page.getByRole("combobox", { name: label, exact: true }).click();
  await page.getByRole("option", { name: option, exact: true }).click();
}

async function adjustRetention(page: Page, label: string, value: number) {
  const slider = page.getByRole("slider", { name: label, exact: true });
  const current = Number(await slider.inputValue());
  for (let i = 0; i < Math.abs(value - current); i++) {
    await slider.press(value > current ? "ArrowRight" : "ArrowLeft");
  }
  await expect(slider).toHaveValue(String(value));
  await expect(slider).toHaveAttribute("aria-valuetext", `${value} 份`);
}

async function policyFixture(page: Page) {
  const policies = new Map<string, Policy>();
  const saves: PolicySave[] = [];
  let databases = [
    "app",
    "other",
    "mysql",
    "information_schema",
    "performance_schema",
    "phpmyadmin",
    "sys",
  ];
  await page.route("**/api/v1/backups/components", (route) =>
    route.fulfill({
      json: {
        items: ["gdrive", "rclone"].map((name) => ({
          name,
          status: "missing",
          version: "",
          supported: true,
        })),
      },
    }),
  );
  await page.route("**/api/v1/backups/policies", async (route) => {
    if (route.request().method() === "POST") {
      const body = route.request().postDataJSON() as PolicySave;
      saves.push(body);
      const { password: _password, password_action, copy_from, ...data } = body;
      const source = copy_from ? policies.get(copy_from.id) : undefined;
      if (copy_from && (!source || source.revision !== copy_from.revision))
        return route.fulfill({
          status: 409,
          json: { error: "备份政策已被修改，请刷新后重试" },
        });
      const policy = {
        ...data,
        id: String(policies.size + 1).padStart(32, "0"),
        revision: 1,
        compression: {
          ...data.compression,
          has_password:
            password_action === "replace" ||
            (password_action === "keep" && !!source?.compression.has_password),
        },
        updated_at: new Date().toISOString(),
      };
      policies.set(policy.id, policy);
      return route.fulfill({ status: 201, json: policy });
    }
    return route.fulfill({
      json: [...policies.values()].map((p) => ({
        ...p,
        database_enabled: p.databases.enabled,
        database_mode: p.databases.mode,
        database_count: p.databases.selected.length,
        file_count: p.files.length,
      })),
    });
  });
  await page.route("**/api/v1/backups/policies/*", async (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1)!;
    const previous = policies.get(id)!;
    if (route.request().method() === "PUT") {
      const body = route.request().postDataJSON() as PolicySave;
      saves.push(body);
      const { password: _password, password_action, ...data } = body;
      const policy = {
        ...data,
        revision: previous.revision + 1,
        compression: {
          ...data.compression,
          has_password:
            password_action === "keep"
              ? previous.compression.has_password
              : password_action === "replace",
        },
      };
      policies.set(id, policy);
      return route.fulfill({ json: policy });
    }
    if (route.request().method() === "DELETE") {
      policies.delete(id);
      return route.fulfill({ json: {} });
    }
    return route.fulfill({ json: previous });
  });
  await page.route("**/api/v1/backups/databases/inspect", (route) => {
    if (route.request().postDataJSON().password !== "db-only-secret")
      return route.fulfill({
        status: 502,
        json: { error: "无法认证本机数据库管理员" },
      });
    return route.fulfill({
      json: databases.map((name) => ({ name, size: 2048 })),
    });
  });
  await page.route("**/api/v1/backups/databases/tables", (route) => {
    expect(route.request().postDataJSON().password).toBe("db-only-secret");
    return route.fulfill({
      json: {
        items: [
          { name: "orders", type: "BASE TABLE", size: 1024 },
          { name: "logs", type: "BASE TABLE", size: 2048 },
          { name: "view", type: "VIEW", size: null },
        ],
        page: 1,
        page_size: 100,
        total: 3,
      },
    });
  });
  await page.route("**/api/v1/backups/files/inspect", (route) => {
    const input = route.request().postDataJSON();
    return route.fulfill({
      json: {
        path: input.path,
        type: "directory",
        valid: true,
        complete: true,
        original_bytes: 4096,
        included_bytes: input.excludes.length ? 1024 : 4096,
        rules: input.excludes.map((pattern: string) => ({
          pattern,
          count: pattern === "no-match/**" ? 0 : 3,
          samples: pattern === "no-match/**" ? [] : ["cache/first.log"],
          truncated: false,
        })),
      },
    });
  });
  await page.route("**/api/v1/backups/policies/validate-paths", (route) => {
    const { files, destinations } = route.request().postDataJSON();
    const warnings = files.flatMap((s: { path: string }, i: number) =>
      files
        .slice(0, i)
        .filter(
          (old: { path: string }) =>
            s.path.startsWith(old.path + "/") ||
            old.path.startsWith(s.path + "/"),
        )
        .map(() => ({
          field: "files",
          index: i,
          message: "来源存在父子范围重叠，可能重复备份",
        })),
    );
    const errors = destinations.flatMap(
      (d: { type: string; path: string }, i: number) =>
        d.type === "local" &&
        files.some(
          (s: { path: string }) =>
            d.path === s.path || d.path.startsWith(s.path + "/"),
        )
          ? [
              {
                field: "destinations",
                index: i,
                message: "本地存储位置不能位于备份来源内部或与其相同",
              },
            ]
          : [],
    );
    return route.fulfill({ json: { warnings, errors } });
  });
  await page.route("**/api/v1/vhosts", (route) =>
    route.fulfill({
      json: {
        status: "ready",
        sites: [
          {
            name: "app.conf",
            domains: ["app.example.com"],
            enabled: true,
            roots: [
              {
                configured: "/srv/project/public",
                path: "/srv/project",
                public_parent: true,
              },
            ],
          },
          {
            name: "shared.conf",
            domains: ["shared.example.com"],
            enabled: false,
            roots: [
              {
                configured: "/srv/project",
                path: "/srv/project",
                public_parent: false,
              },
            ],
          },
          {
            name: "dynamic.conf",
            domains: ["dynamic.example.com"],
            enabled: true,
            roots: [
              {
                configured: "$dynamic",
                path: "",
                public_parent: false,
                error: "动态 root 无法解析",
              },
            ],
          },
        ],
        usage: {},
        updated_at: null,
      },
    }),
  );
  await page.goto("/backups");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "网站备份", exact: true }),
  ).toBeVisible();
  return {
    policies,
    saves,
    addDatabase: (name: string) => {
      databases = [...databases, name];
    },
  };
}

async function addLocalDestination(page: Page, path = "/var/backups/web") {
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("tab", { name: "存储位置", exact: true }).click();
  await dialog
    .getByRole("button", { name: "添加存储位置", exact: true })
    .click();
  await dialog.getByLabel("本地目录", { exact: true }).last().fill(path);
}

test("database loading is centered and refresh preserves selections and table rules", async ({
  page,
}, testInfo) => {
  await policyFixture(page);
  let release!: () => void;
  let pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/v1/backups/databases/inspect", async (route) => {
    await pending;
    return route.fulfill({
      json: [
        { name: "app", size: 2048 },
        { name: "other", size: 1024 },
      ],
    });
  });
  await page.route("**/api/v1/backups/databases/tables", (route) =>
    route.fulfill({
      json: {
        items: [{ name: "orders", type: "BASE TABLE", size: 1024 }],
        page: 1,
        page_size: 100,
        total: 1,
      },
    }),
  );
  await page.getByRole("button", { name: "添加备份政策", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("tab", { name: "数据库", exact: true }).click();
  const list = dialog.locator(".policy-database-list");
  const loading = list.getByRole("status");
  await expect(loading).toHaveText("正在读取数据库…");
  await expect(loading.locator("span")).toHaveCSS("font-size", "14px");
  await expect(loading.locator("svg")).toHaveCSS("animation-name", "spin");
  await page.emulateMedia({ reducedMotion: "reduce" });
  await expect(loading.locator("svg")).toHaveCSS("animation-name", "none");
  await dialog.evaluate((el) =>
    Promise.all(
      el
        .getAnimations()
        .filter(
          (animation) => animation.effect?.getTiming().iterations !== Infinity,
        )
        .map((animation) => animation.finished),
    ),
  );
  const centered = await list.evaluate((el) => {
    const area = el.getBoundingClientRect();
    const icon = el.querySelector("svg")!.getBoundingClientRect();
    const text = el
      .querySelector('[role="status"] span')!
      .getBoundingClientRect();
    return {
      horizontal: Math.abs(
        (icon.left + icon.right) / 2 - (area.left + area.right) / 2,
      ),
      vertical: Math.abs(
        (icon.top + text.bottom) / 2 - (area.top + area.bottom) / 2,
      ),
    };
  });
  expect(centered.horizontal).toBeLessThan(2);
  expect(centered.vertical).toBeLessThan(2);
  await expect(loading).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("backup-database-loading.png"),
    animations: "disabled",
  });
  release();
  await expect(loading).toHaveCount(0);
  await choosePolicyOption(page, "数据库范围", "指定数据库");
  await dialog
    .getByRole("checkbox", { name: "备份数据库 other", exact: true })
    .uncheck();
  await choosePolicyOption(page, "app 的表规则", "仅备份所选表");
  await dialog
    .getByRole("checkbox", { name: "app.orders", exact: true })
    .check();
  pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  await dialog.getByRole("button", { name: "刷新数据库" }).click();
  await expect(loading).toBeVisible();
  await expect(list.getByRole("checkbox")).toHaveCount(0);
  release();
  await expect(loading).toHaveCount(0);
  await expect(
    dialog.getByRole("checkbox", { name: "备份数据库 app", exact: true }),
  ).toBeChecked();
  await expect(
    dialog.getByRole("checkbox", { name: "备份数据库 other", exact: true }),
  ).not.toBeChecked();
  await expect(
    dialog.getByRole("combobox", { name: "app 的表规则", exact: true }),
  ).toContainText("仅备份所选表");
  await expect(
    dialog.getByRole("checkbox", { name: "app.orders", exact: true }),
  ).toBeChecked();
  await dialog
    .getByRole("textbox", { name: "搜索数据库", exact: true })
    .fill("no-match");
  await expect(
    list.getByText("没有匹配的数据库。", { exact: true }),
  ).toBeVisible();
  await expect(loading).toHaveCount(0);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("backup policy tabs, source rules, secrets and CRUD", async ({
  page,
}, testInfo) => {
  const f = await policyFixture(page);
  await page.getByRole("button", { name: "添加备份政策", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("button", { name: "查看五次" }),
  ).toHaveAttribute("aria-expanded", "false");
  await expect(
    dialog.getByRole("button", { name: "高级编辑", exact: true }),
  ).toHaveAttribute("aria-expanded", "false");
  await dialog.evaluate((element) =>
    Promise.all(element.getAnimations().map((animation) => animation.finished)),
  );
  const headerBefore = await dialog
    .locator(".policy-editor-header")
    .boundingBox();
  const footerBefore = await dialog
    .locator(".policy-editor-footer")
    .boundingBox();
  await dialog
    .locator('.policy-tab-panel[data-state="active"]')
    .evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });
  await expect(
    dialog.getByRole("button", { name: "保存政策" }),
  ).toBeInViewport();
  await expect(dialog.getByRole("tab", { name: "基本设置" })).toBeInViewport();
  expect((await dialog.locator(".policy-editor-header").boundingBox())?.y).toBe(
    headerBefore?.y,
  );
  expect((await dialog.locator(".policy-editor-footer").boundingBox())?.y).toBe(
    footerBefore?.y,
  );
  await dialog
    .locator('.policy-tab-panel[data-state="active"]')
    .evaluate((el) => {
      el.scrollTop = 0;
    });
  await dialog.getByLabel("名称", { exact: true }).fill("网站每日备份");
  await dialog.getByLabel("备注", { exact: true }).fill("保留文件与数据库");
  await choosePolicyOption(page, "频率编辑方式", "每周");
  await dialog.getByLabel("计划小时").fill("3");
  await dialog.getByLabel("计划分钟").fill("15");
  await choosePolicyOption(page, "星期", "周六");
  await expect(dialog.locator(".policy-cron-summary code")).toHaveText(
    "15 3 * * 6",
  );
  await choosePolicyOption(page, "压缩格式", "7z");
  await choosePolicyOption(page, "压缩等级", "7");
  await choosePolicyOption(page, "压缩密码", "设置新密码");
  await dialog.getByLabel("新压缩密码").fill("archive-only-secret");
  await dialog.getByLabel("确认压缩密码").fill("archive-only-secret");
  expect(
    await dialog
      .locator("input, textarea")
      .evaluateAll((inputs) => [
        ...new Set(inputs.map((input) => getComputedStyle(input).fontSize)),
      ]),
  ).toEqual(["14px"]);
  await adjustRetention(page, "数据库备份保留份数", 14);
  await adjustRetention(page, "文件备份保留份数", 4);
  await dialog
    .getByRole("slider", { name: "文件备份保留份数" })
    .scrollIntoViewIfNeeded();
  await page.screenshot({
    path: testInfo.outputPath("backup-policy-retention.png"),
    animations: "disabled",
  });
  const visibleIcons = await dialog
    .locator(".policy-field-label:visible > svg")
    .evaluateAll((icons) => icons.map((icon) => icon.getAttribute("class")));
  expect(visibleIcons.length).toBeGreaterThan(8);
  expect(new Set(visibleIcons).size).toBe(visibleIcons.length);
  await expect(dialog.getByRole("alertdialog")).toHaveCount(0);
  const format = dialog.getByRole("combobox", {
    name: "压缩格式",
    exact: true,
  });
  await expect(format).toHaveCSS("font-size", "14px");
  await format.focus();
  await format.press("Enter");
  await expect(page.getByRole("listbox")).toBeVisible();
  await expect(page.getByRole("option", { name: "7z", exact: true })).toHaveCSS(
    "font-size",
    "14px",
  );
  await expect(dialog.getByRole("alertdialog")).toHaveCount(0);
  await expect(
    page.getByRole("option", { name: "7z", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await page.screenshot({
    path: testInfo.outputPath("backup-policy-select.png"),
    animations: "disabled",
    fullPage: false,
  });
  await expect(dialog.getByRole("alertdialog")).toHaveCount(0);
  await expect(
    dialog.locator('[role="combobox"][aria-label="压缩格式"]'),
  ).toHaveAttribute("aria-expanded", "true");
  await page.keyboard.press("Escape");
  await expect(format).toBeFocused();
  await expect(dialog.getByRole("alertdialog")).toHaveCount(0);
  await expect(dialog.getByLabel("数据库管理员密码")).toHaveCount(0);

  await dialog.getByRole("tab", { name: "数据库", exact: true }).click();
  await expect(dialog.getByText(/无法认证本机数据库管理员/)).toBeVisible();
  await dialog.getByLabel("数据库管理员密码").fill("db-only-secret");
  await dialog.getByRole("tab", { name: "基本设置", exact: true }).click();
  await expect(dialog.getByLabel("数据库管理员密码")).toHaveCount(0);
  await dialog.getByRole("tab", { name: "数据库", exact: true }).click();
  await expect(
    dialog.getByRole("checkbox", { name: "备份数据库 app", exact: true }),
  ).toBeChecked();
  await dialog.locator(".policy-db-connection summary").click();
  await expect(dialog.getByLabel("数据库管理员密码")).toHaveValue(
    "db-only-secret",
  );
  await dialog.getByRole("button", { name: "重新连接" }).click();
  await expect(
    dialog.getByRole("checkbox", { name: "备份数据库 app", exact: true }),
  ).toBeChecked();
  for (const name of [
    "mysql",
    "information_schema",
    "performance_schema",
    "phpmyadmin",
    "sys",
  ])
    await expect(
      dialog.getByRole("checkbox", { name: `备份数据库 ${name}`, exact: true }),
    ).not.toBeChecked();
  const app = dialog.locator(".policy-database").filter({
    has: page.getByRole("checkbox", { name: "备份数据库 app", exact: true }),
  });
  await app.getByRole("button", { name: "查看 app 的表" }).click();
  await choosePolicyOption(page, "app 的表规则", "仅备份所选表");
  const detail = dialog.locator(".policy-database-detail");
  await detail
    .getByRole("checkbox", { name: "app.orders", exact: true })
    .check();
  await expect(detail.getByText("1.0 KB", { exact: true })).toBeVisible();
  f.addDatabase("future_app");
  await dialog.getByRole("button", { name: "刷新数据库" }).click();
  await expect(
    dialog.getByRole("checkbox", {
      name: "备份数据库 future_app",
      exact: true,
    }),
  ).toBeChecked();
  await page.screenshot({
    path: testInfo.outputPath("backup-policy-databases.png"),
    animations: "disabled",
    fullPage: false,
  });
  await addLocalDestination(page);
  await dialog.getByRole("tab", { name: "文件与目录" }).click();
  await dialog.getByLabel("添加文件或目录路径").fill("/srv/网站 files");
  await dialog.getByRole("button", { name: "添加路径" }).click();
  await dialog
    .getByLabel("排除子目录或文件（相对路径，每行一条）")
    .fill("cache/\nlogs/*.log\n**/node_modules/**");
  const source = dialog.locator(".policy-file-source");
  await expect(source.locator(".policy-file-size").nth(0)).toContainText(
    "4.0 KB",
  );
  await expect(source.locator(".policy-file-size").nth(1)).toContainText(
    "1.0 KB",
  );
  await source.getByRole("button", { name: "收起规则" }).click();
  await expect(
    dialog.getByLabel("排除子目录或文件（相对路径，每行一条）"),
  ).toHaveCount(0);
  await source.getByRole("button", { name: "排除规则", exact: true }).click();
  await expect(
    dialog.getByLabel("排除子目录或文件（相对路径，每行一条）"),
  ).toHaveValue("cache/\nlogs/*.log\n**/node_modules/**");
  await page.screenshot({
    path: testInfo.outputPath("backup-policy-files.png"),
    fullPage: true,
  });
  await dialog.getByRole("tab", { name: "基本设置" }).click();
  await expect(dialog.getByLabel("名称", { exact: true })).toHaveValue(
    "网站每日备份",
  );
  await dialog.getByRole("button", { name: "保存政策" }).click();
  await expect(dialog).not.toBeVisible();
  expect(f.saves[0].databases.mode).toBe("all");
  expect(f.saves[0].databases.rules).toContainEqual({
    database: "app",
    mode: "include",
    tables: ["orders"],
  });
  expect(f.saves[0].files[0].excludes).toEqual([
    "cache/",
    "logs/*.log",
    "**/node_modules/**",
  ]);
  expect(JSON.stringify(f.policies.values().next().value)).not.toContain(
    "secret",
  );
  await page.reload();
  await expect(page.getByText("网站每日备份", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await expect(
    dialog.getByRole("combobox", { name: "压缩密码", exact: true }),
  ).toHaveText("保持已有密码（已设置）");
  await expect(dialog.getByLabel("新压缩密码")).toHaveCount(0);
  await expect(
    dialog.getByRole("slider", { name: "数据库备份保留份数" }),
  ).toHaveValue("14");
  await expect(
    dialog.getByRole("slider", { name: "文件备份保留份数" }),
  ).toHaveValue("4");
  await dialog.getByLabel("备注", { exact: true }).fill("编辑后的备注");
  await dialog.getByRole("button", { name: "保存政策" }).click();
  await expect(dialog).not.toBeVisible();
  expect(f.saves.at(-1)?.password_action).toBe("keep");
  await page.getByRole("switch", { name: "启用 网站每日备份" }).click();
  await expect(
    page.getByRole("switch", { name: "启用 网站每日备份" }),
  ).not.toBeChecked();
  const search = page.getByRole("searchbox", {
    name: "搜索政策",
    includeHidden: true,
  });
  await search.fill("网站");
  await search.press("Enter");
  await page.getByRole("button", { name: "查看", exact: true }).click();
  await expect(search).toHaveValue("网站");
  await expect(dialog.getByLabel("数据库管理员密码")).toHaveCount(0);
  expect(
    await search.evaluate((input: HTMLInputElement) => ({
      autocomplete: input.autocomplete,
      role: input.form?.getAttribute("role"),
      passwords: input.form?.querySelectorAll('input[type="password"]').length,
    })),
  ).toEqual({ autocomplete: "off", role: "search", passwords: 0 });
  await expect(dialog.getByLabel("名称", { exact: true })).toBeDisabled();
  await expect(
    dialog.getByRole("slider", { name: "数据库备份保留份数" }),
  ).toBeDisabled();
  await expect(dialog.getByLabel("备注", { exact: true })).toHaveValue(
    "编辑后的备注",
  );
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(search).toHaveValue("网站");
  await search.fill("");
  await page.getByRole("button", { name: "删除", exact: true }).click();
  await page.getByRole("button", { name: "确认删除", exact: true }).click();
  await expect(
    page.getByText("尚未添加备份政策", { exact: false }),
  ).toBeVisible();
  expect(f.policies.size).toBe(0);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBeTruthy();
});

test("policy validation, file-only configuration and discard confirmation", async ({
  page,
}) => {
  const f = await policyFixture(page);
  await page.getByRole("button", { name: "添加备份政策", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "保存政策" }).click();
  await expect(dialog.getByRole("alert")).toContainText("政策名称");
  await dialog.getByLabel("名称", { exact: true }).fill("仅文件备份");
  await dialog.getByRole("tab", { name: "数据库", exact: true }).click();
  await dialog
    .getByRole("switch", { name: "备份数据库", exact: true })
    .uncheck();
  await dialog.getByRole("button", { name: "保存政策" }).click();
  await expect(dialog.getByRole("tab", { name: "文件与目录" })).toHaveAttribute(
    "data-state",
    "active",
  );
  await dialog.getByLabel("添加文件或目录路径").fill("relative/path");
  await dialog.getByRole("button", { name: "添加路径" }).click();
  await expect(dialog.getByText("请输入绝对路径")).toBeVisible();
  await dialog.getByLabel("添加文件或目录路径").fill("/srv/site");
  await dialog.getByRole("button", { name: "添加路径" }).click();
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(
    dialog.getByRole("alertdialog", { name: "放弃未保存修改" }),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "继续编辑" }).click();
  await addLocalDestination(page);
  await dialog.getByRole("button", { name: "保存政策" }).click();
  await expect(dialog).not.toBeVisible();
  expect(f.saves[0].databases.enabled).toBe(false);
  expect(f.saves[0].files[0].path).toBe("/srv/site");
});

test("policy advanced schedule and white fixed workspace", async ({
  page,
}, testInfo) => {
  await policyFixture(page);
  await page.getByRole("button", { name: "添加备份政策", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("combobox", { name: "频率编辑方式", exact: true }),
  ).toHaveText("每天");
  await expect(dialog.locator(".policy-schedule-preview")).toHaveCount(0);
  await dialog.getByRole("button", { name: "查看五次" }).click();
  await expect(dialog.locator(".policy-schedule-preview li")).toHaveCount(5);
  await dialog.getByRole("button", { name: "收起计划时间" }).click();
  await dialog.getByRole("button", { name: "高级编辑", exact: true }).click();
  await dialog.getByLabel("Cron 表达式", { exact: true }).fill("*/10 * * * *");
  await expect(dialog.locator(".policy-cron-summary code")).toHaveText(
    "*/10 * * * *",
  );
  await choosePolicyOption(page, "高级编辑方式", "五字段高级表单");
  await expect(dialog.locator(".policy-cron-fields")).toBeVisible();
  await dialog.getByRole("button", { name: "收起高级编辑" }).click();
  await expect(dialog.locator(".policy-cron-fields")).toHaveCount(0);
  await expect(dialog.locator(".policy-cron-summary code")).toHaveText(
    "*/10 * * * *",
  );
  await dialog.getByRole("tab", { name: "文件与目录" }).click();
  for (const path of ["/srv/a", "/srv/b"]) {
    await dialog.getByLabel("添加文件或目录路径").fill(path);
    await dialog.getByRole("button", { name: "添加路径" }).click();
  }
  await expect(
    dialog.getByLabel("排除子目录或文件（相对路径，每行一条）"),
  ).toHaveCount(1);
  const first = dialog
    .locator(".policy-file-source")
    .filter({ hasText: "/srv/a" });
  await first.getByRole("button", { name: "排除规则", exact: true }).click();
  await expect(
    dialog.getByLabel("排除子目录或文件（相对路径，每行一条）"),
  ).toHaveCount(1);
  await page.screenshot({
    path: testInfo.outputPath("policy-compact-files.png"),
    animations: "disabled",
    fullPage: false,
  });
  await dialog.getByRole("tab", { name: "基本设置" }).click();
  await dialog
    .locator('.policy-tab-panel[data-state="active"]')
    .evaluate((el) => {
      el.scrollTop = 0;
    });
  await page.evaluate(() => document.documentElement.classList.add("dark"));
  expect(
    await dialog.evaluate((el) => getComputedStyle(el).backgroundColor),
  ).toBe("rgb(255, 255, 255)");
  await page.screenshot({
    path: testInfo.outputPath("policy-compact-basic.png"),
    animations: "disabled",
    fullPage: false,
  });
});

test("multiple destinations, site sources, rule previews and password confirmation", async ({
  page,
}, testInfo) => {
  const f = await policyFixture(page);
  await page.route("**/api/v1/backups/components", (route) =>
    route.fulfill({
      json: {
        items: [
          {
            name: "gdrive",
            status: "ready",
            version: "3.9.1",
            supported: true,
            accounts: {
              status: "ready",
              items: [
                { name: "backup@example.com", type: "drive", current: true },
              ],
            },
          },
          {
            name: "rclone",
            status: "ready",
            version: "1.75.2",
            supported: true,
            accounts: {
              status: "ready",
              items: [{ name: "archive", type: "s3" }],
            },
          },
        ],
      },
    }),
  );
  await page.getByRole("button", { name: "添加备份政策", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("名称", { exact: true }).fill("网站多位置备份");
  await choosePolicyOption(page, "压缩密码", "设置新密码");
  await dialog
    .getByLabel("新压缩密码", { exact: true })
    .fill("archive-private");
  await dialog.getByLabel("确认压缩密码", { exact: true }).fill("wrong");
  await dialog.getByRole("button", { name: "显示密码", exact: true }).click();
  await expect(
    dialog.getByLabel("新压缩密码", { exact: true }),
  ).toHaveAttribute("type", "text");
  await expect(
    dialog.getByLabel("确认压缩密码", { exact: true }),
  ).toHaveAttribute("type", "text");
  await dialog.getByRole("button", { name: "隐藏密码", exact: true }).click();
  await dialog.getByRole("button", { name: "保存政策", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText(
    "两次输入的压缩密码不一致",
  );
  expect(f.saves).toHaveLength(0);
  await dialog
    .getByLabel("确认压缩密码", { exact: true })
    .fill("archive-private");
  await addLocalDestination(page, "/var/backups/project");
  for (const kind of ["gdrive", "rclone"]) {
    await dialog
      .getByRole("button", { name: "添加存储位置", exact: true })
      .click();
    const group = dialog.getByRole("group", {
      name: `存储位置 ${kind === "gdrive" ? 2 : 3}`,
      exact: true,
    });
    await group
      .getByRole("combobox", { name: "存储类型", exact: true })
      .click();
    await page.getByRole("option", { name: kind, exact: true }).click();
    await group
      .getByRole("combobox", {
        name: kind === "gdrive" ? "gdrive 账户" : "rclone 远程",
        exact: true,
      })
      .click();
    await page
      .getByRole("option", {
        name: kind === "gdrive" ? "backup@example.com" : "archive",
        exact: true,
      })
      .click();
    if (kind === "gdrive")
      await group.getByLabel("文件夹 ID", { exact: true }).fill("root");
    else
      await group
        .getByLabel("远程内路径", { exact: true })
        .fill("backups/project");
  }
  await page.screenshot({
    path: testInfo.outputPath("backup-policy-storage.png"),
    animations: "disabled",
  });
  await dialog.getByRole("tab", { name: "文件与目录", exact: true }).click();
  await dialog.getByRole("button", { name: "从网站添加", exact: true }).click();
  await dialog
    .getByRole("checkbox", {
      name: "选择网站目录 app.conf /srv/project",
      exact: true,
    })
    .check();
  await dialog
    .getByRole("checkbox", {
      name: "选择网站目录 shared.conf /srv/project",
      exact: true,
    })
    .check();
  await expect(
    dialog.getByRole("checkbox", {
      name: "选择网站目录 dynamic.conf $dynamic",
      exact: true,
    }),
  ).toBeDisabled();
  await dialog
    .getByRole("button", { name: "添加所选目录", exact: true })
    .click();
  await expect(dialog.locator(".policy-file-source")).toHaveCount(1);
  await expect(dialog.locator(".policy-file-identity")).toContainText(
    "/srv/project",
  );
  await dialog
    .getByLabel("排除子目录或文件（相对路径，每行一条）", { exact: true })
    .fill("cache/\nno-match/**");
  await expect(dialog.getByLabel("排除规则预览")).toContainText(
    "命中 3 个文件",
  );
  await expect(dialog.getByLabel("排除规则预览")).toContainText(
    "cache/first.log",
  );
  await expect(dialog.getByLabel("排除规则预览")).toContainText("未命中");
  await dialog.getByLabel("添加文件或目录路径").fill("/srv/project/public");
  await dialog.getByRole("button", { name: "添加路径", exact: true }).click();
  await expect(
    dialog.getByText("来源存在父子范围重叠，可能重复备份", { exact: true }),
  ).toBeVisible();
  await dialog.getByRole("tab", { name: "存储位置", exact: true }).click();
  await dialog
    .getByLabel("本地目录", { exact: true })
    .fill("/srv/project/backups");
  await expect(
    dialog.getByText(/本地存储位置不能位于备份来源内部/),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "保存政策", exact: true }).click();
  expect(f.saves).toHaveLength(0);
  await dialog
    .getByLabel("本地目录", { exact: true })
    .fill("/var/backups/project");
  await dialog.getByRole("button", { name: "保存政策", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(f.saves[0].destinations).toEqual([
    { type: "local", path: "/var/backups/project" },
    { type: "gdrive", account: "backup@example.com", folder_id: "root" },
    { type: "rclone", remote: "archive", path: "backups/project" },
  ]);
  expect(f.saves[0].files).toHaveLength(2);
  expect(f.saves[0]).not.toHaveProperty("confirmation");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
});

test("copy is an unsaved disabled draft with explicit secret handling and legacy destinations remain editable", async ({
  page,
}) => {
  const f = await policyFixture(page);
  await page.getByRole("button", { name: "添加备份政策", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("名称", { exact: true }).fill("原政策");
  await dialog.getByRole("switch", { name: "启用政策", exact: true }).uncheck();
  await choosePolicyOption(page, "压缩密码", "设置新密码");
  await dialog.getByLabel("新压缩密码", { exact: true }).fill("source-private");
  await dialog
    .getByLabel("确认压缩密码", { exact: true })
    .fill("source-private");
  await dialog.getByRole("button", { name: "保存政策", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const source = [...f.policies.values()][0];
  // An older record may have no destinations field at all.
  delete (source as Partial<Policy>).destinations;
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  await expect(dialog.getByLabel("名称", { exact: true })).toHaveValue(
    "原政策",
  );
  await dialog.getByRole("tab", { name: "存储位置", exact: true }).click();
  await expect(dialog.getByText(/可保存为停用草稿/)).toBeVisible();
  await dialog.getByRole("button", { name: "保存政策", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await page.getByRole("button", { name: "复制", exact: true }).click();
  await expect(
    dialog.getByRole("heading", { name: "复制备份政策", exact: true }),
  ).toBeVisible();
  await expect(dialog.getByLabel("名称", { exact: true })).toHaveValue(
    "原政策（副本）",
  );
  await expect(
    dialog.getByRole("switch", { name: "启用政策", exact: true }),
  ).not.toBeChecked();
  await expect(dialog.getByLabel("新压缩密码", { exact: true })).toHaveCount(0);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(f.policies.size).toBe(1);
  await page.getByRole("button", { name: "复制", exact: true }).click();
  await expect(
    dialog.getByRole("heading", { name: "复制备份政策", exact: true }),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "保存政策", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText(
    "请选择复制后的压缩密码处理方式",
  );
  await choosePolicyOption(page, "压缩密码", "沿用原政策密码");
  await dialog.getByRole("button", { name: "保存政策", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(f.policies.size).toBe(2);
  const request = f.saves.at(-1)!;
  expect(request.enabled).toBe(false);
  expect(request.id).toBe("");
  expect(request.password).toBe("");
  expect(request.password_action).toBe("keep");
  expect(request.copy_from?.id).toBe(source.id);
  expect(JSON.stringify([...f.policies.values()])).not.toContain(
    "source-private",
  );
});

test("destination feedback is inline, delayed for empty rows, and error tabs locate the field", async ({
  page,
}, testInfo) => {
  const f = await policyFixture(page);
  await page.route("**/api/v1/backups/policies/validate-paths", (route) => {
    const { destinations } = route.request().postDataJSON();
    const errors = destinations.flatMap(
      (d: { type: string; path: string }, index: number) =>
        d.type === "local" && (!d.path || !d.path.startsWith("/"))
          ? [
              {
                field: "destinations",
                index,
                control: "path",
                message: "组件安装参数无效：本地存储位置须为规范的绝对目录",
              },
            ]
          : [],
    );
    return route.fulfill({ json: { errors, warnings: [] } });
  });
  await page.getByRole("button", { name: "添加备份政策", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("名称", { exact: true }).fill("字段提示布局");
  await dialog.getByRole("tab", { name: "存储位置", exact: true }).click();
  await expect(dialog.locator(".form-error")).toHaveCount(0);
  await dialog
    .getByRole("button", { name: "添加存储位置", exact: true })
    .click();
  const first = dialog.getByRole("group", { name: "存储位置 1", exact: true });
  const input = first.getByLabel("本地目录", { exact: true });
  await expect(first.getByRole("alert")).toHaveCount(0);
  await input.focus();
  await input.press("Tab");
  await expect(first.getByRole("alert")).toHaveText("请填写本地目录");
  await expect(input).toHaveAttribute("aria-invalid", "true");
  await expect(input).toHaveAccessibleDescription("请填写本地目录");
  await expect(
    dialog
      .locator(".policy-tabs-list")
      .getByRole("tab", { name: "存储位置", exact: true })
      .locator(".policy-tab-error"),
  ).toHaveText("1");
  await dialog.getByRole("tab", { name: "基本设置", exact: true }).click();
  await dialog.getByRole("tab", { name: "存储位置", exact: true }).click();
  await expect(input).toBeFocused();
  await input.fill("relative/path");
  await expect(first.getByRole("alert")).toHaveText(
    "请输入规范的绝对路径，例如 /var/backups/websites",
  );
  await page.screenshot({
    path: testInfo.outputPath("backup-policy-inline-error.png"),
    animations: "disabled",
  });
  await input.fill("/var/backups/valid");
  await expect(first.getByRole("alert")).toHaveCount(0);
  await expect(input).toHaveAttribute("aria-invalid", "false");
  await dialog
    .getByRole("button", { name: "添加存储位置", exact: true })
    .click();
  const second = dialog.getByRole("group", { name: "存储位置 2", exact: true });
  await expect(second.getByRole("alert")).toHaveCount(0);
  await dialog.getByRole("button", { name: "保存政策", exact: true }).click();
  await expect(second.getByRole("alert")).toHaveText("请填写本地目录");
  await expect(second.getByLabel("本地目录", { exact: true })).toBeFocused();
  expect(f.saves).toHaveLength(0);
  await second
    .getByLabel("本地目录", { exact: true })
    .fill("/var/backups/another");
  await page.route("**/api/v1/backups/policies", (route) =>
    route.request().method() === "POST"
      ? route.fulfill({
          status: 502,
          json: { error: "备份服务暂不可用，请稍后重试" },
        })
      : route.fallback(),
  );
  await dialog.getByRole("button", { name: "保存政策", exact: true }).click();
  await expect(
    dialog.locator(".policy-footer-zone").getByRole("alert"),
  ).toHaveText("备份服务暂不可用，请稍后重试");
  await expect(dialog.locator(".policy-tabs > .form-error")).toHaveCount(0);
  await expect(dialog).not.toContainText("组件安装参数无效");
  expect(
    await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth),
  ).toBeTruthy();
});
