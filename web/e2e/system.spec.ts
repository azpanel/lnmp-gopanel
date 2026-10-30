import { test, expect, type Page } from "./fixtures";
import type { Host } from "../src/lib/types";

const ubuntu: Host = {
  hostname: "test-host",
  os: "linux",
  platform: "ubuntu",
  version: "24.04",
  kernel: "6.8.0-test",
  arch: "amd64",
  cores: 4,
  cpu_model: "Intel(R) Xeon(R) CPU",
};

async function login(page: Page) {
  await page.goto("/");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await expect(page.locator(".workspace")).toBeVisible();
}

function field(page: Page, label: string) {
  return page.locator(".host-fields > div").filter({
    has: page.locator("dt").getByText(label, { exact: true }),
  });
}

test("host values select brand icons independently and fit both themes", async ({
  page,
}, testInfo) => {
  let host = ubuntu;
  await page.route("**/api/v1/host", (route) => route.fulfill({ json: host }));
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await login(page);

  const cases = [
    { host: ubuntu, platform: "ubuntu", cpu: "intel", arm: false, linux: true },
    {
      host: { ...ubuntu, platform: " Debian ", cpu_model: "AMD EPYC 7763" },
      platform: "debian",
      cpu: "amd",
      arm: false,
      linux: true,
    },
    {
      host: {
        ...ubuntu,
        platform: "Rocky Linux",
        arch: " ARM64 ",
        cpu_model: "ARMv8 Processor rev 1",
      },
      platform: "rockylinux",
      cpu: "arm",
      arm: true,
      linux: true,
    },
    {
      host: {
        ...ubuntu,
        platform: "unknown-distro",
        arch: "aarch64",
        cpu_model: "Generic Processor",
      },
      platform: "linux",
      cpu: undefined,
      arm: true,
      linux: true,
    },
    {
      host: {
        ...ubuntu,
        os: "freebsd",
        platform: "freebsd",
        cpu_model: "Generic Processor",
      },
      platform: undefined,
      cpu: undefined,
      arm: false,
      linux: false,
    },
  ];
  for (const item of cases) {
    host = item.host;
    await page.goto("/system");
    await expect(field(page, "CPU 型号").locator("dd")).toHaveText(
      host.cpu_model,
    );
    for (const [label, brand] of [
      ["操作系统", item.platform],
      ["CPU 型号", item.cpu],
      ["系统架构", item.arm ? "arm" : undefined],
      ["内核版本", item.linux ? "linux" : undefined],
    ] as const) {
      const icon = field(page, label).locator("dd img");
      if (brand) {
        await expect(icon).toHaveAttribute("src", `/brands/${brand}.svg`);
        await expect(icon).toBeVisible();
        await expect
          .poll(() =>
            icon.evaluate((node) => (node as HTMLImageElement).naturalWidth),
          )
          .toBeGreaterThan(0);
        await expect(icon).toHaveAttribute("alt", "");
      } else {
        await expect(icon).toHaveCount(0);
      }
    }
    await expect(page.locator(".host-fields dt svg")).toHaveCount(7);
  }

  host = { ...ubuntu, cpu_model: "Intel(R) " + "LongCPUModel".repeat(25) };
  await page.goto("/system");
  await expect(field(page, "CPU 型号").locator("dd")).toHaveText(
    host.cpu_model,
  );
  for (const theme of ["light", "dark"]) {
    if (theme === "dark")
      await page.getByRole("button", { name: "切换深色主题" }).click();
    await expect(page.locator(".main-shell")).toHaveCSS(
      "margin-left",
      testInfo.project.name === "mobile" ? "0px" : "280px",
    );
    await expect(field(page, "CPU 型号").locator("dd img")).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    ).toBeTruthy();
    expect(
      await page
        .locator(".host-fields dd")
        .evaluateAll((nodes) =>
          nodes.every((node) => node.scrollWidth <= node.clientWidth),
        ),
    ).toBeTruthy();
    await page.screenshot({
      path: testInfo.outputPath(`system-brands-${theme}.png`),
      fullPage: true,
      animations: "disabled",
    });
  }
  expect(errors).toEqual([]);
});

test("missing data, failed images and host errors preserve readable values", async ({
  page,
}) => {
  let host = {
    ...ubuntu,
    platform: " ",
    version: "",
    kernel: "",
    arch: "",
    cpu_model: "",
  };
  let failed = false;
  await page.route("**/api/v1/host", (route) =>
    failed
      ? route.fulfill({ status: 503, json: { error: "host unavailable" } })
      : route.fulfill({ json: host }),
  );
  await login(page);
  await page.goto("/system");
  for (const label of ["操作系统", "内核版本", "系统架构", "CPU 型号"])
    await expect(field(page, label).locator("dd")).toHaveText("—");
  await expect(page.locator(".host-brand-icon")).toHaveCount(0);

  host = ubuntu;
  await page.route("**/brands/intel.svg", (route) => route.abort());
  await page.reload();
  await expect(field(page, "CPU 型号").locator("dd")).toHaveText(
    host.cpu_model,
  );
  await expect(field(page, "CPU 型号").locator("dd img")).toBeHidden();
  await expect(field(page, "操作系统").locator("dd img")).toBeVisible();

  failed = true;
  await page.reload();
  await expect(page.locator(".notice")).toContainText("host unavailable");
  await expect(page.locator(".host-brand-icon")).toHaveCount(0);
  await expect(field(page, "操作系统").locator("dd")).toHaveText("—");
});
