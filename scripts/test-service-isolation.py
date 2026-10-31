#!/usr/bin/env python3
"""Reproduce daemon cgroup inheritance and verify the actual Go service runner.

Requires root, systemd and Go (or --fixture-binary). Uses only uniquely named
transient units and simulated init scripts; never starts/stops installed services.
"""

import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import uuid

PROJECT = Path(__file__).resolve().parents[1]
PREFIX = "gopanel-isolation-test-"


def run(*args, **kwargs):
    result = subprocess.run(args, text=True, capture_output=True, timeout=90, **kwargs)
    if result.returncode:
        raise AssertionError(f"Command failed: {args}\n{result.stdout}\n{result.stderr}")
    return result.stdout


def live(record):
    try:
        fields = Path(f"/proc/{record['pid']}/stat").read_text().split(") ", 1)[1].split()
        return fields[0] != "Z" and fields[19] == record["start"]
    except FileNotFoundError:
        return False


def cgroup(record):
    for line in record["cgroup"].splitlines():
        number, controllers, path = line.split(":", 2)
        if number == "0" or controllers == "name=systemd":
            return path
    raise AssertionError("No systemd cgroup found")


def wait_for(check, description):
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        result = check()
        if result:
            return result
        time.sleep(0.05)
    raise AssertionError("Timed out: " + description)


def simulator(path, state):
    # Both fork and setsid are intentional: detaching from the process group is
    # insufficient to escape the caller's cgroup, which is the original bug.
    path.write_text("""#!/usr/bin/python3
import ctypes,json,os,signal,sys,time
from pathlib import Path
STATE=Path(%r)
def current():
    try:
        r=json.loads(STATE.read_text())
        fields=Path('/proc/%%d/stat'%%r['pid']).read_text().split(') ',1)[1].split()
        if fields[0]!='Z' and fields[19]==r['start']: return r
    except (FileNotFoundError,json.JSONDecodeError): pass
    return None
action=sys.argv[1]
if action not in ('start','stop','restart'): sys.exit(64)
r=current()
if action in ('stop','restart') and r:
    os.kill(r['pid'],signal.SIGTERM)
    until=time.monotonic()+5
    while current() and time.monotonic()<until: time.sleep(.02)
    if current(): sys.exit(1)
    STATE.unlink(missing_ok=True)
if action=='stop': sys.exit(0)
if current(): sys.exit(0)
if os.fork(): os._exit(0)
os.setsid()
if os.fork(): os._exit(0)
fd=os.open('/dev/null',os.O_RDWR)
for target in (0,1,2): os.dup2(fd,target)
if fd>2: os.close(fd)
ctypes.CDLL(None).prctl(15,b'php-fpm',0,0,0)
pid=os.getpid()
start=Path('/proc/self/stat').read_text().split(') ',1)[1].split()[19]
data={'pid':pid,'start':start,'cgroup':Path('/proc/self/cgroup').read_text().strip()}
temporary=STATE.with_suffix('.new')
temporary.write_text(json.dumps(data))
temporary.replace(STATE)
while True: signal.pause()
""" % str(state))
    path.chmod(0o755)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixture-binary", type=Path)
    args = parser.parse_args()
    if os.geteuid() != 0 or Path("/proc/1/comm").read_text().strip() != "systemd":
        raise SystemExit("Run as root on a host with systemd as PID 1")
    with tempfile.TemporaryDirectory(prefix="gopanel-service-isolation-", dir="/var/tmp") as work:
        root = Path(work)
        binary = args.fixture_binary.resolve() if args.fixture_binary else root / "control.test"
        if args.fixture_binary is None:
            run("go", "test", "-c", "-o", str(binary), "./internal/control", cwd=PROJECT,
                env=dict(os.environ, CGO_ENABLED="0"))
        parents, scopes = set(), set()
        installer = (PROJECT / "install.sh").read_text()
        safety = "check_panel_cgroup_safety() {" + installer.split("check_panel_cgroup_safety() {", 1)[1].split("\nstop_recovery_service()", 1)[0]

        def record(state):
            try:
                value = json.loads(state.read_text())
                group = cgroup(value).rsplit("/", 1)[-1]
                if group.startswith("lnmp-gopanel-managed-php-fpm-") and group.endswith(".scope"):
                    scopes.add(group)
                return value
            except (FileNotFoundError, json.JSONDecodeError):
                return None

        def launch(unit, mode, script, action="start", oneshot=False):
            assert unit.startswith(PREFIX) and unit.endswith(".service")
            parents.add(unit)
            command = ["/usr/bin/systemd-run", "--quiet", "--collect", "--unit=" + unit,
                       "--property=Type=exec", "--property=TimeoutStopSec=3s",
                       "--property=RuntimeMaxSec=40s", "--property=ProtectControlGroups=true",
                       "--setenv=GOPANEL_TEST_SERVICE_SCOPE_MODE=" + mode,
                       "--setenv=GOPANEL_TEST_SERVICE_SCOPE_SCRIPT=" + str(script),
                       "--setenv=GOPANEL_TEST_SERVICE_SCOPE_ACTION=" + action]
            if oneshot:
                command += ["--wait", "--pipe", "--setenv=GOPANEL_TEST_SERVICE_SCOPE_ONESHOT=1"]
            run(*command, str(binary), "-test.run=^TestServiceScopeFixture$", "-test.timeout=35s")

        try:
            for mode in ("raw", "scope"):
                state = root / (mode + ".json")
                script = root / (mode + "-init.py")
                simulator(script, state)
                unit = PREFIX + mode + "-" + uuid.uuid4().hex[:10] + ".service"
                launch(unit, mode, script)
                before = wait_for(lambda: record(state), "initial simulated daemon")
                assert live(before), before
                if mode == "raw":
                    assert cgroup(before).endswith("/" + unit), before
                else:
                    assert cgroup(before).rsplit("/", 1)[-1] in scopes, before
                # Exercise the installer's actual read-only preflight against
                # real /proc and real systemd units. Never run the installer.
                check = subprocess.run(["/bin/bash", "-c", "set -euo pipefail\n" + safety + '\ncheck_panel_cgroup_safety "$1"',
                                        "safety-check", unit], text=True, capture_output=True, timeout=15)
                if mode == "raw":
                    assert check.returncode != 0 and f"PID={before['pid']}" in check.stderr, check
                    assert live(before), "Read-only preflight disturbed the legacy daemon"
                    print("升级保护验证：检测到真实控制组内的遗留业务进程并拒绝继续，原进程保持运行。", flush=True)
                else:
                    assert check.returncode == 0, check.stderr
                    print("升级保护验证：独立 scope 内的业务进程不会阻止升级。", flush=True)
                run("/usr/bin/systemctl", "restart", unit)
                if mode == "raw":
                    wait_for(lambda: not live(before), "original daemon stopped with parent")
                    after = wait_for(lambda: (r if (r := record(state)) and r["pid"] != before["pid"] else None),
                                     "replacement simulated daemon")
                    print("已复现：原启动方式的守护进程属于模拟面板控制组，面板重启会带停它。", flush=True)
                else:
                    run("/usr/bin/systemctl", "is-active", "--quiet", unit)
                    assert live(before), "Scoped daemon died on panel restart"
                    assert record(state)["pid"] == before["pid"], "Daemon unexpectedly restarted"
                    # Explicitly stopping the caller must also leave the daemon alive.
                    run("/usr/bin/systemctl", "stop", unit)
                    assert live(before), "Scoped daemon died on panel stop"
                    print("修复验证：独立 scope 中的守护进程在模拟面板重启、停止后保持原 PID。", flush=True)
                    operation = PREFIX + "restart-" + uuid.uuid4().hex[:10] + ".service"
                    launch(operation, mode, script, "restart", True)
                    after = wait_for(lambda: (r if (r := record(state)) and r["pid"] != before["pid"] else None),
                                     "explicit daemon restart")
                    assert not live(before) and live(after), "Managed restart did not replace daemon"
                    operation = PREFIX + "stop-" + uuid.uuid4().hex[:10] + ".service"
                    launch(operation, mode, script, "stop", True)
                    wait_for(lambda: not live(after), "explicit daemon stop")
                    for scope in list(scopes):
                        wait_for(lambda: run("/usr/bin/systemctl", "show", scope, "--property=LoadState", "--value").strip() == "not-found",
                                 "empty scope collection")
                    print("修复验证：显式重启、停止仍正常执行，空 scope 自动回收。", flush=True)
                if mode == "raw":
                    run("/usr/bin/systemctl", "stop", unit)
        finally:
            for state in root.glob("*.json"):
                record(state)
            for unit in parents | scopes:
                subprocess.run(["/usr/bin/systemctl", "stop", unit], capture_output=True, timeout=15)
                subprocess.run(["/usr/bin/systemctl", "reset-failed", unit], capture_output=True, timeout=15)
        print("临时测试服务、scope 和文件已清理。", flush=True)


if __name__ == "__main__":
    main()
