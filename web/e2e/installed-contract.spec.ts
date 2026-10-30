import { expect, test } from "@playwright/test";

// Deliberately no page.route(): these responses must come from the installed
// production binary, its service account, sudo policy and privileged helper.
test("installed HTTPS panel authenticates and reports real helper results", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/services");
  await page
    .getByLabel("密码", { exact: true })
    .fill(process.env.GOPANEL_E2E_PASSWORD!);
  await page.getByRole("button", { name: "登录面板", exact: true }).click();
  await expect(page.locator(".workspace")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "编程组件", exact: true }),
  ).toBeVisible();
  const session = await page.request.get("/api/v1/auth/session");
  expect(session.status()).toBe(200);
  expect((await session.json()).csrf_token).toBeTruthy();
  expect(
    (await page.context().cookies()).some(
      (cookie) => cookie.secure && cookie.httpOnly,
    ),
  ).toBe(true);
  const denied = process.env.GOPANEL_EXPECT_HELPER_ERROR === "1";
  await expect
    .poll(async () => {
      const response = await page.request.get("/api/v1/components");
      expect(response.status()).toBe(200);
      const status = (await response.json()).status;
      return denied ? status === "failed" : ["ready", "partial"].includes(status);
    })
    .toBe(true);
  if (denied) {
    await expect(
      page.getByRole("alert").filter({ hasText: "组件查询辅助程序不可用" }),
    ).toBeVisible();
  } else {
    await expect(page.locator("[data-component]")).toHaveCount(6);
    const response = await page.request.get("/api/v1/components");
    expect(
      (await response.json()).items.some(
        (item: { name: string; status: string }) =>
          item.name === "python3" && item.status === "ready",
      ),
    ).toBe(true);
  }
  const withoutCSRF = await page.request.post("/api/v1/components/refresh");
  expect(withoutCSRF.status()).toBe(403);
  expect(errors).toEqual([]);
});
