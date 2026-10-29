import { test, expect } from "@playwright/test";

test("six TOTP digits submit once while recovery codes require a click", async ({
  page,
}) => {
  const submissions: Record<string, string>[] = [];
  let releaseResponse!: () => void;
  const responseReady = new Promise<void>((resolve) => {
    releaseResponse = resolve;
  });
  await page.route("**/api/v1/auth/session", (route) =>
    route.fulfill({ status: 401, json: { error: "请先登录" } }),
  );
  await page.route("**/api/v1/auth/login", (route) =>
    route.fulfill({
      json: {
        totp_required: true,
        challenge_token: "test-challenge",
        expires_in: 300,
      },
    }),
  );
  await page.route("**/api/v1/auth/login/totp", async (route) => {
    submissions.push(route.request().postDataJSON());
    await responseReady;
    await route.fulfill({ status: 401, json: { error: "验证码错误" } });
  });
  await page.goto("/");
  await page.getByLabel("密码", { exact: true }).fill("test-password");
  await page.getByRole("button", { name: "登录面板", exact: true }).click();

  const code = page.getByLabel("动态验证码", { exact: true });
  const submit = page.locator('.login-step button[type="submit"]');
  await code.fill("12345");
  expect(submissions).toHaveLength(0);
  await code.fill("12345a");
  expect(submissions).toHaveLength(0);
  await code.fill("");
  await code.pressSequentially("123456");
  await expect.poll(() => submissions.length).toBe(1);
  expect(submissions[0]).toEqual({
    challenge_token: "test-challenge",
    code: "123456",
  });
  await expect(submit).toBeDisabled();
  await code.fill("654321");
  expect(submissions).toHaveLength(1);

  releaseResponse();
  await expect(page.getByRole("alert")).toHaveText("验证码错误");
  await expect(submit).toBeEnabled();
  expect(submissions).toHaveLength(1);
  await code.fill("123456");
  await expect.poll(() => submissions.length).toBe(2);
  await expect(submit).toBeEnabled();

  await page.getByRole("button", { name: "使用恢复码", exact: true }).click();
  await page.getByLabel("恢复码", { exact: true }).fill("123456");
  expect(submissions).toHaveLength(2);
  await submit.click();
  await expect.poll(() => submissions.length).toBe(3);
  expect(submissions[2]).toEqual({
    challenge_token: "test-challenge",
    recovery_code: "123456",
  });
});
