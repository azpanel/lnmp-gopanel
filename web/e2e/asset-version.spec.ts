import { expect, test, type Page } from "@playwright/test";

async function checkAgain(page: Page) {
  const response = page.waitForResponse(
    (response) => new URL(response.url()).pathname === "/index.html",
  );
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await response;
  await page.waitForLoadState("networkidle");
}

for (const extension of ["js", "css"]) {
  test(`refreshes stale ${extension} once per target and keeps the current URL`, async ({
    page,
  }) => {
    const original = await (await page.request.get("/")).text();
    let latest = original;
    let navigations = 0;
    page.on("request", (request) => {
      if (request.isNavigationRequest() && request.frame() === page.mainFrame())
        navigations++;
    });
    await page.route("**/index.html?*", (route) =>
      route.fulfill({ contentType: "text/html", body: latest }),
    );
    await page.goto("/vhosts?view=cards#preserve");
    await page.waitForLoadState("networkidle");
    expect(navigations).toBe(1);
    const url = page.url();
    latest = original.replace(
      new RegExp(`/assets/index-[^"\\s]+\\.${extension}`),
      `/assets/index-updated.${extension}`,
    );
    expect(latest).not.toBe(original);
    await checkAgain(page);
    await expect.poll(() => navigations).toBe(2);
    await page.waitForLoadState("networkidle");
    expect(page.url()).toBe(url);
    // Simulate a proxy still serving the previous document after the reload.
    await checkAgain(page);
    expect(navigations).toBe(2);
    latest = latest.replace("index-updated", "index-another");
    await checkAgain(page);
    await expect.poll(() => navigations).toBe(3);
  });
}

test("ignores failed or invalid checks, polls and resumes when the page becomes visible", async ({
  page,
}) => {
  const original = await (await page.request.get("/")).text();
  let mode: "same" | "error" | "invalid" | "offline" | "updated" = "same";
  let checks = 0,
    navigations = 0;
  page.on("request", (request) => {
    if (request.isNavigationRequest() && request.frame() === page.mainFrame())
      navigations++;
  });
  await page.route("**/index.html?*", (route) => {
    checks++;
    if (mode === "offline") return route.abort();
    if (mode === "error")
      return route.fulfill({ status: 503, body: "unavailable" });
    if (mode === "invalid")
      return route.fulfill({
        contentType: "text/html",
        body: "<html>maintenance</html>",
      });
    return route.fulfill({
      contentType: "text/html",
      body:
        mode === "updated"
          ? original.replace(
              /\/assets\/index-[^"\s]+\.css/,
              "/assets/index-new.css",
            )
          : original,
    });
  });
  await page.clock.install();
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  const initial = checks;
  await page.clock.fastForward(60001);
  await expect.poll(() => checks).toBeGreaterThan(initial);
  await page.waitForLoadState("networkidle");
  for (const failure of ["error", "invalid"] as const) {
    mode = failure;
    await checkAgain(page);
  }
  mode = "offline";
  const failed = page.waitForEvent(
    "requestfailed",
    (request) => new URL(request.url()).pathname === "/index.html",
  );
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await failed;
  await page.waitForLoadState("networkidle");
  expect(navigations).toBe(1);
  mode = "updated";
  await page.evaluate(() =>
    Object.defineProperty(document, "hidden", {
      configurable: true,
      get: () => true,
    }),
  );
  const hidden = checks;
  await page.clock.fastForward(60001);
  expect(checks).toBe(hidden);
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", {
      configurable: true,
      get: () => false,
    });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await expect.poll(() => navigations).toBe(2);
});

test("the independent monitor recovers a failed app bundle when tab storage is unavailable", async ({
  page,
}) => {
  const original = await (await page.request.get("/")).text();
  const stale = original.replace(
    /\/assets\/index-[^"\s]+\.js/,
    "/assets/missing-old.js",
  );
  let navigations = 0;
  let updated = false;
  page.on("request", (request) => {
    if (request.isNavigationRequest() && request.frame() === page.mainFrame())
      navigations++;
  });
  await page.addInitScript(() => {
    Object.defineProperty(window, "sessionStorage", {
      get: () => {
        throw new Error("blocked");
      },
    });
  });
  await page.route("**/index.html?*", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: updated ? original : stale,
    }),
  );
  await page.route("**/vhosts*", (route) =>
    route.fulfill({ contentType: "text/html", body: stale }),
  );
  await page.goto("/vhosts?view=cards#preserve");
  await page.waitForLoadState("networkidle");
  updated = true;
  await page.evaluate(() =>
    window.dispatchEvent(new Event("vite:preloadError")),
  );
  await expect.poll(() => navigations).toBe(2);
  await page.waitForLoadState("networkidle");
  expect(new URL(page.url()).searchParams.has("_asset_version")).toBe(true);
  await checkAgain(page);
  expect(navigations).toBe(2);
});
