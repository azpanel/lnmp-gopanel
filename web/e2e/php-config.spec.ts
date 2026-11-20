import { test, expect } from "./fixtures";

function snapshot() {
  const source = "/usr/local/php/etc/php.ini";
  const values: Record<string, string> = {
    upload_max_filesize: "50M",
    post_max_size: "50M",
    memory_limit: "128M",
    max_execution_time: "300",
    max_input_time: "60",
    "date.timezone": "PRC",
    display_errors: "Off",
  };
  return {
    revision: "a".repeat(64),
    path: source,
    editable: true,
    message: "修改全局 PHP 配置，保存后自动重载 PHP-FPM。",
    fields: Object.fromEntries(
      Object.entries(values).map(([key, value]) => [
        key,
        { value, source, editable: true },
      ]),
    ),
    disable_functions: ["exec", "shell_exec"],
    disable_functions_source: source,
    disable_functions_editable: true,
    pools: [
      {
        pool: "www",
        source: "/usr/local/php/etc/php-fpm.conf",
        values: { disable_functions: "exec,system", memory_limit: "64M" },
        disable_functions: ["exec", "system"],
      },
    ],
  };
}

test("PHP configuration drafts, function list editing, rollback and save", async ({
  page,
}, testInfo) => {
  let current = snapshot();
  current.disable_functions = ["exec", "Shell_Exec"];
  let saves = 0;
  let fail = true;
  let failRead = true;
  await page.route("**/api/v1/services", (route) =>
    route.fulfill({
      json: [
        {
          name: "php-fpm",
          state: "running",
          manager: "init",
          actions: ["start", "stop", "restart"],
          message: "服务运行中",
          checked_at: new Date().toISOString(),
          busy: false,
          version: "8.3.36",
        },
      ],
    }),
  );
  await page.route("**/api/v1/services/php-fpm/config", async (route) => {
    if (route.request().method() === "GET") {
      if (failRead)
        return route.fulfill({
          status: 503,
          json: { error: "配置来源暂不可读取" },
        });
      return route.fulfill({ json: current });
    }
    saves++;
    expect(route.request().headers()["x-csrf-token"]).toBeTruthy();
    const body = route.request().postDataJSON();
    expect(body.revision).toBe(current.revision);
    if (fail)
      return route.fulfill({
        status: 409,
        json: { error: "PHP-FPM 重载失败；已恢复原配置" },
      });
    for (const [key, value] of Object.entries(body.fields ?? {}))
      current.fields[key].value = value as string;
    if (body.disable_functions)
      current.disable_functions = body.disable_functions;
    current.revision = "b".repeat(64);
    return route.fulfill({
      json: { ...current, message: "PHP 配置已保存，PHP-FPM 已重载" },
    });
  });
  await page.goto("/services");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await page.getByRole("button", { name: "配置管理", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("alert")).toContainText("配置来源暂不可读取");
  failRead = false;
  await dialog.getByRole("button", { name: "重新读取", exact: true }).click();
  const save = dialog.getByRole("button", { name: "保存并重载", exact: true });
  await expect(dialog.getByLabel("脚本内存限制", { exact: true })).toHaveValue(
    "128M",
  );
  await expect(save).toBeDisabled();
  await expect(
    dialog.getByRole("heading", { name: "PHP-FPM 配置管理" }),
  ).toHaveCSS("font-size", "16px");
  await expect(dialog.getByLabel("脚本内存限制", { exact: true })).toHaveCSS(
    "height",
    "32px",
  );
  await expect(dialog.getByLabel("脚本内存限制", { exact: true })).toHaveCSS(
    "font-size",
    "14px",
  );
  await expect(dialog.locator('label[for="php-memory_limit"]')).toHaveCSS(
    "font-size",
    "13px",
  );
  expect(
    await dialog
      .getByLabel("脚本内存限制", { exact: true })
      .evaluate(
        (element) => getComputedStyle(element, "::placeholder").fontSize,
      ),
  ).toBe("14px");
  await expect(
    dialog.locator(".php-config-field-heading label > svg"),
  ).toHaveCount(6);
  await expect(dialog.getByRole("switch").locator("svg")).toHaveCount(0);
  await dialog
    .getByRole("button", { name: "脚本内存限制说明", exact: true })
    .click();
  await expect(page.getByRole("tooltip")).toContainText("memory_limit");
  await expect(page.getByRole("tooltip")).toContainText(
    "来源：/usr/local/php/etc/php.ini",
  );
  await page.keyboard.press("Escape");
  await expect(page.getByRole("tooltip")).toHaveCount(0);
  await expect(dialog).toBeVisible();
  await dialog.getByLabel("脚本内存限制", { exact: true }).fill("512M");
  await dialog.getByRole("button", { name: "重新读取", exact: true }).click();
  await dialog.getByRole("button", { name: "放弃并读取", exact: true }).click();
  await expect(dialog.getByLabel("脚本内存限制", { exact: true })).toHaveValue(
    "128M",
  );
  await expect(save).toBeDisabled();
  await dialog.getByLabel("脚本内存限制", { exact: true }).fill("256M");
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();

  const discardDialog = page.getByRole("alertdialog", {
    name: "放弃未保存的修改？",
    exact: true,
  });
  await expect(discardDialog).toContainText("有未保存的修改，确定放弃并关闭？");
  await expect(
    discardDialog.getByRole("button", { name: "继续编辑", exact: true }),
  ).toBeFocused();
  await expect(dialog.getByRole("alert")).toHaveCount(0);
  expect(saves).toBe(0);
  await discardDialog
    .getByRole("button", { name: "继续编辑", exact: true })
    .click();
  await expect(discardDialog).toHaveCount(0);
  await expect(dialog.getByLabel("脚本内存限制", { exact: true })).toHaveValue(
    "256M",
  );
  await expect(
    dialog.getByRole("button", { name: "关闭", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(discardDialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(discardDialog).toHaveCount(0);
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel("脚本内存限制", { exact: true })).toHaveValue(
    "256M",
  );
  await dialog.getByRole("tab", { name: /禁用函数/ }).click();
  const list = dialog.getByRole("list", { name: "全局禁用函数" });
  await expect(list.getByRole("listitem")).toHaveCount(2);
  await expect(list.locator('[data-slot="chip"]')).toHaveCount(2);
  const search = dialog.getByLabel("搜索禁用函数", { exact: true });
  await search.fill("shell");
  await expect(list.getByRole("listitem")).toHaveCount(1);
  await expect(
    list.getByRole("button", { name: "Shell_Exec 函数说明", exact: true }),
  ).toBeVisible();
  await search.clear();
  const execHelp = list.getByRole("button", {
    name: "exec 函数说明",
    exact: true,
  });
  if (testInfo.project.name === "mobile") await execHelp.tap();
  else await execHelp.hover();
  await expect(page.getByRole("tooltip")).toContainText("执行外部命令");
  expect(saves).toBe(0);
  await page.keyboard.press("Escape");
  await expect(list.getByRole("listitem")).toHaveCount(2);
  await dialog
    .getByRole("button", { name: "禁用函数配置说明", exact: true })
    .focus();
  await expect(page.getByRole("tooltip")).toContainText(
    "来源：/usr/local/php/etc/php.ini",
  );
  await page.keyboard.press("Escape");
  await dialog.getByLabel("添加禁用函数", { exact: true }).fill("system;bad");
  await dialog.getByRole("button", { name: "添加", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("有效的函数名");
  await dialog
    .getByLabel("添加禁用函数", { exact: true })
    .fill("SYSTEM,exec,system,shell_exec");
  await dialog.getByRole("button", { name: "添加", exact: true }).click();
  await expect(list.getByRole("listitem")).toHaveCount(3);
  await expect(
    list.getByRole("button", { name: "Shell_Exec 函数说明", exact: true }),
  ).toHaveCount(0);
  await expect(
    list.getByRole("button", { name: "shell_exec 函数说明", exact: true }),
  ).toBeVisible();
  await dialog.getByLabel("搜索禁用函数", { exact: true }).fill("shell");
  await dialog.getByRole("tab", { name: /禁用函数/ }).focus();
  await page.keyboard.press("ArrowLeft");
  await expect(
    dialog.getByRole("tab", { name: "常用配置", exact: true }),
  ).toHaveAttribute("data-state", "active");
  await expect(dialog.getByLabel("脚本内存限制", { exact: true })).toHaveValue(
    "256M",
  );
  await page.keyboard.press("ArrowRight");
  await expect(dialog.getByRole("tab", { name: /禁用函数/ })).toHaveAttribute(
    "data-state",
    "active",
  );
  await expect(dialog.getByLabel("搜索禁用函数", { exact: true })).toHaveValue(
    "shell",
  );
  await expect(list.getByRole("listitem")).toHaveCount(1);
  await dialog
    .getByRole("button", { name: "删除 shell_exec", exact: true })
    .click();
  await expect(dialog.getByText("没有匹配的函数。")).toBeVisible();
  await dialog.getByLabel("搜索禁用函数", { exact: true }).fill("");
  await dialog.locator("summary").click();
  await expect(
    dialog
      .getByRole("list", { name: "www 进程池禁用函数" })
      .getByRole("listitem"),
  ).toHaveCount(2);
  expect(saves).toBe(0);
  await save.click();
  await expect(dialog.getByRole("alert")).toContainText("已恢复原配置");
  await expect(list.getByRole("listitem")).toHaveCount(2);
  await dialog.getByRole("tab", { name: "常用配置", exact: true }).click();
  await expect(dialog.getByLabel("脚本内存限制", { exact: true })).toHaveValue(
    "256M",
  );
  fail = false;
  await save.click();
  await expect(save).toBeDisabled();
  await expect(dialog.getByRole("alert")).toHaveCount(0);
  expect(current.fields.memory_limit.value).toBe("256M");
  expect(current.disable_functions).toEqual(["exec", "system"]);
  await dialog.getByRole("tab", { name: /禁用函数/ }).click();
  await dialog.getByRole("button", { name: "删除 exec", exact: true }).click();
  await dialog
    .getByRole("button", { name: "删除 system", exact: true })
    .click();
  await expect(
    dialog.getByText("未禁用任何函数。", { exact: true }),
  ).toBeVisible();
  await save.click();
  await expect(save).toBeDisabled();
  expect(current.disable_functions).toEqual([]);
  await expect(
    dialog
      .getByRole("list", { name: "www 进程池禁用函数" })
      .getByRole("listitem"),
  ).toHaveCount(2);
  const bounds = await dialog.evaluate((element) => ({
    width: element.getBoundingClientRect().width,
    viewport: window.innerWidth,
    scrollWidth: element.scrollWidth,
    clientWidth: element.clientWidth,
  }));
  expect(bounds.width).toBeLessThanOrEqual(bounds.viewport);
  expect(bounds.scrollWidth).toBeLessThanOrEqual(bounds.clientWidth + 1);
  await page.screenshot({
    path: testInfo.outputPath("php-config.png"),
    fullPage: true,
  });
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(saves).toBe(3);
  await page.getByRole("button", { name: "配置管理", exact: true }).click();
  await dialog.getByLabel("脚本内存限制", { exact: true }).fill("512M");
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();

  await expect(discardDialog).toBeVisible();
  await discardDialog
    .getByRole("button", { name: "放弃修改", exact: true })
    .click();
  await expect(discardDialog).toHaveCount(0);
  await expect(dialog).toHaveCount(0);
  expect(saves).toBe(3);
  await page.getByRole("button", { name: "配置管理", exact: true }).click();
  await expect(dialog.getByLabel("脚本内存限制", { exact: true })).toHaveValue(
    "256M",
  );
});

test("PHP stopped service and scanned INI overrides remain read-only", async ({
  page,
}) => {
  const data = snapshot();
  let updates = 0;
  data.editable = false;
  data.message = "PHP-FPM 未运行，请启动服务后保存配置。";
  data.fields.memory_limit = {
    value: "64M",
    editable: false,
    source: "/usr/local/php/conf.d/99.ini",
  };
  data.disable_functions_editable = false;
  await page.route("**/api/v1/services", (route) =>
    route.fulfill({
      json: [
        {
          name: "php-fpm",
          state: "stopped",
          manager: "init",
          actions: ["start", "stop", "restart"],
          message: "已停止",
          checked_at: new Date().toISOString(),
          busy: false,
          version: "8.3.36",
        },
      ],
    }),
  );
  await page.route("**/api/v1/services/php-fpm/config", (route) => {
    if (route.request().method() !== "GET") updates++;
    return route.fulfill({ json: data });
  });
  await page.goto("/services");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await page.getByRole("button", { name: "配置管理", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByLabel("脚本内存限制", { exact: true }),
  ).toBeDisabled();
  await expect(
    dialog.getByRole("button", { name: "保存并重载", exact: true }),
  ).toBeDisabled();
  await expect(dialog.getByText(data.message, { exact: true })).toBeVisible();
  await dialog
    .getByRole("button", { name: "脚本内存限制说明", exact: true })
    .click();
  await expect(page.getByRole("tooltip")).toContainText(
    "/usr/local/php/conf.d/99.ini",
  );
  await page.keyboard.press("Escape");
  await dialog.getByRole("tab", { name: /禁用函数/ }).click();
  await expect(
    dialog.getByLabel("添加禁用函数", { exact: true }),
  ).toBeDisabled();
  await expect(
    dialog.getByRole("button", { name: "删除 exec", exact: true }),
  ).toBeDisabled();
  await dialog
    .getByRole("list", { name: "全局禁用函数", exact: true })
    .getByRole("button", { name: "exec 函数说明", exact: true })
    .click();
  await expect(page.getByRole("tooltip")).toContainText("执行外部命令");
  await page.keyboard.press("Escape");
  data.editable = true;
  await dialog.getByRole("button", { name: "重新读取", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "删除 exec", exact: true }),
  ).toBeDisabled();
  await dialog.getByRole("tab", { name: "常用配置", exact: true }).click();
  await expect(
    dialog.getByLabel("脚本内存限制", { exact: true }),
  ).toBeDisabled();
  await expect(
    dialog.getByLabel("脚本执行超时", { exact: true }),
  ).toBeEnabled();
  expect(updates).toBe(0);
});

test("PHP default function chips explain legacy names and wrap on small screens", async ({
  page,
}, testInfo) => {
  const data = snapshot();
  const names = [
    "passthru",
    "exec",
    "system",
    "chroot",
    "chgrp",
    "chown",
    "shell_exec",
    "proc_open",
    "proc_get_status",
    "popen",
    "ini_alter",
    "ini_restore",
    "dl",
    "openlog",
    "syslog",
    "readlink",
    "symlink",
    "popepassthru",
    "stream_socket_server",
  ];
  const custom = `custom_${"long_name_".repeat(11)}`;
  data.disable_functions = [...names, custom, "__proto__"];
  let updates = 0;
  await page.route("**/api/v1/services", (route) =>
    route.fulfill({
      json: [
        {
          name: "php-fpm",
          state: "running",
          manager: "init",
          actions: ["start", "stop", "restart"],
          message: "服务运行中",
          checked_at: new Date().toISOString(),
          busy: false,
          version: "8.3.36",
        },
      ],
    }),
  );
  await page.route("**/api/v1/services/php-fpm/config", (route) => {
    if (route.request().method() !== "GET") updates++;
    return route.fulfill({ json: data });
  });
  await page.goto("/services");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await page.getByRole("button", { name: "配置管理", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByLabel("脚本内存限制", { exact: true }),
  ).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("php-config-common.png") });
  await dialog.getByRole("tab", { name: /禁用函数/ }).click();
  const list = dialog.getByRole("list", { name: "全局禁用函数", exact: true });
  await expect(list.locator('[data-slot="chip"]')).toHaveCount(21);
  const rows = await list
    .getByRole("listitem")
    .evaluateAll(
      (items) =>
        new Set(
          items.map((item) => Math.round(item.getBoundingClientRect().top)),
        ).size,
    );
  expect(rows).toBeGreaterThan(1);
  for (const name of names) {
    const trigger = list.getByRole("button", {
      name: `${name} 函数说明`,
      exact: true,
    });
    if (testInfo.project.name === "mobile") await trigger.tap();
    else await trigger.click();
    await expect(page.getByRole("tooltip")).not.toContainText("暂未收录");
    if (name === "popepassthru")
      await expect(page.getByRole("tooltip")).toContainText(
        "未识别的标准 PHP 函数名",
      );
    await page.keyboard.press("Escape");
  }
  for (const name of [custom, "__proto__"]) {
    const trigger = list.getByRole("button", {
      name: `${name} 函数说明`,
      exact: true,
    });
    if (testInfo.project.name === "mobile") await trigger.tap();
    else await trigger.click();
    await expect(page.getByRole("tooltip")).toContainText(
      "暂未收录此函数的用途说明",
    );
    await page.keyboard.press("Escape");
  }
  await expect(list.locator('[data-slot="chip"]')).toHaveCount(21);
  expect(updates).toBe(0);
  const bounds = await dialog.evaluate((element) => ({
    scrollWidth: element.scrollWidth,
    clientWidth: element.clientWidth,
  }));
  expect(bounds.scrollWidth).toBeLessThanOrEqual(bounds.clientWidth + 1);
  await page.screenshot({ path: testInfo.outputPath("php-config-chips.png") });
});
