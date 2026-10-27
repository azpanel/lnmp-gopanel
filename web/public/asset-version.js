// Run independently of the application bundle, including when an old chunk fails
// to load after deployment. Vite's development page has no hashed asset snapshot.
(() => {
  const marker = "lnmp:asset-reload";
  const fallback = "_asset_version";
  function snapshot(doc) {
    if (!doc.getElementById("root")) return null;
    const assets = new Set();
    let script = false,
      style = false;
    for (const node of doc.querySelectorAll(
      'script[src], link[rel="stylesheet"], link[rel="modulepreload"]',
    )) {
      const url = new URL(
        node.getAttribute("src") || node.getAttribute("href"),
        location.origin,
      );
      if (
        url.origin !== location.origin ||
        !url.pathname.startsWith("/assets/")
      )
        continue;
      assets.add(url.pathname + url.search);
      if (node.tagName === "SCRIPT") script = true;
      if (node.rel === "stylesheet") style = true;
    }
    return script && style ? JSON.stringify([...assets].sort()) : null;
  }
  const loaded = snapshot(document);
  if (!loaded) return;
  try {
    if (sessionStorage.getItem(marker) === loaded)
      sessionStorage.removeItem(marker);
  } catch {
    /* Storage may be unavailable; the URL guards reloads in that case. */
  }
  const current = new URL(location.href);
  if (current.searchParams.get(fallback) === loaded) {
    current.searchParams.delete(fallback);
    history.replaceState(history.state, "", current);
  }

  let checking = false,
    reloading = false;
  function refresh(version) {
    try {
      // A proxy may keep serving stale HTML even after a reload. Try each target
      // once per tab, then wait for another version instead of looping.
      if (sessionStorage.getItem(marker) === version) return;
      sessionStorage.setItem(marker, version);
    } catch {
      const url = new URL(location.href);
      if (url.searchParams.get(fallback) === version) return;
      url.searchParams.set(fallback, version);
      reloading = true;
      location.replace(url);
      return;
    }
    reloading = true;
    location.reload();
  }
  async function check() {
    if (checking || reloading || document.hidden) return;
    checking = true;
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 5000);
    try {
      const url = new URL("/index.html", location.origin);
      url.searchParams.set("_asset_check", String(Date.now()));
      const response = await fetch(url, {
        cache: "no-store",
        signal: controller.signal,
      });
      if (
        !response.ok ||
        !response.headers.get("content-type")?.includes("text/html")
      )
        return;
      const latest = snapshot(
        new DOMParser().parseFromString(await response.text(), "text/html"),
      );
      if (latest && latest !== loaded) refresh(latest);
    } catch {
      /* Offline pages and transient server failures must not force a reload. */
    } finally {
      clearTimeout(timeout);
      checking = false;
    }
  }
  window.addEventListener("focus", check);
  window.addEventListener("pageshow", check);
  document.addEventListener("visibilitychange", check);
  window.addEventListener("vite:preloadError", check);
  window.addEventListener(
    "error",
    (event) => {
      if (
        event.target instanceof HTMLScriptElement ||
        (event.target instanceof HTMLLinkElement &&
          event.target.rel === "stylesheet")
      )
        void check();
    },
    true,
  );
  setInterval(check, 60000);
  void check();
})();
