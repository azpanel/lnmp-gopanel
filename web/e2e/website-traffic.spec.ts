import { test, expect } from "./fixtures";

test("website traffic switches period and chart, remembers preferences and keeps stale data", async ({
  page,
}, testInfo) => {
  await page.clock.install();
  let failure = false;
  const periods: string[] = [];
  await page.route("**/api/v1/dashboard/widgets", (route) =>
    route.fulfill({ json: { enabled: ["website_traffic"] } }),
  );
  await page.route("**/api/v1/dashboard/traffic?*", (route) => {
    const period = new URL(route.request().url()).searchParams.get("period")!;
    periods.push(period);
    if (failure)
      return route.fulfill({ status: 503, json: { error: "vnstat 查询失败" } });
    return route.fulfill({
      json: {
        interface: "eth0",
        period,
        updated_at: "2026-11-18T13:30:00+08:00",
        points: Array.from(
          { length: period === "day" ? 30 : 24 },
          (_, index) => ({
            time: new Date(Date.UTC(2026, 10, 18, index)).toISOString(),
            label:
              period === "day"
                ? `2026-11-${String(index + 1).padStart(2, "0")}`
                : `2026-11-18 ${String(index).padStart(2, "0")}:00 +08:00`,
            rx: index === 1 ? null : 1024 * 1024 * (index + 1),
            tx: index === 1 ? null : 512 * 1024 * (index + 1),
          }),
        ),
      },
    });
  });
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  const widget = page.locator('[data-widget="website_traffic"]');
  await expect(widget).toContainText("最近 30 天");
  await expect(widget).toContainText("网卡 eth0");
  await expect(widget.locator(".recharts-line")).toHaveCount(2);
  const toggle = widget.getByRole("switch", { name: "按小时显示流量" });
  const chart = widget.getByRole("combobox", { name: "流量图表类型" });
  await toggle.check();
  await expect(widget).toContainText("最近 24 小时");
  await expect.poll(() => periods.includes("hour")).toBeTruthy();
  await chart.click();
  await page.getByRole("option", { name: "柱状图", exact: true }).click();
  await expect(widget.locator(".recharts-bar")).toHaveCount(2);
  await toggle.uncheck();
  await expect(widget).toContainText("最近 30 天");
  await expect(widget.locator(".recharts-bar")).toHaveCount(2);
  await chart.click();
  await page.getByRole("option", { name: "折线图", exact: true }).click();
  await expect(widget.locator(".recharts-line")).toHaveCount(2);
  await toggle.check();
  await chart.click();
  await page.getByRole("option", { name: "柱状图", exact: true }).click();
  await page.reload();
  await expect(toggle).toBeChecked();
  await expect(chart).toContainText("柱状图");
  await expect(widget.locator(".recharts-bar")).toHaveCount(2);
  failure = true;
  await page.clock.fastForward(61000);
  await expect(widget).toContainText("更新失败，当前显示上次数据");
  await expect(widget.locator(".recharts-bar")).toHaveCount(2);
  failure = false;
  await widget.getByRole("button", { name: "重试", exact: true }).click();
  await expect(widget).not.toContainText("更新失败");
  await widget.scrollIntoViewIfNeeded();
  await expect(chart).toHaveCSS("font-size", "14px");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  await widget.screenshot({
    path: testInfo.outputPath("website-traffic-light.png"),
  });
  await page.evaluate(() => document.documentElement.classList.add("dark"));
  await widget.screenshot({
    path: testInfo.outputPath("website-traffic-dark.png"),
  });
});

test("website traffic shows installation hints and waits for initial history", async ({
  page,
}) => {
  let empty = false;
  await page.route("**/api/v1/dashboard/widgets", (route) =>
    route.fulfill({ json: { enabled: ["website_traffic"] } }),
  );
  await page.route("**/api/v1/dashboard/traffic?*", (route) =>
    empty
      ? route.fulfill({
          json: {
            interface: "eth0",
            period: "day",
            updated_at: "2026-11-18T13:00:00+08:00",
            points: [],
          },
        })
      : route.fulfill({
          status: 503,
          json: {
            error: "未安装 vnstat，请安装 vnstat 2.x 并启动 vnstat 服务",
          },
        }),
  );
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  const widget = page.locator('[data-widget="website_traffic"]');
  await expect(widget).toContainText("未安装 vnstat");
  await expect(widget.locator(".recharts-line")).toHaveCount(0);
  empty = true;
  await widget.getByRole("button", { name: "重试", exact: true }).click();
  await expect(widget).toContainText("等待 vnstat 积累流量数据");
  await expect(widget).not.toContainText("未安装 vnstat");
});
