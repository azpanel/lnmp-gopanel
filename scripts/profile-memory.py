#!/usr/bin/env python3
"""Compare isolated panel binaries and GC settings without installing them.

Example: python3 scripts/profile-memory.py --before /tmp/panel-before \
    --after /tmp/panel-after --output test-results/memory-profile.json
"""

import argparse
import concurrent.futures
import http.cookiejar
import json
import os
from pathlib import Path
import re
import ssl
import statistics
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request


def process_stats(pid):
    status = Path(f"/proc/{pid}/status").read_text()
    values = {key: int(value) * 1024 for key, value in
              re.findall(r"^(VmRSS|VmHWM|RssAnon):\s+(\d+) kB$", status, re.M)}
    fields = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()
    values["cpu_seconds"] = (int(fields[11]) + int(fields[12])) / os.sysconf("SC_CLK_TCK")
    return values


def measure(child, work):
    samples = []
    done = threading.Event()

    def sample():
        while not done.is_set():
            try:
                samples.append(process_stats(child.pid))
            except FileNotFoundError:
                break
            done.wait(0.02)

    start = process_stats(child.pid)
    started = time.monotonic()
    sampler = threading.Thread(target=sample, daemon=True)
    sampler.start()
    try:
        latencies = work()
        end = process_stats(child.pid)
    finally:
        done.set()
        sampler.join()
    samples.extend([start, end])
    elapsed = time.monotonic() - started
    result = {
        "elapsed_seconds": elapsed,
        "cpu_seconds": end["cpu_seconds"] - start["cpu_seconds"],
        "rss_median_bytes": statistics.median(s["VmRSS"] for s in samples),
        "rss_peak_bytes": max(s["VmRSS"] for s in samples),
        "rss_final_bytes": end["VmRSS"],
        "anonymous_final_bytes": end["RssAnon"],
        "lifetime_rss_peak_bytes": end["VmHWM"],
    }
    if latencies:
        result["requests"] = len(latencies)
        result["latency_p50_ms"] = statistics.median(latencies) * 1000
        result["latency_p95_ms"] = sorted(latencies)[int((len(latencies) - 1) * 0.95)] * 1000
    return result


def profile(label, binary, gogc, args, idle_barrier, workload_lock):
    with tempfile.TemporaryDirectory(prefix="gopanel-memory-") as directory:
        root = Path(directory)
        config_dir = root / "config"
        password = "Memory-Profile-Isolated-Password-2026"
        environment = dict(os.environ)
        for key in ("GOGC", "GOMEMLIMIT", "GODEBUG"):
            environment.pop(key, None)
        if gogc is not None:
            environment["GOGC"] = str(gogc)
        subprocess.run([str(binary), "init", "--dir", str(config_dir),
                        "--listen", "127.0.0.1:0", "--password-stdin"],
                       cwd=root, env=environment, input=password + "\n", text=True,
                       stdout=subprocess.DEVNULL, check=True, timeout=30)
        config = config_dir / "config.yaml"
        content = config.read_text()
        listen = re.search(r"^listen: (.+)$", content, re.M).group(1)
        # Never invoke the installed host helper or manage host services.
        content, replaced = re.subn(r"^helper:.*$", "helper: /nonexistent/memory-profile-helper",
                                   content, flags=re.M)
        assert replaced == 1, "Cannot safely disable the host helper"
        config.write_text(content)
        services = config_dir / "services.yaml"
        services.write_text("services: []\n")
        command = [str(binary), "--config", str(config), "--services", str(services)]
        if os.geteuid() == 0:
            command.append("--allow-root-for-development")
        with (root / "panel.log").open("w+") as log:
            child = subprocess.Popen(command, cwd=root, env=environment, stdout=log, stderr=log)
            try:
                context = ssl.create_default_context(cafile=str(config_dir / "tls/panel.crt"))
                jar = http.cookiejar.CookieJar()
                client = urllib.request.build_opener(urllib.request.HTTPSHandler(context=context),
                                                    urllib.request.HTTPCookieProcessor(jar))
                base = "https://" + listen

                def request(path, data=None, headers=None):
                    req = urllib.request.Request(base + path, data=data, headers=headers or {})
                    with client.open(req, timeout=15) as response:
                        return response.read()

                deadline = time.monotonic() + 20
                while True:
                    try:
                        request("/")
                        break
                    except urllib.error.URLError:
                        if child.poll() is not None or time.monotonic() >= deadline:
                            raise
                        time.sleep(0.1)
                session = json.loads(request("/api/v1/auth/login",
                    json.dumps({"username": "admin", "password": password}).encode(),
                    {"Content-Type": "application/json", "Origin": base}))
                cookie = "; ".join(f"{c.name}={c.value}" for c in jar)
                login_log = config_dir / "logs-state/login.log"
                event = json.dumps({"time": "2026-01-01T00:00:00Z", "ip": "127.0.0.1",
                                    "ua": "memory profile " * 20, "username": "admin",
                                    "result": "success", "stage": "password"}) + "\n"
                with login_log.open("a") as output:
                    output.write(event * 4000)
                assets = sorted(args.assets.glob("*.js"), key=lambda p: p.stat().st_size,
                                reverse=True)[:8]
                assert assets, "No built assets found"

                def workload(kind):
                    def run(index):
                        headers = {"Accept-Encoding": "gzip" if index % 2 else "identity"}
                        data = None
                        path = "/assets/" + assets[index % len(assets)].name
                        if kind == "logs":
                            path = "/api/v1/logs/read"
                            data = json.dumps({"path": str(login_log), "mode": "tail", "rules": []}).encode()
                            headers.update({"Content-Type": "application/json", "Origin": base,
                                            "Cookie": cookie, "X-CSRF-Token": session["csrf_token"]})
                        started = time.monotonic()
                        # A separate opener avoids sharing CookieJar mutation between workers.
                        worker = urllib.request.build_opener(urllib.request.HTTPSHandler(context=context))
                        with worker.open(urllib.request.Request(base + path, data=data, headers=headers),
                                         timeout=15) as response:
                            assert response.status == 200 and response.read()
                        return time.monotonic() - started

                    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
                        return list(pool.map(run, range(args.requests)))

                result = {"binary": str(binary), "gogc": gogc or "default"}
                print(f"{label}: idle {args.idle_seconds}s", flush=True)
                result["idle"] = measure(child, lambda: time.sleep(args.idle_seconds))
                idle_barrier.wait(timeout=60)
                with workload_lock:
                    for kind in ("static", "logs"):
                        result[kind] = measure(child, lambda: workload(kind))
                print(f"{label}: finished", flush=True)
                return result
            except BaseException:
                idle_barrier.abort()
                log.flush()
                print((root / "panel.log").read_text(), flush=True)
                raise
            finally:
                child.terminate()
                try:
                    child.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--before", type=Path, required=True)
    parser.add_argument("--after", type=Path, required=True)
    parser.add_argument("--assets", type=Path, default=Path(__file__).resolve().parents[1] / "web/dist/assets")
    parser.add_argument("--idle-seconds", type=float, default=600)
    parser.add_argument("--requests", type=int, default=200)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if args.idle_seconds < 0 or args.requests < 1:
        parser.error("idle-seconds must be nonnegative and requests must be positive")
    variants = [("before", args.before.resolve(), None),
                ("after", args.after.resolve(), None),
                ("after_gogc50", args.after.resolve(), 50)]
    # Idle samples can overlap; request workloads run sequentially to avoid
    # cross-variant load affecting CPU and latency comparisons.
    results = {}
    idle_barrier = threading.Barrier(len(variants))
    workload_lock = threading.Lock()
    with concurrent.futures.ThreadPoolExecutor(max_workers=len(variants)) as pool:
        pending = {pool.submit(profile, label, binary, gogc, args, idle_barrier, workload_lock): label
                   for label, binary, gogc in variants}
        for future in concurrent.futures.as_completed(pending):
            results[pending[future]] = future.result()
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(json.dumps(results, ensure_ascii=False, indent=2) + "\n")
    print(f"Saved {args.output}")


if __name__ == "__main__":
    main()
