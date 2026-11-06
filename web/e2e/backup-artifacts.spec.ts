import { test, expect, type Page } from "./fixtures";
import { newPolicy } from "../src/lib/backup-policies";

async function fixture(page: Page) {
  const id = "a".repeat(32);
  const policy = {
    ...newPolicy(),
    id,
    revision: 7,
    name: "多位置备份",
    enabled: false,
    destinations: [
      { type: "local", path: "/backups/site" },
      { type: "gdrive", account: "backup@example.com", folder_id: "folder123" },
      { type: "rclone", remote: "cloud", path: "site" },
      { type: "local", path: "/backups/second" },
    ],
  };
  const reads: number[] = [];
  let failure = false,
    conflict = false;
  await page.route("**/api/v1/backups/components", (r) =>
    r.fulfill({ json: { items: [] } }),
  );
  await page.route("**/api/v1/backups/runtime", (r) =>
    r.fulfill({ json: { scheduler: true, dependencies: [] } }),
  );
  await page.route("**/api/v1/backups/policies", (r) =>
    r.fulfill({ json: [policy] }),
  );
  await page.route(`**/api/v1/backups/policies/${id}`, (r) =>
    r.fulfill({ json: policy }),
  );
  await page.route(`**/api/v1/backups/policies/${id}/artifacts?*`, (r) => {
    const params = new URL(r.request().url()).searchParams;
    const index = Number(params.get("destination_index"));
    reads.push(index);
    if (conflict) {
      conflict = false;
      policy.revision++;
      policy.destinations = [{ type: "local", path: "/backups/new" }];
      return r.fulfill({ status: 409, json: { error: "配置已变化" } });
    }
    expect(params.get("revision")).toBe(String(policy.revision));
    if (failure)
      return r.fulfill({ status: 502, json: { error: "远程网络不可用" } });
    return r.fulfill({
      json: {
        items:
          index === 2
            ? []
            : Array.from({ length: index === 0 ? 21 : 1 }, (_, n) => ({
                object: `object-${n}`,
                name: `${id}-${n % 2 ? "databases" : "files"}-20261106T020000Z-${n.toString(16).padStart(32, "0")}.zip`,
                category: n % 2 ? "databases" : "files",
                backup_at: "2026-11-06T02:00:00Z",
                ...(index === 1
                  ? { size_display: "1.2 MiB" }
                  : { bytes: 2048 }),
              })),
        checked_at: "2026-11-06T03:00:00Z",
        truncated: false,
      },
    });
  });
  await page.goto("/backups");
  await page
    .getByLabel("密码", { exact: true })
    .fill("Test-Only-Panel-Password-2026");
  await page.getByRole("button", { name: "登录面板" }).click();
  await page.getByRole("button", { name: "查看备份", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "查看备份 · 多位置备份" });
  await expect(dialog.getByRole("table")).toBeVisible();
  return {
    dialog,
    reads,
    fail: () => {
      failure = true;
    },
    conflict: () => {
      conflict = true;
    },
  };
}

test("backup files load per destination, paginate and refresh without losing previous results", async ({
  page,
}, testInfo) => {
  const f = await fixture(page);
  const d = f.dialog;
  expect(f.reads).toEqual([0]);
  await expect(d.locator("tbody tr")).toHaveCount(20);
  await d.getByRole("button", { name: "下一页" }).click();
  await expect(d.locator("tbody tr")).toHaveCount(1);
  await expect(d).toContainText("第 2 / 2 页");
  await d.getByRole("button", { name: "刷新备份" }).click();
  await expect(d).toContainText("第 1 / 2 页");
  await expect.poll(() => f.reads.length).toBe(2);
  await d.getByRole("tab", { name: "gdrive 2" }).click();
  await expect(d).toContainText("约 1.2 MiB");
  await expect(d.locator("tbody tr")).toHaveCount(1);
  await d.getByRole("tab", { name: "rclone 3" }).click();
  await expect(d).toContainText("此存储位置暂无当前政策的备份文件");
  await d.getByRole("tab", { name: "本地 4" }).click();
  await expect(d).toContainText("/backups/second");
  await expect(d.getByRole("table")).toBeVisible();
  f.fail();
  await d.getByRole("button", { name: "刷新备份" }).click();
  await expect(d.getByRole("alert")).toContainText("远程网络不可用");
  await expect(d).toContainText("以下为上次查询结果");
  await expect(d.getByRole("table")).toBeVisible();
  expect(f.reads).toEqual([0, 0, 1, 2, 3, 3]);
  const box = await d.boundingBox();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
  await page.screenshot({
    path: testInfo.outputPath("backup-artifacts.png"),
    fullPage: true,
  });
});

test("configuration conflict reloads destinations and queries the new revision", async ({
  page,
}) => {
  const f = await fixture(page);
  f.conflict();
  await f.dialog.getByRole("button", { name: "刷新备份" }).click();
  await expect(f.dialog.getByRole("tab")).toHaveCount(1);
  await expect(f.dialog).toContainText("/backups/new");
  await expect(f.dialog.getByRole("table")).toBeVisible();
  expect(f.reads).toEqual([0, 0, 0]);
});
