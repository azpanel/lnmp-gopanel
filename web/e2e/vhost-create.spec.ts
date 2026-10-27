import { expect, test } from "@playwright/test";
import type {
  VhostCreateInput,
  VhostCreateOptions,
  VhostJob,
} from "../src/lib/vhost-create";

test("create a site with DNS warnings, wildcard selection, live output and SSL retry", async ({
  page,
}, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const now = new Date().toISOString();
  const cert = {
    path: "/root/.acme.sh/example.com_ecc/fullchain.cer",
    issuer: "CN=Test Wildcard Issuer",
    subject: "CN=*.example.com",
    dns_names: ["*.example.com", "example.com"],
    not_before: now,
    not_after: "2030-01-01T00:00:00Z",
    days_remaining: 90,
    serial: "42",
    fingerprint: "a".repeat(64),
    status: "valid" as const,
  };
  const options: VhostCreateOptions = {
    php: [
      { id: "default", label: "默认 PHP (8.3.0)", path_info_available: true },
    ],
    rewrites: ["none", "wordpress"],
    certificates: [
      {
        id: "a".repeat(64),
        cert_file: cert.path,
        key_file: "/root/.acme.sh/example.com_ecc/example.com.key",
        certificate: cert,
        usable: true,
      },
      {
        id: "b".repeat(64),
        cert_file: "/root/.acme.sh/other.com/fullchain.cer",
        key_file: "/root/.acme.sh/other.com/other.com.key",
        certificate: { ...cert, dns_names: ["*.other.com"] },
        usable: true,
      },
    ],
    acme_available: true,
    database_available: true,
    database_auth_available: false,
    warnings: [],
  };
  let current: VhostJob | null = null;
  let submitted: VhostCreateInput | null = null;
  let retryBody: { name: string; ssl: { mode: string } } | null = null;
  let submissions = 0;
  await page.route("**/api/v1/vhosts/count", (route) =>
    route.fulfill({ json: { count: 0 } }),
  );
  await page.route("**/api/v1/vhosts/screenshots", (route) =>
    route.fulfill({ json: { browser_available: false, items: {} } }),
  );
  await page.route("**/api/v1/vhosts/options", (route) =>
    route.fulfill({ json: options }),
  );
  await page.route("**/api/v1/vhosts/jobs", (route) =>
    route.fulfill({ json: current ? [{ ...current, lines: [] }] : [] }),
  );
  await page.route("**/api/v1/vhosts/jobs/*", (route) =>
    route.fulfill({ json: current }),
  );
  await page.route("**/api/v1/vhosts/check-dns", (route) =>
    route.fulfill({
      json: {
        server_ips: ["192.0.2.1"],
        warnings: [
          "部分公网 IP 无法探测，未匹配的解析可能无法确认；NAT 可在配置中补充公网 IP",
        ],
        records: route
          .request()
          .postDataJSON()
          .domains.map((domain: string) => ({
            domain,
            addresses: ["192.0.2.2"],
            matched: [],
            status: "mismatch",
          })),
      },
    }),
  );
  function start(requestID: string) {
    current = {
      id: requestID,
      name: "app.example.com.conf",
      status: "running",
      phase: "ssl",
      created_at: now,
      updated_at: now,
      lines: [],
    };
    return current;
  }
  await page.route("**/api/v1/vhosts", (route) => {
    if (route.request().method() === "POST") {
      submitted = route.request().postDataJSON() as VhostCreateInput;
      expect(route.request().headers()["x-csrf-token"]).toBeTruthy();
      submissions++;
      return route.fulfill({ status: 202, json: start(submitted.request_id) });
    }
    return route.fulfill({
      json: { status: "ready", sites: [], usage: {}, updated_at: now },
    });
  });
  await page.route("**/api/v1/vhosts/ssl/retry", (route) => {
    retryBody = route.request().postDataJSON();
    submissions++;
    return route.fulfill({
      status: 202,
      json: start(route.request().postDataJSON().request_id),
    });
  });
  await page.route("**/api/v1/vhosts/jobs/*/events", (route) => {
    current = {
      ...current!,
      status: submissions === 1 ? "partial" : "succeeded",
      phase: "done",
      updated_at: new Date().toISOString(),
      lines: [
        {
          id: 1,
          time: now,
          phase: "ssl",
          text: "acme.sh: validating app.example.com",
        },
        {
          id: 2,
          time: now,
          phase: "ssl",
          text: submissions === 1 ? "ACME: validation failed" : "SSL enabled",
        },
      ],
      result: {
        name: "app.example.com.conf",
        site_created: true,
        database: "created",
        ssl: submissions === 1 ? "failed" : "enabled",
        message: submissions === 1 ? "HTTP 站点已保留" : "站点与 SSL 已启用",
      },
      error: submissions === 1 ? "证书申请失败，HTTP 站点已保留" : undefined,
    };
    return route.fulfill({
      contentType: "text/event-stream",
      body: `event: snapshot\ndata: ${JSON.stringify(current)}\n\nevent: complete\ndata: {}\n\n`,
    });
  });
  await page.goto("/vhosts");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await page.getByRole("button", { name: "添加站点", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await page.screenshot({
    path: testInfo.outputPath("create-site-sections.png"),
    fullPage: true,
  });
  await dialog.getByLabel("主域名", { exact: true }).fill("app.example.com");
  await dialog.getByLabel("附加域名", { exact: true }).fill("www.example.com");
  const domainBox = (await dialog
    .getByLabel("主域名", { exact: true })
    .boundingBox())!;
  const aliasesBox = (await dialog
    .getByLabel("附加域名", { exact: true })
    .boundingBox())!;
  const dnsBox = (await dialog
    .getByRole("button", { name: "校验解析", exact: true })
    .boundingBox())!;
  expect(Math.abs(domainBox.y - aliasesBox.y)).toBeLessThan(2);
  expect(
    Math.abs(domainBox.y + domainBox.height / 2 - dnsBox.y - dnsBox.height / 2),
  ).toBeLessThan(2);
  expect(dnsBox.x).toBeGreaterThanOrEqual(aliasesBox.x + aliasesBox.width);
  const help = dialog.getByRole("button", {
    name: "解析校验说明",
    exact: true,
  });
  await expect(page.getByRole("tooltip")).toHaveCount(0);
  await help.focus();
  await expect(page.getByRole("tooltip")).toContainText(
    "解析校验仅供参考，未通过仍可继续；CDN 或代理可能显示其他 IP。",
  );
  await page.keyboard.press("Escape");
  await expect(page.getByRole("tooltip")).toHaveCount(0);
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "校验解析" }).click();
  await expect(dialog.getByText("未解析到本机", { exact: true })).toHaveCount(
    2,
  );
  await expect(dialog.locator(".create-dns")).not.toContainText(
    "部分公网 IP 无法探测",
  );
  await expect(dialog.locator(".create-dns")).not.toContainText(
    "解析校验仅供参考",
  );
  await help.click();
  await expect(page.getByRole("tooltip")).toContainText(
    "部分公网 IP 无法探测，未匹配的解析可能无法确认；NAT 可在配置中补充公网 IP",
  );
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
  await dialog.getByLabel("记录访问日志", { exact: true }).check();
  await expect(
    dialog.getByText("保存在 /home/wwwlogs/ 下", { exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByLabel("访问日志文件名", { exact: true }),
  ).toHaveAttribute("placeholder", "app.example.com.log");
  await dialog
    .getByLabel("访问日志文件名", { exact: true })
    .fill("site-access.log");
  await expect(dialog.getByText("创建 ftp", { exact: false })).toHaveCount(0);
  await dialog.getByRole("combobox", { name: "证书来源" }).click();
  await page.getByRole("option", { name: "指定 cert／key 文件" }).click();
  await dialog
    .getByLabel("cert 文件绝对路径（完整证书链）")
    .fill("/ssl/fullchain.pem");
  await dialog.getByLabel("key 文件绝对路径").fill("/ssl/key.pem");
  await dialog.getByRole("combobox", { name: "证书来源" }).click();
  await page.getByRole("option", { name: /已有通配符证书/ }).click();
  await dialog
    .getByRole("combobox", { name: "通配符证书", exact: true })
    .click();
  await expect(page.getByRole("option", { name: /other.com/ })).toHaveAttribute(
    "data-disabled",
    "",
  );
  await page.getByRole("option", { name: /\*\.example.com/ }).click();
  await expect(dialog.getByText(cert.path, { exact: true })).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("create-site.png"),
    fullPage: true,
  });
  expect(
    await dialog.evaluate(
      (element) => element.scrollWidth <= element.clientWidth,
    ),
  ).toBe(true);
  await dialog.getByRole("combobox", { name: "证书来源" }).click();
  await page.getByRole("option", { name: "申请 ZeroSSL" }).click();
  await dialog.getByLabel("联系邮箱").fill("admin@example.com");
  await dialog.getByLabel("创建数据库及同名本机用户").check();
  await dialog.getByLabel("数据库／用户名").fill("site_database");
  const password = dialog.getByLabel("数据库密码", { exact: true });
  const generate = dialog.getByRole("button", {
    name: "随机生成强密码",
    exact: true,
  });
  await generate.click();
  const firstPassword = await password.inputValue();
  await generate.click();
  const generated = await password.inputValue();
  expect(generated).not.toBe(firstPassword);
  expect(generated).toHaveLength(24);
  expect(generated).toMatch(/[A-Z]/);
  expect(generated).toMatch(/[a-z]/);
  expect(generated).toMatch(/[0-9]/);
  expect(generated).toMatch(/[!@#$%*_+\-=]/);
  expect(submissions).toBe(0);
  await expect(password).toHaveAttribute("type", "password");
  await dialog
    .getByRole("button", { name: "显示数据库密码", exact: true })
    .click();
  await expect(password).toHaveAttribute("type", "text");
  await dialog
    .getByRole("button", { name: "隐藏数据库密码", exact: true })
    .click();
  await expect(password).toHaveAttribute("type", "password");
  await dialog.getByLabel(/数据库管理员密码/).fill("Admin-Password-123");
  await page.screenshot({
    path: testInfo.outputPath("create-site-database.png"),
    fullPage: true,
  });
  await dialog.getByRole("button", { name: "创建站点", exact: true }).click();
  await expect(dialog.getByLabel("建站实时输出")).toContainText(
    "acme.sh: validating app.example.com",
  );
  await expect(dialog.getByRole("alert")).toContainText("HTTP 站点已保留");
  expect(submissions).toBe(1);
  expect(submitted).toMatchObject({
    domain: "app.example.com",
    aliases: ["www.example.com"],
    access_log: true,
    log_name: "site-access.log",
    ssl: { mode: "zerossl", redirect: true },
    database: { enabled: true, name: "site_database", password: generated },
  });
  await dialog.getByRole("button", { name: "重试 SSL", exact: true }).click();
  await expect(dialog.getByLabel("主域名", { exact: true })).toHaveCount(0);
  await dialog.getByLabel("联系邮箱").fill("admin@example.com");
  await dialog.getByRole("button", { name: "开始申请" }).click();
  await expect(dialog.getByLabel("建站实时输出")).toContainText("SSL enabled");
  expect(retryBody).toMatchObject({ name: "app.example.com.conf" });
  expect(retryBody).not.toHaveProperty("database");
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page.reload();
  await page.getByRole("button", { name: "最近建站结果" }).click();
  await expect(page.getByLabel("建站实时输出")).toContainText("SSL enabled");
  expect(errors).toEqual([]);
});
