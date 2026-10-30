import { test as base } from "@playwright/test";
import { spawn } from "node:child_process";
import https from "node:https";
import net from "node:net";
import path from "node:path";
import { setTimeout as delay } from "node:timers/promises";

export * from "@playwright/test";

function ready(url: URL): Promise<boolean> {
  return new Promise((resolve) => {
    const request = https.get(
      new URL("/api/v1/auth/session", url),
      { rejectUnauthorized: false, timeout: 500 },
      (response) => {
        response.resume();
        resolve(response.statusCode === 401);
      },
    );
    request.on("timeout", () => request.destroy());
    request.on("error", () => resolve(false));
  });
}

async function requireFreePort(port: number) {
  await new Promise<void>((resolve, reject) => {
    const server = net.createServer();
    server.once("error", reject);
    server.listen(port, "127.0.0.1", () => server.close(() => resolve()));
  });
}

// The real limiter and account state belong to one test, rather than the whole
// suite. A quick machine must not turn unrelated successful logins into 429s.
export const test = base.extend<{ panelInstance: void }>({
  panelInstance: [
    async ({ baseURL }, use, testInfo) => {
      const url = new URL(baseURL!);
      if (url.protocol !== "https:" || url.hostname !== "127.0.0.1")
        throw new Error("Fixture panel must listen on local HTTPS");
      await requireFreePort(Number(url.port));
      let logs = "";
      let finished = false;
      let launchError: Error | undefined;
      const child = spawn(
        process.execPath,
        [
          path.resolve(import.meta.dirname, "../scripts/e2e-server.mjs"),
          url.port,
        ],
        {
          cwd: path.resolve(import.meta.dirname, ".."),
          env: process.env,
          detached: process.platform !== "win32",
          stdio: ["ignore", "pipe", "pipe"],
        },
      );
      const append = (data: Buffer) => {
        logs = (logs + data.toString()).slice(-65536);
      };
      child.stdout.on("data", append);
      child.stderr.on("data", append);
      const ended = new Promise<void>((resolve) => {
        child.once("exit", () => {
          finished = true;
          resolve();
        });
        child.once("error", (error) => {
          launchError = error;
          finished = true;
          resolve();
        });
      });
      const signal = (name: NodeJS.Signals) => {
        try {
          if (child.pid && process.platform !== "win32")
            process.kill(-child.pid, name);
          else child.kill(name);
        } catch (error) {
          if ((error as NodeJS.ErrnoException).code !== "ESRCH") throw error;
        }
      };
      let failed = false;
      try {
        const deadline = Date.now() + 30000;
        while (!(await ready(url))) {
          if (finished || Date.now() >= deadline)
            throw new Error(
              `Fixture panel did not start: ${launchError || ""}\n${logs}`,
            );
          await delay(50);
        }
        if (finished) throw new Error(`Fixture panel exited: ${logs}`);
        await use();
      } catch (error) {
        failed = true;
        throw error;
      } finally {
        if (!finished) {
          signal("SIGTERM");
          let timer: ReturnType<typeof setTimeout> | undefined;
          await Promise.race([
            ended,
            new Promise<void>((resolve) => {
              timer = setTimeout(resolve, 15000);
            }),
          ]);
          clearTimeout(timer);
          if (!finished) {
            signal("SIGKILL");
            await ended;
          }
        }
        if (
          failed ||
          testInfo.status === "failed" ||
          testInfo.status === "timedOut"
        )
          await testInfo.attach("panel.log", {
            body: logs,
            contentType: "text/plain",
          });
      }
    },
    { auto: true, timeout: 30000 },
  ],
});
