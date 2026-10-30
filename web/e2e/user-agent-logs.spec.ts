import { test, expect, type Page } from "./fixtures";

const samples = [
  ["Chrome/130.0 Safari/537.36", "Chrome", "chrome.svg"],
  ["Chrome/130.0 Safari/537.36 Edg/130.0", "Microsoft Edge", "edge.svg"],
  ["Gecko/20100101 Firefox/132.0", "Firefox", "firefox.svg"],
  ["Version/18.0 Mobile/15E148 Safari/604.1", "Safari", "safari.svg"],
  ["Chrome/130.0 Safari/537.36 OPR/115.0", "Opera", "opera.svg"],
  ["curl/8.10.1", "curl", "curl.svg"],
  ["Wget/1.21.4", "GNU Wget", "wget.svg"],
  ["python-requests/2.32.3", "Python requests", "python3.svg"],
  ["Go-http-client/1.1", "Go HTTP Client", "go.svg"],
  ["okhttp/4.12.0", "OkHttp", "okhttp.png"],
  ["axios/1.7.7", "axios", "axios.svg"],
] as const;

async function revealRow(page: Page, offset: string) {
  const row = page.locator(`.log-line[data-offset="${offset}"]`);
  await expect
    .poll(
      async () => {
        if (await row.count()) return true;
        await page.locator(".log-viewport").evaluate((node) => {
          node.scrollTop += node.clientHeight / 2;
        });
        return false;
      },
      { intervals: [20, 50, 100] },
    )
    .toBe(true);
  return row;
}

test("Nginx UA icons load locally in raw and table views without changing log text", async ({
  page,
}, testInfo) => {
  test.setTimeout(60000);
  const path = "/home/wwwlogs/access.log";
  const prefix =
    '192.0.2.1 - - [27/Oct/2026:14:23:45 +0800] "GET /curl/8.10.1 HTTP/1.1" 200 123';
  const agents = [
    ...samples.map(([ua]) => ua),
    "unknown",
    "-",
    "<script>window.logInjected=true</script>",
  ];
  const lines = [
    ...agents.map((ua, i) => ({
      offset: String(i * 1000),
      text: `${prefix} "https://example.com/Chrome/130.0" "${ua}"`,
    })),
    { offset: "14000", text: prefix },
    { offset: "15000", text: "2026/10/27 [error] curl/8.10.1 request failed" },
  ];
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/api/v1/logs", (route) =>
    route.fulfill({
      json: {
        files: [
          {
            path,
            available: true,
            size: 16000,
            modified: "2026-10-27T06:23:45Z",
          },
        ],
        warnings: [],
      },
    }),
  );
  await page.route("**/api/v1/logs/read", (route) => {
    const after = route.request().postDataJSON().mode === "after";
    return route.fulfill({
      json: {
        lines: after ? [] : lines,
        from: after ? "16000" : "0",
        to: "16000",
        replace_from: after ? "16000" : "0",
        start: after ? "c16000" : "c0",
        end: "c16000",
        size: 16000,
        modified: "2026-10-27T06:23:45Z",
        has_before: false,
        has_after: false,
        limited: false,
      },
    });
  });
  await page.goto("/logs");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(page.locator(".log-line").first()).toBeVisible();
  await page.getByRole("button", { name: "暂停追踪" }).click();
  await page.getByRole("checkbox", { name: "自动滚动" }).uncheck();

  const viewport = page.locator(".log-viewport");
  const format = page.getByRole("checkbox", { name: "格式化为表格" });
  for (const table of [false, true]) {
    if (table) await format.check();
    await viewport.evaluate((node) => {
      node.scrollTop = 0;
    });
    for (const [i, [ua, name, asset]] of samples.entries()) {
      const row = await revealRow(page, String(i * 1000));
      const icon = row.getByRole("img", { name, exact: true });
      await expect(icon).toHaveAttribute("src", `/brands/${asset}`);
      await expect(icon).toHaveAttribute("title", name);
      await expect(icon).toHaveCSS("width", "16px");
      await expect(icon).toHaveCSS("height", "16px");
      await expect
        .poll(() =>
          icon.evaluate(
            (node: HTMLImageElement) => node.complete && node.naturalWidth > 0,
          ),
        )
        .toBe(true);
      if (table) await expect(row.getByRole("cell").last()).toHaveText(ua);
      else {
        const text = row.locator(".log-text");
        await expect(text).toHaveText(lines[i].text);
        if (i === 0) {
          const selected = await text.evaluate((node) => {
            const range = document.createRange();
            range.selectNodeContents(node);
            const selection = window.getSelection()!;
            selection.removeAllRanges();
            selection.addRange(range);
            const value = selection.toString();
            selection.removeAllRanges();
            return value;
          });
          expect(selected).toBe(lines[i].text);
        }
      }
    }
    for (let i = samples.length; i < lines.length; i++) {
      const row = await revealRow(page, lines[i].offset);
      await expect(row.getByRole("img")).toHaveCount(0);
    }
    expect(
      await page.evaluate(
        () => (window as Window & { logInjected?: boolean }).logInjected,
      ),
    ).toBeUndefined();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);

    for (const theme of ["light", "dark"]) {
      await page.evaluate(
        (theme) =>
          document.documentElement.classList.toggle("dark", theme === "dark"),
        theme,
      );
      for (const [offset, name, inverted] of [
        ["5000", "curl", theme === "dark"],
        ["9000", "OkHttp", theme === "light"],
      ] as const) {
        await viewport.evaluate((node) => {
          node.scrollTop = 0;
        });
        const row = await revealRow(page, offset);
        await expect(row.getByRole("img", { name, exact: true })).toHaveCSS(
          "filter",
          inverted ? "invert(1)" : "none",
        );
      }
      await viewport.evaluate((node) => {
        node.scrollTop = 0;
      });
      if (table) {
        await page.locator(".log-table-scroll").evaluate((node) => {
          node.scrollLeft = node.scrollWidth;
        });
        await expect
          .poll(() =>
            page
              .locator('[data-offset="0"] .log-user-agent-icon')
              .evaluate((icon) => {
                const cell = icon.getBoundingClientRect();
                const table = icon
                  .closest(".log-table-scroll")!
                  .getBoundingClientRect();
                return cell.left >= table.left && cell.right <= table.right;
              }),
          )
          .toBe(true);
      }
      await page.screenshot({
        path: testInfo.outputPath(`ua-${table ? "table" : "raw"}-${theme}.png`),
        fullPage: true,
      });
    }
  }

  // A missing asset must not replace the UA text with a broken image.
  await page.route("**/brands/chrome.svg", (route) => route.abort());
  await page.reload();
  await expect(page.locator(".log-line").first()).toBeVisible();
  await page.getByRole("button", { name: "暂停追踪" }).click();
  await page.getByRole("checkbox", { name: "自动滚动" }).uncheck();
  await viewport.evaluate((node) => {
    node.scrollTop = 0;
  });
  const chrome = await revealRow(page, "0");
  await expect(chrome.locator(".log-user-agent-icon")).toHaveCSS(
    "visibility",
    "hidden",
  );
  await expect(chrome.locator(".log-text")).toHaveText(lines[0].text);
  expect(errors).toEqual([]);
});
