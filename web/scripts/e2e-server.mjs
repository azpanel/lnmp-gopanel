import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
const directory = mkdtempSync(path.join(tmpdir(), "lnmp-gopanel-e2e-"));
const binary = path.resolve(
  process.env.GOPANEL_E2E_BINARY || "../bin/lnmp-gopanel",
);
const port = Number(process.argv[2] ?? 18091);
if (!Number.isInteger(port) || port < 1024 || port > 65535) {
  throw new Error("Invalid E2E server port");
}
const init = spawnSync(
  binary,
  [
    "init",
    "--dir",
    directory,
    "--password-stdin",
    "--listen",
    `127.0.0.1:${port}`,
  ],
  { input: "Test-Only-Panel-Password-2026\n", encoding: "utf8" },
);
if (init.status !== 0) {
  rmSync(directory, { recursive: true, force: true });
  throw new Error(init.stderr || "Build the panel with make build first");
}
const configPath = path.join(directory, "config.yaml");
const config = readFileSync(configPath, "utf8");
if (!/^helper:.*$/m.test(config)) {
  rmSync(directory, { recursive: true, force: true });
  throw new Error(
    "Init output has no helper field; refusing an unsafe test instance",
  );
}
writeFileSync(
  configPath,
  config.replace(/^helper:.*$/m, "helper: /nonexistent/lnmp-gopanel-control"),
);
// Browser tests must never execute operations against installed host services.
writeFileSync(path.join(directory, "services.yaml"), "services: []\n");
const child = spawn(
  binary,
  [
    "--config",
    path.join(directory, "config.yaml"),
    "--services",
    path.join(directory, "services.yaml"),
    "--allow-root-for-development",
  ],
  { stdio: "inherit" },
);
for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, () => child.kill(signal));
child.on("exit", (code) => {
  rmSync(directory, { recursive: true, force: true });
  process.exit(code ?? 0);
});
