#!/usr/bin/env python3
"""Exercise real release binaries, sudo and systemd on a disposable CI machine.

Never run on a server with an existing panel installation. Does not install LNMP.
"""

import argparse
import hashlib
import grp
import http.cookiejar
import json
import os
from pathlib import Path
import pwd
import re
import shutil
import ssl
import stat
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.request

PROJECT = Path(__file__).resolve().parents[1]
PASSWORD = "Integration-Only-Panel-Password-2026!"
PANEL = "/usr/local/bin/lnmp-gopanel"
HELPER = "/usr/local/libexec/lnmp-gopanel-control"
CONFIG = Path("/etc/lnmp-gopanel/config.yaml")
STATE = Path("/var/lib/lnmp-gopanel")
BACKUPS = Path("/var/backups/lnmp-gopanel")
UNIT = Path("/etc/systemd/system/lnmp-gopanel.service")
SUDOERS = Path("/etc/sudoers.d/lnmp-gopanel")
TARGETS = [Path(PANEL), Path(HELPER), UNIT, SUDOERS, CONFIG,
           CONFIG.with_name("services.yaml"), CONFIG.parent / "tls/panel.crt",
           CONFIG.parent / "tls/panel.key"]


def run(*args, success=True, **kwargs):
    result = subprocess.run(args, text=True, capture_output=True, timeout=600, **kwargs)
    if (result.returncode == 0) != success:
        raise AssertionError(f"Command failed expectation: {args}\n{result.stdout}\n{result.stderr}")
    return result.stdout + result.stderr


def guard():
    guest = Path("/run/gopanel-ci-guest")
    hosted = os.environ.get("GITHUB_ACTIONS") == "true" and os.environ.get("RUNNER_ENVIRONMENT") == "github-hosted"
    if os.geteuid() != 0 or not (hosted or (guest.is_file() and guest.read_text().strip() == "disposable")):
        raise SystemExit("Real install tests require a disposable hosted CI runner or marked guest VM")
    if Path("/proc/1/comm").read_text().strip() != "systemd":
        raise SystemExit("systemd must be PID 1")
    occupied = [path for path in TARGETS + [STATE, CONFIG.parent, BACKUPS, Path(str(UNIT) + ".d")]
                if path.exists() or path.is_symlink()]
    if occupied:
        raise SystemExit(f"Refusing to overwrite an existing installation: {occupied}")
    for lookup in (pwd.getpwnam, grp.getgrnam):
        try:
            lookup("lnmp-panel")
        except KeyError:
            continue
        raise SystemExit("Refusing to reuse an existing lnmp-panel account or group")


def snapshot():
    result = {}
    for path in TARGETS:
        info = path.stat()
        result[str(path)] = (hashlib.sha256(path.read_bytes()).hexdigest(),
                             stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid)
    return result


def prepare_hosted_machine():
    # Hosted images use writable npm directories and a Chromium snapshot outside
    # the distro's AppArmor profile. Give this disposable machine normal parent
    # permissions and a userns grant for that exact browser, keeping its sandbox.
    run("install", "-d", "-o", "root", "-g", "root", "-m", "0755", "/usr/local/bin")
    browser = shutil.which("chromium")
    restriction = Path("/proc/sys/kernel/apparmor_restrict_unprivileged_userns")
    if (browser and Path(browser).resolve() == Path("/usr/local/share/chromium/chrome-linux/chrome")
            and restriction.is_file() and restriction.read_text().strip() == "1"):
        profile = Path("/etc/apparmor.d/gopanel-ci-chromium")
        profile.write_text('abi <abi/4.0>,\ninclude <tunables/global>\n'
                           'profile gopanel-ci-chromium /usr/local/share/chromium/chrome-linux/chrome flags=(unconfined) {\n'
                           '  userns,\n}\n')
        run("apparmor_parser", "-r", str(profile))


def endpoint():
    listen = run("sudo", "-n", "-u", "lnmp-panel", "--", PANEL, "https", "--check").strip()
    port = listen.rsplit(":", 1)[1]
    return f"https://127.0.0.1:{port}"


def http_checks(version, helper_status="ready"):
    base = endpoint()
    context = ssl.create_default_context(cafile=str(CONFIG.parent / "tls/panel.crt"))
    cookies = http.cookiejar.CookieJar()
    client = urllib.request.build_opener(urllib.request.HTTPSHandler(context=context),
                                        urllib.request.HTTPCookieProcessor(cookies))

    def request(path, data=None, headers=None, method=None):
        req = urllib.request.Request(base + path, data=data, headers=headers or {}, method=method)
        return client.open(req, timeout=15)

    deadline = time.monotonic() + 30
    while True:
        try:
            with request("/") as response:
                html = response.read().decode()
                assert response.headers["Cache-Control"] == "no-store"
            break
        except (OSError, urllib.error.URLError):
            if time.monotonic() >= deadline:
                raise
            time.sleep(0.25)
    try:
        request("/api/v1/auth/session")
        raise AssertionError("Anonymous session was accepted")
    except urllib.error.HTTPError as error:
        assert error.code == 401
    data = json.dumps({"username": "admin", "password": PASSWORD}).encode()
    with request("/api/v1/auth/login", data, {"Content-Type": "application/json", "Origin": base}) as response:
        assert response.status == 200
        assert "Secure" in response.headers["Set-Cookie"]
    with request("/api/v1/build") as response:
        assert json.load(response)["version"] == version
    with request("/api/v1/auth/session") as response:
        assert json.load(response)["username"] == "admin"
    deadline = time.monotonic() + 30
    while True:
        with request("/api/v1/components") as response:
            components = json.load(response)
        if (components["status"] == helper_status
                or (helper_status == "ready" and components["status"] == "partial")):
            if helper_status == "ready":
                assert len(components["items"]) == 6, components
                assert any(item["name"] == "python3" and item["status"] == "ready"
                           for item in components["items"]), components
            break
        if time.monotonic() >= deadline or (helper_status == "ready" and components["status"] == "failed"):
            raise AssertionError(f"Helper failed inside the actual service namespace: {components}")
        time.sleep(0.2)
    assets = re.findall(r'(?:src|href)="(/assets/[^\"]+)"', html)
    assert assets, "Installed binary has no embedded frontend assets"
    for path in assets:
        with request(path) as response:
            assert response.status == 200 and response.read()
            assert "immutable" in response.headers["Cache-Control"]
    with request("/services") as response:
        assert response.read().decode() == html
    with request(assets[0], method="HEAD") as response:
        assert not response.read() and int(response.headers["Content-Length"]) > 0
    return base


def privileges():
    pid = run("systemctl", "show", "lnmp-gopanel", "--property=MainPID", "--value").strip()
    status = Path(f"/proc/{int(pid)}/status").read_text()
    account = pwd.getpwnam("lnmp-panel")
    uid = account.pw_uid
    assert re.search(rf"^Uid:\s+{uid}\s+{uid}\s+{uid}\s+{uid}$", status, re.M)
    assert re.search(r"^NoNewPrivs:\s+0$", status, re.M)
    assert STATE.stat().st_uid == uid and stat.S_IMODE(STATE.stat().st_mode) == 0o700
    for name in ("auth-state.json", "auth.key", "auth.lock"):
        info = (STATE / name).stat()
        assert info.st_uid == uid and stat.S_IMODE(info.st_mode) == 0o600
    for path, mode in ((CONFIG, 0o640), (CONFIG.parent, 0o750),
                       (CONFIG.parent / "tls", 0o750), (CONFIG.parent / "tls/panel.crt", 0o640),
                       (CONFIG.parent / "tls/panel.key", 0o640)):
        info = path.stat()
        assert (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) == (0, account.pw_gid, mode)
    for path, mode in ((Path(PANEL), 0o755), (Path(HELPER), 0o755), (SUDOERS, 0o440)):
        info = path.stat()
        assert (info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) == (0, 0, mode)
    prefix = ("sudo", "-n", "-u", "lnmp-panel", "--", "sudo", "-n", "--")
    for name in ("components", "docker"):
        json.loads(run(*prefix, HELPER, name, "snapshot"))
    for args in [("/bin/sh", "-c", "id"), (HELPER, "components", "snapshot", "extra")]:
        run(*prefix, *args, success=False)


def mutated_archive(original, directory, fault):
    payload = directory / "payload"
    payload.mkdir()
    with tarfile.open(original) as archive:
        for member in archive.getmembers():
            target = payload / member.name
            if not target.resolve().is_relative_to(payload.resolve()) or not (member.isdir() or member.isfile()):
                raise AssertionError("Unsafe package member")
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(archive.extractfile(member).read())
                target.chmod(member.mode & 0o777)
    root = payload / "lnmp-gopanel"
    unit = root / "deploy/lnmp-gopanel.service"
    sudoers = root / "deploy/lnmp-gopanel.sudoers"
    if fault == "sudo":
        sudoers.write_text(sudoers.read_text().replace("components snapshot", "components denied"))
    elif fault == "certificate":
        unit.write_text(unit.read_text() + "\n[Service]\nExecStartPre=+/usr/bin/chmod 0600 /etc/lnmp-gopanel/tls/panel.key\n")
    elif fault == "privileges":
        unit.write_text(unit.read_text().replace("NoNewPrivileges=false", "NoNewPrivileges=true"))
    destination = directory / original.name
    with tarfile.open(destination, "w:gz") as archive:
        archive.add(root, arcname="lnmp-gopanel")
    destination.with_name(destination.name + ".sha256").write_text(
        hashlib.sha256(destination.read_bytes()).hexdigest() + "  " + destination.name + "\n")
    return destination


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", required=True, type=Path)
    parser.add_argument("--browser-contract", action="store_true")
    parser.add_argument("--screenshots-test", type=Path)
    args = parser.parse_args()
    guard()  # Always before installing packages, changing accounts or touching /etc.
    prepare_hosted_machine()
    archive = args.archive.resolve()
    installer = str(PROJECT / "install.sh")
    installed = False
    try:
        # Run from outside the source tree: frontend files must come from embed.
        with tempfile.TemporaryDirectory(prefix="gopanel-systemd-") as work:
            options = ("bash", installer, "--archive", str(archive))
            installed = True  # A failed fresh install may still leave a dedicated account.
            print("Checking fresh install with real sudo, systemd and Chromium", flush=True)
            run(*options, "--password-stdin", "--tls-mode", "self-signed", "--with-screenshots",
                input=PASSWORD + "\n", cwd=work)
            with tarfile.open(archive) as package:
                version = package.extractfile("lnmp-gopanel/VERSION").read().decode().strip()
            base = http_checks(version)
            privileges()
            baseline = snapshot()
            auth = {name: (STATE / name).read_bytes() for name in ("auth-state.json", "auth.key")}
            print("Checking upgrade and credential/configuration preservation", flush=True)
            run(*options, cwd=work)
            assert snapshot() == baseline, "Upgrade changed preserved configuration or package files"
            assert all((STATE / name).read_bytes() == content for name, content in auth.items())
            run("systemctl", "restart", "lnmp-gopanel")
            assert http_checks(version) == base
            for fault in ("sudo", "certificate", "privileges"):
                print(f"Checking real rollback after {fault} failure", flush=True)
                fault_dir = Path(work) / fault
                fault_dir.mkdir()
                broken = mutated_archive(archive, fault_dir, fault)
                message = run("bash", installer, "--archive", str(broken), success=False, cwd=work)
                expected = {"sudo": "Panel account cannot use helper: components snapshot",
                            "certificate": "Panel did not start",
                            "privileges": "NoNewPrivs=1"}
                assert expected[fault] in message, f"{fault}: did not reach the intended failure check: {message}"
                assert snapshot() == baseline, f"{fault}: managed files were not restored"
                run("systemctl", "is-active", "--quiet", "lnmp-gopanel")
                assert http_checks(version) == base
            run("sudo", "-n", "-u", "lnmp-panel", "--", PANEL, "check-screenshots")
            if args.screenshots_test:
                browser = shutil.which("chromium") or shutil.which("google-chrome")
                assert browser, "A native sandboxed browser is required"
                # Copy out of the source/home directory so the service user can traverse it.
                test_dir = Path(tempfile.mkdtemp(prefix="gopanel-browser-test-"))
                try:
                    test_dir.chmod(0o755)
                    executable = test_dir / "screenshots.test"
                    shutil.copy2(args.screenshots_test, executable)
                    executable.chmod(0o755)
                    properties = []
                    inherited = {"User", "Group", "UMask", "NoNewPrivileges", "ProtectKernelTunables",
                                 "ProtectKernelModules", "ProtectControlGroups", "RestrictRealtime", "LockPersonality"}
                    for line in UNIT.read_text().splitlines():
                        key, sep, value = line.partition("=")
                        key, value = key.strip(), value.strip()
                        if sep and key in inherited:
                            properties.append(f"--property={key}={value}")
                    assert "--property=User=lnmp-panel" in properties
                    assert "--property=Group=lnmp-panel" in properties
                    output = run("systemd-run", "--quiet", "--wait", "--pipe", "--collect",
                        "--unit=gopanel-ci-browser", *properties,
                        f"--setenv=SCREENSHOT_TEST_BROWSER={browser}",
                        str(executable), "-test.run=^TestChromiumIntegration", "-test.v", "-test.timeout=180s", cwd="/tmp")
                    for name in ("TestChromiumIntegration", "TestChromiumIntegrationLanguageAndDelayedCSS",
                                 "TestChromiumIntegrationZoom", "TestChromiumIntegrationViewportAndJavaScript"):
                        assert re.search(r"^--- PASS: " + name + r" ", output, re.M), f"Missing sandbox test: {name}\n{output}"
                    assert "--- SKIP:" not in output, output
                    print("All four Chromium integrations passed with production unit restrictions", flush=True)
                finally:
                    shutil.rmtree(test_dir)
            if args.browser_contract:
                env = dict(os.environ, GOPANEL_E2E_BASE_URL=base, GOPANEL_E2E_PASSWORD=PASSWORD)
                run("npm", "run", "test:e2e:installed", cwd=PROJECT / "web", env=env)
                original = SUDOERS.read_text()
                try:
                    SUDOERS.write_text(original.replace("components snapshot", "components denied"))
                    run("visudo", "-cf", str(SUDOERS))
                    run("systemctl", "restart", "lnmp-gopanel")
                    http_checks(version, helper_status="failed")
                    run("npm", "run", "test:e2e:installed", cwd=PROJECT / "web",
                        env=dict(env, GOPANEL_EXPECT_HELPER_ERROR="1"))
                finally:
                    SUDOERS.write_text(original)
            run("systemctl", "stop", "lnmp-gopanel")
            run("bash", installer, "--uninstall", "--yes", cwd=work)
            run("bash", installer, "--uninstall", "--yes", cwd=work)
            assert not any(path.exists() for path in TARGETS + [STATE, CONFIG.parent])
            installed = False
        print("Real package, permissions, systemd, rollback and sandbox checks passed")
    finally:
        if installed:
            diagnostics = PROJECT / "test-results"
            diagnostics.mkdir(parents=True, exist_ok=True)
            output = subprocess.run(["journalctl", "-u", "lnmp-gopanel", "--no-pager", "-n", "300"],
                                    text=True, capture_output=True, timeout=30)
            (diagnostics / "systemd-journal.txt").write_text(output.stdout + output.stderr)
            output = subprocess.run(["journalctl", "-k", "--no-pager", "-n", "100"],
                                    text=True, capture_output=True, timeout=30)
            (diagnostics / "kernel-journal.txt").write_text(output.stdout + output.stderr)
            # This script owns this install, having refused any existing installation.
            subprocess.run(["bash", installer, "--uninstall", "--yes"], timeout=60, check=False)


if __name__ == "__main__":
    main()
