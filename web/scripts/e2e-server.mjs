import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
const directory = mkdtempSync(path.join(tmpdir(), "lnmp-gopanel-e2e-"));
const binary = path.resolve("../bin/lnmp-gopanel");
const init = spawnSync(
  binary,
  ["init", "--dir", directory, "--password-stdin"],
  { input: "Test-Only-Panel-Password-2026\n", encoding: "utf8" },
);
if (init.status !== 0) {
  rmSync(directory, { recursive: true, force: true });
  throw new Error(init.stderr || "Build the panel with make build first");
}
writeFileSync(
  path.join(directory, "config.yaml"),
  readFileSync(path.join(directory, "config.yaml"), "utf8").replace(
    "127.0.0.1:8090",
    "127.0.0.1:18091",
  ),
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
