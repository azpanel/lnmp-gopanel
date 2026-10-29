import { createHmac } from "node:crypto";
import { readFile } from "node:fs/promises";
import { test, expect, type Page } from "@playwright/test";

const original = "Test-Only-Panel-Password-2026";
function totp(secret: string) {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = "";
  for (const char of secret)
    bits += alphabet.indexOf(char).toString(2).padStart(5, "0");
  const key = Buffer.from(
    bits.match(/.{8}/g)!.map((byte) => parseInt(byte, 2)),
  );
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000)));
  const digest = createHmac("sha1", key).update(counter).digest();
  const offset = digest[digest.length - 1] & 15;
  return ((digest.readUInt32BE(offset) & 0x7fffffff) % 1000000)
    .toString()
    .padStart(6, "0");
}
async function passwordLogin(page: Page, password: string) {
  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "登录面板", exact: true }).click();
}
async function openSecurity(page: Page, mobile: boolean, tab = "修改密码") {
  const menu = page.getByRole("button", { name: "打开导航", exact: true });
  if (mobile && (await menu.getAttribute("aria-expanded")) !== "true")
    await menu.click();
  await page.getByRole("button", { name: "账户安全", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("tab", { name: tab, exact: true }).click();
  await expect(dialog.getByLabel("当前登录密码")).toBeVisible();
  return dialog;
}
async function signOut(page: Page, mobile: boolean) {
  const menu = page.getByRole("button", { name: "打开导航", exact: true });
  if (mobile && (await menu.getAttribute("aria-expanded")) !== "true")
    await menu.click();
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "登录管理面板" }),
  ).toBeVisible();
}

test("password changes, TOTP enrollment, recovery and session rotation work together", async ({
  page,
}, testInfo) => {
  test.setTimeout(120000);
  const mobile = testInfo.project.name === "mobile";
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/");
  await passwordLogin(page, original);
  await expect(page.locator(".workspace")).toBeVisible();
  let dialog = await openSecurity(page, mobile);
  await page.screenshot({
    path: testInfo.outputPath("account-password-light.png"),
    fullPage: true,
    animations: "disabled",
  });
  await dialog.getByLabel("当前登录密码").fill(original);
  await dialog
    .getByLabel("新密码", { exact: true })
    .fill("passwordpasswordpassword");
  await dialog.getByLabel("确认新密码").fill("passwordpasswordpassword");
  await expect(
    dialog.getByText("密码容易被猜到", { exact: false }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "修改密码并退出登录" }),
  ).toBeDisabled();
  await dialog.getByRole("button", { name: "随机生成强密码" }).click();
  const password = await dialog
    .getByLabel("新密码", { exact: true })
    .inputValue();
  expect(password).toHaveLength(24);
  await expect(dialog.getByLabel("确认新密码")).toHaveValue(password);
  await expect(dialog.getByText("密码强度已通过")).toBeVisible();
  await dialog.getByLabel("确认新密码").fill("not-matching");
  await dialog.getByRole("button", { name: "修改密码并退出登录" }).click();
  await expect(dialog.getByRole("alert")).toHaveText("两次输入的新密码不一致");
  await dialog.getByLabel("确认新密码").fill(password);
  await dialog.getByRole("button", { name: "修改密码并退出登录" }).click();
  await expect(
    page.getByRole("heading", { name: "登录管理面板" }),
  ).toBeVisible();
  await passwordLogin(page, original);
  await expect(page.getByRole("alert")).toContainText("用户名或密码错误");
  await passwordLogin(page, password);
  await expect(page.locator(".workspace")).toBeVisible();
  dialog = await openSecurity(page, mobile, "TOTP 设置");
  await dialog.getByLabel("当前登录密码").fill(password);
  await dialog.getByRole("button", { name: "绑定 TOTP", exact: true }).click();
  await expect(
    dialog.getByRole("img", { name: "TOTP 绑定二维码" }),
  ).toBeVisible();
  const secret = await dialog.getByLabel("手动输入密钥").inputValue();
  const enrollmentCode = totp(secret);
  await dialog.getByLabel("动态验证码", { exact: true }).fill(enrollmentCode);
  await dialog.getByRole("button", { name: "确认启用 TOTP" }).click();
  const codeBlock = dialog.getByLabel("一次性恢复码", { exact: true });
  await expect(codeBlock).toBeVisible();
  const codes = (await codeBlock.innerText()).split("\n");
  expect(codes).toHaveLength(10);
  expect(new Set(codes).size).toBe(10);
  const downloadEvent = page.waitForEvent("download");
  await dialog.getByRole("button", { name: "下载恢复码" }).click();
  const download = await downloadEvent;
  expect(download.suggestedFilename()).toBe("gopanel-recovery-codes.txt");
  expect(await readFile((await download.path())!, "utf8")).toContain(codes[0]);
  await dialog.getByRole("button", { name: "我已保存" }).click();
  await expect(
    dialog.getByText("已启用 · 剩余 10 个恢复码。", { exact: false }),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("account-security-light.png"),
    fullPage: true,
    animations: "disabled",
  });
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  if (mobile)
    await page.getByRole("button", { name: "关闭导航", exact: true }).click();
  await expect(page.getByText("实时连接", { exact: true })).toBeVisible({
    timeout: 10000,
  });
  await page.getByRole("button", { name: "切换深色主题" }).click();
  dialog = await openSecurity(page, mobile, "TOTP 设置");
  await page.screenshot({
    path: testInfo.outputPath("account-security-dark.png"),
    fullPage: true,
    animations: "disabled",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBeTruthy();
  expect(
    await dialog.evaluate((node) => node.scrollWidth <= node.clientWidth),
  ).toBeTruthy();
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await signOut(page, mobile);
  await passwordLogin(page, password);
  await expect(page.getByLabel("动态验证码", { exact: true })).toBeVisible();
  await page.getByLabel("动态验证码", { exact: true }).fill(enrollmentCode);
  await page.getByRole("button", { name: "验证并登录" }).click();
  await expect(page.getByRole("alert")).toContainText("验证码错误");
  await page.getByRole("button", { name: "使用恢复码", exact: true }).click();
  await page.getByLabel("恢复码", { exact: true }).fill(codes[0]);
  await page.getByRole("button", { name: "验证并登录" }).click();
  await expect(page.locator(".workspace")).toBeVisible();
  dialog = await openSecurity(page, mobile, "TOTP 设置");
  await expect(
    dialog.getByText("剩余 9 个恢复码", { exact: false }),
  ).toBeVisible();
  await dialog.getByLabel("当前登录密码").fill(password);
  await dialog.getByRole("button", { name: "使用恢复码", exact: true }).click();
  await dialog.getByLabel("一次性恢复码", { exact: true }).fill(codes[1]);
  await dialog.getByRole("button", { name: "重新生成恢复码" }).click();
  await expect(dialog.locator("pre.recovery-codes")).toBeVisible();
  const fresh = (await dialog.locator("pre.recovery-codes").innerText()).split(
    "\n",
  );
  expect(fresh).toHaveLength(10);
  expect(fresh.some((code) => codes.includes(code))).toBe(false);
  await dialog.getByRole("button", { name: "我已保存" }).click();
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await signOut(page, mobile);
  await passwordLogin(page, password);
  await page.getByRole("button", { name: "使用恢复码", exact: true }).click();
  await page.getByLabel("恢复码", { exact: true }).fill(codes[2]);
  await page.getByRole("button", { name: "验证并登录" }).click();
  await expect(page.getByRole("alert")).toContainText("验证码错误");
  await page.getByLabel("恢复码", { exact: true }).fill(fresh[0]);
  await page.getByRole("button", { name: "验证并登录" }).click();
  await expect(page.locator(".workspace")).toBeVisible();
  dialog = await openSecurity(page, mobile, "TOTP 设置");
  await dialog.getByLabel("当前登录密码").fill(password);
  await dialog.getByRole("button", { name: "使用恢复码", exact: true }).click();
  await dialog.getByLabel("一次性恢复码", { exact: true }).fill(fresh[1]);
  await dialog.getByRole("button", { name: "关闭 TOTP", exact: true }).click();
  await expect(
    dialog.getByRole("button", { name: "绑定 TOTP", exact: true }),
  ).toBeVisible();
  // Restore this isolated test server's original credentials for other specs.
  await dialog.getByRole("tab", { name: "修改密码", exact: true }).click();
  await dialog.getByLabel("当前登录密码").fill(password);
  await dialog.getByLabel("新密码", { exact: true }).fill(original);
  await dialog.getByLabel("确认新密码").fill(original);
  await expect(dialog.getByText("密码强度已通过")).toBeVisible();
  await dialog.getByRole("button", { name: "修改密码并退出登录" }).click();
  await expect(
    page.getByRole("heading", { name: "登录管理面板" }),
  ).toBeVisible();
  await passwordLogin(page, original);
  await expect(page.locator(".workspace")).toBeVisible();
  expect(errors).toEqual([]);
});
