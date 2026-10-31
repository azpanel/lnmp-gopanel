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
      const { password: _password, password_action, ...data } = body;
      const policy = {
        ...data,
        id: "a".repeat(32),
        revision: 1,
        compression: {
          ...data.compression,
          has_password: password_action === "replace",
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
      },
    });
  });
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

  await dialog.getByRole("tab", { name: "数据库", exact: true }).click();
  await expect(dialog.getByText(/无法认证本机数据库管理员/)).toBeVisible();
  await dialog.getByLabel("数据库管理员密码").fill("db-only-secret");
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
  await page.getByRole("button", { name: "查看", exact: true }).click();
  await expect(dialog.getByLabel("名称", { exact: true })).toBeDisabled();
  await expect(
    dialog.getByRole("slider", { name: "数据库备份保留份数" }),
  ).toBeDisabled();
  await expect(dialog.getByLabel("备注", { exact: true })).toHaveValue(
    "编辑后的备注",
  );
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
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
