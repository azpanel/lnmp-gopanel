import { test, expect } from "@playwright/test";
import type { VhostJob, VhostRenewItem } from "../src/lib/vhost-create";

test("select sites and renew serially with per-site CA, live logs and failed retry", async ({
  page,
}, testInfo) => {
  test.setTimeout(60000);
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const now = new Date().toISOString();
  const sites = ["a.example.com", "b.example.com", "disabled.other.com"].map(
    (domain, index) => ({
      name: `${domain}.conf`,
      filename: `${domain}.conf${index === 2 ? ".disabled" : ""}`,
      enabled: index !== 2,
      domains: [domain],
      listens: ["80", "443 ssl"],
      roots: [],
      proxies: [],
      warnings: [],
      modified: now,
      certificates: [
        {
          path: `/ssl/${domain}/fullchain.pem`,
          issuer: index === 0 ? "O=ZeroSSL" : "O=Let's Encrypt",
          subject: `CN=${domain}`,
          dns_names: [domain],
          not_before: now,
          not_after: "2027-01-01T00:00:00Z",
          days_remaining: 60,
          serial: "42",
          fingerprint: "a".repeat(64),
          status: "valid",
        },
      ],
    }),
  );
  let current: VhostJob | null = null;
  let finish = false;
  let submissions = 0;
  let streams = 0;
  let polls = 0;
  const requests: VhostRenewItem[][] = [];
  await page.route("**/api/v1/vhosts/count", (route) =>
    route.fulfill({ json: { count: 3 } }),
  );
  await page.route("**/api/v1/vhosts/screenshots", (route) =>
    route.fulfill({ json: { browser_available: false, items: {} } }),
  );
  await page.route("**/api/v1/vhosts", (route) =>
    route.fulfill({
      json: { status: "ready", sites, usage: {}, updated_at: now },
    }),
  );
  await page.route("**/api/v1/vhosts/jobs", (route) =>
    route.fulfill({ json: current ? [{ ...current, lines: [] }] : [] }),
  );
  await page.route("**/api/v1/vhosts/jobs/*", (route) => {
    polls++;
    return route.fulfill({ json: current });
  });
  await page.route("**/api/v1/vhosts/ssl/renew/preview", (route) =>
    route.fulfill({
      json: route
        .request()
        .postDataJSON()
        .names.map((name: string) => ({
          name,
          domains: sites.find((site) => site.name === name)!.domains,
          provider: name.startsWith("a.") ? "zerossl" : "letsencrypt",
          email: "",
          eligible: !name.startsWith("disabled"),
          reason: name.startsWith("disabled")
            ? "站点已停用，请先启用"
            : undefined,
        })),
    }),
  );
  await page.route("**/api/v1/vhosts/ssl/renew", (route) => {
    const body = route.request().postDataJSON();
    expect(route.request().headers()["x-csrf-token"]).toBeTruthy();
    submissions++;
    requests.push(body.items);
    current = {
      id: body.request_id,
      kind: "ssl-renew",
      name: "批量续签 SSL",
      status: "running",
      phase: "ssl",
      created_at: now,
      updated_at: now,
      lines: [],
      items: body.items.map((item: VhostRenewItem, index: number) => ({
        ...item,
        status: index === 0 ? "running" : "queued",
        phase: "ssl",
      })),
    };
    return route.fulfill({ status: 202, json: current });
  });
  await page.route("**/api/v1/vhosts/jobs/*/events", (route) => {
    streams++;
    if (!finish) return route.abort(); // Exercise polling fallback before reconnecting.
    const job = current!;
    current = {
      ...job,
      status: submissions === 1 ? "partial" : "succeeded",
      phase: "done",
      completed: job.items!.length,
      succeeded: submissions === 1 ? 1 : job.items!.length,
      failed: submissions === 1 ? 1 : 0,
      updated_at: new Date().toISOString(),
      items: job.items!.map((item, index) => ({
        ...item,
        status: submissions === 1 && index === 0 ? "failed" : "succeeded",
        error: submissions === 1 && index === 0 ? "CA 验证失败" : undefined,
      })),
      lines: [
        {
          id: 1,
          name: job.items![0].name,
          time: now,
          phase: "ssl",
          text:
            submissions === 1
              ? "first order failed"
              : "retry signed successfully",
        },
        ...(job.items!.length > 1
          ? [
              {
                id: 2,
                name: job.items![1].name,
                time: now,
                phase: "deploy",
                text: "second order deployed",
              },
            ]
          : []),
      ],
    };
    return route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: `event: snapshot\nid: ${current.lines.at(-1)!.id}\ndata: ${JSON.stringify(current)}\n\nevent: complete\ndata: {}\n\n`,
    });
  });
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(
    page.getByRole("heading", { name: "系统概览", exact: true }),
  ).toBeVisible();
  if (testInfo.project.name === "mobile")
    await page.getByRole("button", { name: "打开导航" }).click();
  await page.getByRole("link", { name: "虚拟主机", exact: true }).click();
  await page.getByRole("button", { name: "表格视图", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "提前续签 SSL", exact: true }),
  ).toBeDisabled();
  await page.getByRole("textbox", { name: "筛选虚拟主机" }).fill("example.com");
  await page.getByRole("checkbox", { name: "全选当前筛选站点" }).check();
  await expect(page.getByText("已选 2 个站点", { exact: true })).toBeVisible();
  await page.getByRole("textbox", { name: "筛选虚拟主机" }).fill("");
  await expect(
    page.getByRole("checkbox", { name: "全选当前筛选站点" }),
  ).toHaveAttribute("data-state", "indeterminate");
  await page
    .getByRole("checkbox", {
      name: "选择站点 disabled.other.com.conf",
      exact: true,
    })
    .check();
  await page
    .getByRole("button", { name: "提前续签 SSL（3）", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("站点已停用，请先启用");
  const first = dialog
    .locator(".renew-config-item")
    .filter({ has: page.getByText("a.example.com", { exact: true }) });
  const second = dialog
    .locator(".renew-config-item")
    .filter({ has: page.getByText("b.example.com", { exact: true }) });
  await first.getByRole("combobox").click();
  await page
    .getByRole("option", { name: "Let's Encrypt", exact: true })
    .click();
  await second.getByRole("combobox").click();
  await page.getByRole("option", { name: "ZeroSSL", exact: true }).click();
  await first.getByLabel("联系邮箱").fill("a@example.com");
  await second.getByLabel("联系邮箱").fill("b@example.com");
  await dialog
    .getByRole("button", { name: "开始逐个续签（2）", exact: true })
    .click();
  await expect(dialog.getByRole("status")).toContainText("已完成 0 / 2");
  expect(requests[0]).toEqual([
    { name: sites[0].name, provider: "letsencrypt", email: "a@example.com" },
    { name: sites[1].name, provider: "zerossl", email: "b@example.com" },
  ]);
  await expect.poll(() => polls).toBeGreaterThan(0);
  await expect.poll(() => streams).toBeGreaterThan(0);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page.reload();
  await expect(
    page.getByRole("button", { name: "查看续签进度", exact: true }),
  ).toBeVisible();
  finish = true;
  await page.getByRole("button", { name: "查看续签进度", exact: true }).click();
  await expect(dialog.getByRole("status")).toContainText("已完成 2 / 2");
  await expect(dialog.getByRole("status")).toContainText("成功 1 · 失败 1");
  await expect(dialog.getByLabel("SSL 签发输出")).toContainText(
    "first order failed",
  );
  await expect(dialog.getByLabel("SSL 签发输出")).toContainText(
    "second order deployed",
  );
  await dialog.getByRole("combobox", { name: "签发输出", exact: true }).click();
  await page
    .getByRole("option", { name: "b.example.com", exact: true })
    .click();
  await expect(dialog.getByLabel("SSL 签发输出")).not.toContainText(
    "first order failed",
  );
  await dialog.getByRole("button", { name: "重试失败项", exact: true }).click();
  await expect(dialog.getByLabel("联系邮箱")).toHaveValue("a@example.com");
  await expect(dialog.getByRole("combobox")).toContainText("Let's Encrypt");
  await dialog
    .getByRole("button", { name: "开始逐个续签（1）", exact: true })
    .click();
  await expect(dialog.getByRole("status")).toContainText("成功 1 · 失败 0");
  expect(requests[1]).toEqual([requests[0][0]]);
  expect(submissions).toBe(2);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  expect(errors).toEqual([]);
});
