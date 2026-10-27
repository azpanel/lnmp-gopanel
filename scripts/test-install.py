#!/usr/bin/env python3
"""Exercise the installer in disposable chroots; never change the host install."""

import fcntl
import hashlib
import io
import os
from pathlib import Path
import platform
import pty
import re
import shutil
import subprocess
import tarfile
import tempfile
import unittest

PROJECT = Path(__file__).resolve().parents[1]
ARCH = "arm64" if platform.machine() in ("aarch64", "arm64") else "amd64"
ASSET = f"lnmp-gopanel_linux_{ARCH}.tar.gz"
TARGETS = (
    "/usr/local/bin/lnmp-gopanel",
    "/usr/local/libexec/lnmp-gopanel-control",
    "/etc/systemd/system/lnmp-gopanel.service",
    "/etc/sudoers.d/lnmp-gopanel",
    "/etc/lnmp-gopanel/config.yaml",
    "/etc/lnmp-gopanel/services.yaml",
    "/etc/lnmp-gopanel/tls/panel.crt",
    "/etc/lnmp-gopanel/tls/panel.key",
)
VHOST_COMMANDS = tuple(f"vhosts {action}" for action in ("list", "config", "usage", "toggle", "options", "create"))


def copy_executable(root, command):
    program = shutil.which(command)
    if program is None:
        raise RuntimeError(f"Missing test dependency: {command}")
    destination = root / program.lstrip("/")
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(program, destination, follow_symlinks=True)
    result = subprocess.run(["ldd", program], capture_output=True, text=True)
    for library in set(re.findall(r"(/[^\s()]+)", result.stdout)):
        source = Path(library)
        if source.is_file():
            target = root / source.relative_to("/")
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target, follow_symlinks=True)


def write(root, name, content, mode=0o644):
    target = root / name.lstrip("/")
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content)
    target.chmod(mode)
    return target


SYSTEMCTL = """#!/bin/bash
printf '%s\\n' "$*" >> /run/systemctl.log
case "$1" in
  is-active) [[ -f /run/panel.active ]] ;;
  is-enabled)
    if [[ ! -f /etc/systemd/system/lnmp-gopanel.service ]]; then
      printf 'Failed to get unit file state for lnmp-gopanel.service: No such file or directory\\n' >&2
      exit 1
    fi
    [[ -f /run/panel.enabled ]] ;;
  enable) touch /run/panel.enabled ;;
  disable) rm -f /run/panel.enabled ;;
  restart) [[ ! -f /run/fail-restart ]] || exit 1; touch /run/panel.active ;;
  stop) [[ ! -f /run/fail-stop ]] || exit 1; rm -f /run/panel.active ;;
  daemon-reload|reset-failed) exit 0 ;;
  show)
    if [[ "$*" == *--property=LoadState* ]]; then
      if [[ -e /etc/systemd/system/lnmp-gopanel.service || -f /run/panel.active ]]; then
        printf 'loaded\\n'
      else
        printf 'not-found\\n'
      fi
    else
      cat /run/panel.pid
    fi ;;
  *) exit 1 ;;
esac
"""
SUDO = """#!/bin/bash
# Model sudo argument handling and failures; no host privilege changes occur.
printf '%s\\n' "$*" >> /run/sudo.log
if [[ "$1" == -n && "$2" == -u && "$3" == lnmp-panel && "$4" == -- ]]; then
  shift 4
  exec "$@"
fi
[[ "$1" == -n ]] || exit 64
shift
if [[ "$1" == -l && "$2" == -- ]]; then
  shift 2
  if [[ -f /run/deny-helper-command && "$*" == "$(cat /run/deny-helper-command)" ]]; then
    printf 'sudo: command not allowed\\n' >&2
    exit 1
  fi
  [[ "$1" == /usr/local/libexec/lnmp-gopanel-control ]] || exit 64
  exit 0
fi
[[ "$1" == -- ]] || exit 64
shift
exec "$@"
"""
HELPER = """#!/bin/bash
printf '%s\\n' "$*" >> /run/helper.log
[[ $# == 2 && "$2" == snapshot && ( "$1" == docker || "$1" == components ) ]] || exit 64
if [[ "$1" == components ]]; then
  if [[ -f /run/fail-components-helper ]]; then
    printf 'components helper execution failed\\n' >&2
    exit 1
  fi
  printf '{"status":"ready","items":[{"name":"python3","status":"missing"},{"name":"go","status":"missing"},{"name":"node","status":"missing"},{"name":"codex","status":"missing"},{"name":"composer","status":"missing"}]}\\n'
  exit 0
fi
if [[ -f /run/fail-helper ]]; then
  printf '{"state":"unknown","message":"helper self-check failed"}\\n'
  printf 'helper execution failed\\n' >&2
  exit 1
fi
printf '{"status":"unavailable","message":"Docker not installed"}\\n'
"""
PANEL = """#!/bin/bash
if [[ "$1" == init && "$2" == --help ]]; then
  printf 'Usage of init:\\n  -tls-dir string\\n  -certificate-dir string\\n' >&2
  exit 1
fi
if [[ "$1" == https ]]; then
  shift
  config=/etc/lnmp-gopanel/config.yaml
  check=false
  while (($#)); do
    case "$1" in
      --check) check=true; shift ;;
      --list-certificates)
        for dir in /usr/local/nginx/conf/ssl/*; do
          [[ -f "$dir/fullchain.pem" && -f "$dir/privkey.pem" ]] && printf '%s\\n' "$dir"
        done
        exit 0 ;;
      --config) config="$2"; shift 2 ;;
      --tls-dir) tls_dir="$2"; shift 2 ;;
      --certificate-dir) certificate_dir="$2"; shift 2 ;;
      *) exit 64 ;;
    esac
  done
  if "$check"; then
    [[ -f "$config" ]] || exit 1
    awk '/^tls:/ { found=1 } END { exit !found }' "$config" || exit 1
    [[ ! -f /run/fail-tls-check ]] || exit 1
    printf '0.0.0.0:39481\\n'
    exit 0
  fi
  config_dir="${config%/*}"
  mkdir -p "$config_dir/tls"
  printf 'tls:\\n  cert_file: %s/panel.crt\\n  key_file: %s/panel.key\\nlisten: 0.0.0.0:39481\\nsecure_cookie: true\\n' "$tls_dir" "$tls_dir" >> "$config"
  printf 'test cert\\n' > "$config_dir/tls/panel.crt"
  printf 'test key\\n' > "$config_dir/tls/panel.key"
  exit 0
fi
if [[ "$1" == check-screenshots ]]; then
  printf '%s\\n' "$*" >> /run/screenshot-check.log
  [[ -x /usr/bin/chromium && ! -f /run/fail-screenshot-check ]] || exit 1
  exit 0
fi
[[ "$1" == init ]] || exit 64
shift
while (($#)); do
  case "$1" in
    --dir) config_dir="$2"; shift 2 ;;
    --username) username="$2"; shift 2 ;;
    --password-stdin) shift ;;
    --listen) shift 2 ;;
    --tls-dir) tls_dir="$2"; shift 2 ;;
    --certificate-dir) certificate_dir="$2"; shift 2 ;;
    *) exit 64 ;;
  esac
done
IFS= read -r password
[[ ${#password} -ge 12 ]] || exit 65
mkdir -p "$config_dir"
printf 'username: %s\\npassword_hash: test-only-placeholder\\n' "$username" > "$config_dir/config.yaml"
printf 'services: []\\n' > "$config_dir/services.yaml"
"$0" https --config "$config_dir/config.yaml" --tls-dir "$tls_dir"
if [[ -n "$certificate_dir" ]]; then
  cp "$certificate_dir/fullchain.pem" "$config_dir/tls/panel.crt"
  cp "$certificate_dir/privkey.pem" "$config_dir/tls/panel.key"
fi
"""


class InstallerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if os.geteuid() != 0:
            raise unittest.SkipTest("Run with sudo to test inside isolated chroots")
        cls.template_dir = tempfile.TemporaryDirectory(prefix="gopanel-installer-base-")
        cls.template = Path(cls.template_dir.name)
        for command in (
            "bash", "uname", "tar", "gzip", "sha256sum", "awk", "install", "stat",
            "cp", "mv", "mkdir", "rm", "mktemp", "cat", "chmod", "chown", "id",
            "date", "sleep", "getent", "groupadd", "useradd", "userdel", "groupdel", "flock", "nologin",
            "touch", "visudo",
        ):
            copy_executable(cls.template, command)
        # The installer intentionally uses /bin/bash and /usr/bin/sudo.
        if not (cls.template / "bin/bash").exists():
            (cls.template / "bin").mkdir(exist_ok=True)
            shutil.copy2(shutil.which("bash"), cls.template / "bin/bash")
        for directory in ("run/systemd/system", "tmp", "dev", "etc/sudoers.d"):
            (cls.template / directory).mkdir(parents=True, exist_ok=True)
        write(cls.template, "/dev/null", "", 0o666)
        write(cls.template, "/etc/passwd", "root:x:0:0:root:/root:/bin/bash\n")
        write(cls.template, "/etc/group", "root:x:0:\n")
        write(cls.template, "/etc/shadow", "root:!:20000:0:99999:7:::\n", 0o600)
        write(cls.template, "/etc/gshadow", "root:!::\n", 0o600)
        write(cls.template, "/etc/nsswitch.conf", "passwd: files\ngroup: files\nshadow: files\n")
        write(cls.template, "/etc/sudoers", "root ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n", 0o440)
        write(cls.template, "/usr/bin/sudo", SUDO, 0o755)
        write(cls.template, "/usr/bin/systemctl", SYSTEMCTL, 0o755)
        write(cls.template, "/usr/bin/curl", """#!/bin/bash
while (($#)); do
  case "$1" in
    https://*) url="$1"; shift ;;
    -o) output="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf '%s\\n' "$url" >> /run/downloads.log
[[ ! -f /run/fail-download ]] || exit 22
cp "/tmp/${url##*/}" "$output"
""", 0o755)
        write(cls.template, "/etc/os-release", 'ID=debian\nVERSION_ID="12"\n')
        write(cls.template, "/usr/bin/apt-get", """#!/bin/bash
printf '%s\\n' "$*" >> /run/apt.log
[[ ! -f /run/fail-apt ]] || exit 1
if [[ "$1" == install ]]; then
  printf '#!/bin/bash\\nexit 0\\n' > /usr/bin/chromium
  chmod 0755 /usr/bin/chromium
fi
""", 0o755)
        write(cls.template, "/run/panel.pid", "4242\n")
        write(cls.template, "/proc/4242/status", "Uid:\t999\t999\t999\t999\nNoNewPrivs:\t0\nSeccomp:\t2\n")
        shutil.copy2(PROJECT / "install.sh", cls.template / "install.sh")

    @classmethod
    def tearDownClass(cls):
        if hasattr(cls, "template_dir"):
            cls.template_dir.cleanup()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="gopanel-installer-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "root"
        shutil.copytree(self.template, self.root)
        self.archive = self.root / "tmp" / ASSET
        self.package()

    def package(self, extra=None, omit=None, panel=PANEL):
        files = {
            "bin/lnmp-gopanel": panel,
            "bin/lnmp-gopanel-control": HELPER,
            "deploy/lnmp-gopanel.service": (PROJECT / "deploy/lnmp-gopanel.service").read_text(),
            "deploy/lnmp-gopanel.sudoers": (PROJECT / "deploy/lnmp-gopanel.sudoers").read_text(),
            "VERSION": "v0.1.0\n",
        }
        if omit:
            del files[omit]
        with tarfile.open(self.archive, "w:gz") as archive:
            for name, content in files.items():
                entry = tarfile.TarInfo("lnmp-gopanel/" + name)
                data = content.encode()
                entry.size = len(data)
                entry.mode = 0o755 if name.startswith("bin/") else 0o644
                archive.addfile(entry, io.BytesIO(data))
            if extra:
                archive.addfile(extra, io.BytesIO(b""))
        digest = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        self.archive.with_suffix(self.archive.suffix + ".sha256").write_text(f"{digest}  {ASSET}\n")

    def run_installer(self, *options, success=True, automatic_tls=True):
        if automatic_tls and "--tls-mode" not in options and "--certificate-dir" not in options:
            options = (*options, "--tls-mode", "self-signed")
        result = subprocess.run(
            ["chroot", str(self.root), "/bin/bash", "/install.sh", "--archive", f"/tmp/{ASSET}", *options],
            input="installer-test-password\n", text=True, capture_output=True, timeout=20,
        )
        if success:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def run_script(self, *options, input_text="", success=True, terminal=False):
        command = ["chroot", str(self.root), "/bin/bash", "/install.sh", *options]
        if terminal:
            master, slave = pty.openpty()
            try:
                process = subprocess.Popen(command, stdin=slave, stdout=subprocess.PIPE,
                                           stderr=subprocess.PIPE, text=True)
                os.close(slave)
                slave = None
                os.write(master, input_text.encode())
                stdout, stderr = process.communicate(timeout=20)
                result = subprocess.CompletedProcess(command, process.returncode, stdout, stderr)
            finally:
                os.close(master)
                if slave is not None:
                    os.close(slave)
        else:
            result = subprocess.run(command, input=input_text, text=True,
                                    capture_output=True, timeout=20)
        if success:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def test_management_menu_install_update_and_exit(self):
        result = self.run_script(input_text="9\n0\n", terminal=True)
        self.assertIn("从 GitHub Release 更新", result.stdout)
        self.assertFalse((self.root / "run/downloads.log").exists())
        self.run_script(input_text="1\n1\ninstaller-test-password\n", terminal=True)
        self.assertTrue((self.root / "etc/lnmp-gopanel/config.yaml").exists())
        self.run_script(input_text="2\n", terminal=True)
        self.assertIn("latest/download/", (self.root / "run/downloads.log").read_text())

    def test_update_downloads_release_and_preserves_data(self):
        previous = self.old_installation(https=True)
        state = write(self.root, "/var/lib/lnmp-gopanel/screenshot-settings.json", "saved state", 0o600)
        for options, suffix in (((), "latest/download"), (("--version", "v1.2.3"), "download/v1.2.3")):
            with self.subTest(options=options):
                self.run_script("--update", *options)
                for target in TARGETS[4:]:
                    self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])
                self.assertEqual(state.read_text(), "saved state")
                downloads = (self.root / "run/downloads.log").read_text().splitlines()
                base = f"https://github.com/azpanel/lnmp-gopanel/releases/{suffix}/{ASSET}"
                self.assertEqual(downloads[-2:], [base, base + ".sha256"])

    def test_update_requires_complete_installation(self):
        result = self.run_script("--update", success=False)
        self.assertIn("No complete panel installation", result.stderr)
        self.assertFalse((self.root / "run/downloads.log").exists())
        write(self.root, "/etc/lnmp-gopanel/config.yaml", "incomplete")
        self.run_script("--update", success=False)
        self.assertFalse((self.root / "run/downloads.log").exists())

    def test_update_failures_preserve_installation(self):
        previous = self.old_installation(https=True)
        for failure in ("download", "checksum", "restart"):
            with self.subTest(failure=failure):
                if failure == "checksum":
                    self.archive.with_suffix(self.archive.suffix + ".sha256").write_text(f"{'0' * 64}  {ASSET}\n")
                else:
                    write(self.root, f"/run/fail-{failure}", "")
                self.run_script("--update", success=False)
                for target, content in previous.items():
                    self.assertEqual((self.root / target.lstrip("/")).read_bytes(), content)
                if failure == "checksum":
                    self.package()
                else:
                    (self.root / f"run/fail-{failure}").unlink()

    def test_uninstall_cleans_panel_only_and_is_repeatable(self):
        self.old_installation(https=True)
        for path in ("/etc/systemd/system/lnmp-gopanel.service.d/override.conf",
                     "/var/lib/lnmp-gopanel/screenshots.key", "/var/backups/lnmp-gopanel/old/manifest"):
            write(self.root, path, "panel data")
        preserved = [write(self.root, path, "keep") for path in (
            "/home/wwwroot/site/index.html", "/var/lib/mysql/database", "/usr/local/nginx/conf/ssl/site/privkey.pem",
            "/usr/local/nginx/conf/vhost/site.conf", "/usr/local/nginx/conf/gopanel-sites/site.conf.json",
            "/usr/local/nginx/conf/ssl/gopanel/site/privkey.pem", "/usr/local/nginx/conf/gopanel-acme/.well-known/acme-challenge/token",
            "/root/.acme.sh/gopanel-certs/site_ecc/site.key", "/etc/cron.d/lnmp-gopanel-acme",
            "/usr/local/acme.sh/acme.sh", "/usr/local/acme.sh/account.conf", "/usr/local/acme.sh/gopanel-certs/site_ecc/site.key",
            "/etc/systemd/system/nginx.service", "/usr/bin/chromium", "/custom/screenshots/state")]
        # Neither sudo nor tar/curl is required for uninstall.
        for path in ("usr/bin/sudo", "usr/bin/curl"):
            (self.root / path).unlink()
        self.run_script("--uninstall", "--yes")
        for path in TARGETS + ("/etc/lnmp-gopanel", "/var/lib/lnmp-gopanel",
                               "/var/backups/lnmp-gopanel", "/etc/systemd/system/lnmp-gopanel.service.d"):
            self.assertFalse((self.root / path.lstrip("/")).exists(), path)
        for path in preserved:
            self.assertEqual(path.read_text(), "keep")
        self.assertNotIn("lnmp-panel:", (self.root / "etc/passwd").read_text())
        self.assertNotIn("lnmp-panel:", (self.root / "etc/group").read_text())
        self.assertFalse((self.root / "run/panel.active").exists())
        self.assertFalse((self.root / "run/panel.enabled").exists())
        self.run_script("--uninstall", "--yes")

    def test_uninstall_confirmation_and_cancel(self):
        previous = self.old_installation(https=True)
        self.run_script("--uninstall", success=False)
        result = self.run_script(input_text="3\nno\n", terminal=True)
        self.assertIn("已取消卸载", result.stdout)
        for path, content in previous.items():
            self.assertEqual((self.root / path.lstrip("/")).read_bytes(), content)
        self.run_script(input_text="3\nyes\n", terminal=True)
        self.assertFalse((self.root / "etc/lnmp-gopanel").exists())

    def test_uninstall_removes_leaf_symlinks_without_following_them(self):
        self.old_installation(https=True)
        preserved = write(self.root, "/external/state/keep", "keep")
        (self.root / "var/lib").mkdir(parents=True, exist_ok=True)
        (self.root / "var/lib/lnmp-gopanel").symlink_to("/external/state")
        (self.root / "etc/lnmp-gopanel/external").symlink_to("/external/state")
        self.run_script("--uninstall", "--yes")
        self.assertEqual(preserved.read_text(), "keep")
        self.assertFalse((self.root / "var/lib/lnmp-gopanel").is_symlink())

    def test_uninstall_rejects_symlink_parents_and_stop_failures(self):
        previous = self.old_installation(https=True)
        (self.root / "var").mkdir(exist_ok=True)
        (self.root / "var/lib").symlink_to("/external")
        result = self.run_script("--uninstall", "--yes", success=False)
        self.assertIn("Refusing a symlink parent", result.stderr)
        (self.root / "var/lib").unlink()
        write(self.root, "/run/fail-stop", "")
        result = self.run_script("--uninstall", "--yes", success=False)
        self.assertIn("Cannot stop panel", result.stderr)
        for path, content in previous.items():
            self.assertEqual((self.root / path.lstrip("/")).read_bytes(), content)

    def test_management_operations_share_a_lock(self):
        lock = self.root / "run/lnmp-gopanel-install.lock"
        with lock.open("w") as handle:
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
            for options in (("--install",), ("--update",), ("--uninstall", "--yes")):
                with self.subTest(options=options):
                    result = self.run_script(*options, success=False)
                    self.assertIn("Another panel installation, update or uninstall", result.stderr)
        self.assertFalse((self.root / "run/downloads.log").exists())
        self.assertFalse((self.root / "run/systemctl.log").exists())

    def test_uninstall_reports_account_cleanup_failure(self):
        self.old_installation(https=True)
        write(self.root, "/usr/sbin/userdel", "#!/bin/bash\nexit 1\n", 0o755)
        result = self.run_script("--uninstall", "--yes", success=False)
        self.assertIn("Cannot remove lnmp-panel account; uninstall is incomplete", result.stderr)
        self.assertNotIn("面板已完全卸载", result.stdout)

    def test_management_options_reject_conflicts(self):
        for options in (("--update", "--uninstall"), ("--uninstall", "--check"),
                        ("--uninstall", "--archive", "/tmp/test"), ("--yes",),
                        ("--update", "--archive", "/tmp/test")):
            with self.subTest(options=options):
                self.run_script(*options, success=False)
        self.assertFalse((self.root / "run/systemctl.log").exists())

    def old_installation(self, https=False):
        # Let real shadow utilities create the dedicated account inside chroot.
        subprocess.run(["chroot", str(self.root), "groupadd", "--system", "lnmp-panel"], check=True)
        subprocess.run(["chroot", str(self.root), "useradd", "--system", "--gid", "lnmp-panel", "--no-create-home", "lnmp-panel"], check=True)
        for target in TARGETS:
            if "sudoers" in target:
                content = "# previous permission configuration\n"
            else:
                content = "previous:" + target + "\n"
            write(self.root, target, content, 0o640 if target.endswith("config.yaml") else 0o644)
        if https:
            write(self.root, "/etc/lnmp-gopanel/config.yaml", "username: previous-admin\npassword_hash: preserved-hash\ntls:\n  cert_file: /etc/lnmp-gopanel/tls/panel.crt\n  key_file: /etc/lnmp-gopanel/tls/panel.key\nlisten: 0.0.0.0:39481\n", 0o640)
        write(self.root, "/run/panel.active", "")
        write(self.root, "/run/panel.enabled", "")
        return {target: (self.root / target.lstrip("/")).read_bytes() for target in TARGETS}

    def test_fresh_install(self):
        result = self.run_installer("--password-stdin", "--username", "operator")
        self.assertNotIn("Failed to get unit file state", result.stderr)
        self.assertNotIn("Docker not installed", result.stdout)
        config = self.root / "etc/lnmp-gopanel/config.yaml"
        self.assertIn("username: operator", config.read_text())
        self.assertIn("secure_cookie: true", config.read_text())
        self.assertIn("HTTPS listener: 0.0.0.0:39481", result.stdout)
        self.assertNotIn("http://", result.stdout)
        for name in ("panel.crt", "panel.key"):
            self.assertEqual((self.root / "etc/lnmp-gopanel/tls" / name).stat().st_mode & 0o777, 0o640)
        self.assertEqual(config.stat().st_mode & 0o777, 0o640)
        self.assertEqual((self.root / "etc/sudoers.d/lnmp-gopanel").stat().st_mode & 0o777, 0o440)
        self.assertIn(
            " /usr/local/libexec/lnmp-gopanel-control cron list, \\\n",
            (self.root / "etc/sudoers.d/lnmp-gopanel").read_text(),
        )
        for command in ("logs list", "logs read", "components snapshot") + VHOST_COMMANDS:
            self.assertIn(f"/usr/local/libexec/lnmp-gopanel-control {command}", (self.root / "etc/sudoers.d/lnmp-gopanel").read_text())
        self.assertTrue((self.root / "run/panel.active").exists())
        self.assertTrue((self.root / "run/panel.enabled").exists())
        self.assertTrue((self.root / "usr/local/bin/lnmp-gopanel").read_text().startswith("#!/bin/bash"))
        sudo_log = (self.root / "run/sudo.log").read_text()
        for command in ("nginx status", "docker snapshot", "components snapshot", "storage scan", "cron list", "logs list", "logs read") + VHOST_COMMANDS:
            self.assertIn(f"-n -u lnmp-panel -- /usr/bin/sudo -n -l -- /usr/local/libexec/lnmp-gopanel-control {command}\n", sudo_log)
        self.assertIn("-n -u lnmp-panel -- /usr/bin/sudo -n -- /usr/local/libexec/lnmp-gopanel-control docker snapshot\n", sudo_log)
        self.assertIn("-n -u lnmp-panel -- /usr/bin/sudo -n -- /usr/local/libexec/lnmp-gopanel-control components snapshot\n", sudo_log)
        self.assertNotIn('"name":"python3"', result.stdout)
        self.assertEqual((self.root / "run/helper.log").read_text(), "docker snapshot\ncomponents snapshot\n")
        self.assertIn("show lnmp-gopanel --property=MainPID --value\n", (self.root / "run/systemctl.log").read_text())

    def test_upgrade_preserves_credentials_and_no_start(self):
        previous = self.old_installation(https=True)
        self.run_installer("--no-start", automatic_tls=False)
        for target in TARGETS[4:]:
            self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])
        log = (self.root / "run/systemctl.log").read_text()
        self.assertNotIn("restart lnmp-gopanel", log)
        self.assertNotIn("enable lnmp-gopanel", log)
        self.assertNotIn("show lnmp-gopanel", log)
        self.assertEqual((self.root / "run/helper.log").read_text(), "docker snapshot\ncomponents snapshot\n")
        self.assertTrue(list((self.root / "var/backups/lnmp-gopanel").glob("*/manifest")))

    def test_https_upgrade_adds_log_permissions_without_reading_logs(self):
        previous = self.old_installation(https=True)
        self.run_installer(automatic_tls=False)
        for target in TARGETS[4:]:
            self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])
        sudoers = (self.root / "etc/sudoers.d/lnmp-gopanel").read_text()
        sudo_log = (self.root / "run/sudo.log").read_text()
        for command in ("logs list", "logs read", "components snapshot"):
            self.assertIn(f"/usr/local/libexec/lnmp-gopanel-control {command},", sudoers)
            self.assertIn(f"-n -l -- /usr/local/libexec/lnmp-gopanel-control {command}\n", sudo_log)
        # Log files are absent in this chroot. The installer must only check
        # sudo authorization, never invoke a log read or copy its output.
        self.assertEqual((self.root / "run/helper.log").read_text(), "docker snapshot\ncomponents snapshot\n")
        self.assertEqual((self.root / "usr/local/libexec/lnmp-gopanel-control").read_text(), HELPER)

    def test_vhost_upgrade_checks_all_permissions_without_touching_sites(self):
        previous = self.old_installation(https=True)
        preserved = [write(self.root, path, "keep") for path in (
            "/usr/local/nginx/conf/vhost/site.conf", "/home/wwwroot/site/index.html",
            "/root/.acme.sh/account.conf", "/etc/cron.d/lnmp-gopanel-acme",
            "/usr/local/acme.sh/acme.sh", "/usr/local/acme.sh/account.conf", "/usr/local/acme.sh/gopanel-certs/site_ecc/site.key",
            "/usr/local/nginx/conf/ssl/gopanel/site/fullchain.pem")]
        self.run_installer(automatic_tls=False)
        for target in TARGETS[4:]:
            self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])
        sudoers = (self.root / "etc/sudoers.d/lnmp-gopanel").read_text()
        sudo_log = (self.root / "run/sudo.log").read_text()
        for command in VHOST_COMMANDS:
            self.assertIn(f"/usr/local/libexec/lnmp-gopanel-control {command}", sudoers)
            self.assertIn(f"-n -l -- /usr/local/libexec/lnmp-gopanel-control {command}\n", sudo_log)
        self.assertEqual((self.root / "run/helper.log").read_text(), "docker snapshot\ncomponents snapshot\n")
        for path in preserved:
            self.assertEqual(path.read_text(), "keep")

    def test_http_upgrade_migrates_and_preserves_credentials_and_services(self):
        previous = self.old_installation()
        self.run_installer()
        config = (self.root / "etc/lnmp-gopanel/config.yaml").read_bytes()
        self.assertIn(previous[TARGETS[4]], config)
        self.assertIn(b"tls:", config)
        self.assertIn(b"listen: 0.0.0.0:39481", config)
        self.assertEqual((self.root / TARGETS[5].lstrip("/")).read_bytes(), previous[TARGETS[5]])

    def test_existing_certificate_selection(self):
        directory = "/usr/local/nginx/conf/ssl/example.com"
        write(self.root, directory + "/fullchain.pem", "existing cert\n")
        key = write(self.root, directory + "/privkey.pem", "existing key\n", 0o600)
        self.run_installer("--password-stdin", "--certificate-dir", directory)
        self.assertEqual((self.root / "etc/lnmp-gopanel/tls/panel.crt").read_text(), "existing cert\n")
        self.assertEqual((self.root / "etc/lnmp-gopanel/tls/panel.key").read_text(), "existing key\n")
        self.assertEqual(key.stat().st_mode & 0o777, 0o600)

    def test_interactive_certificate_menu(self):
        directory = "/usr/local/nginx/conf/ssl/example.com"
        write(self.root, directory + "/fullchain.pem", "existing cert\n")
        write(self.root, directory + "/privkey.pem", "existing key\n")
        master, slave = pty.openpty()
        try:
            process = subprocess.Popen(
                ["chroot", str(self.root), "/bin/bash", "/install.sh", "--archive", f"/tmp/{ASSET}", "--password-stdin"],
                stdin=slave, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
            )
            os.close(slave)
            slave = None
            os.write(master, b"2\n1\ninstaller-test-password\n")
            stdout, stderr = process.communicate(timeout=20)
            self.assertEqual(process.returncode, 0, stdout + stderr)
            self.assertIn("自动生成自签名证书", stdout)
            self.assertIn(directory, stdout)
            self.assertEqual((self.root / "etc/lnmp-gopanel/tls/panel.crt").read_text(), "existing cert\n")
        finally:
            os.close(master)
            if slave is not None:
                os.close(slave)

    def test_noninteractive_certificate_choice_is_required(self):
        result = self.run_installer("--password-stdin", success=False, automatic_tls=False)
        self.assertIn("Certificate selection needs a terminal", result.stderr)
        self.assertFalse((self.root / "usr/local/bin/lnmp-gopanel").exists())

    def test_no_usable_existing_certificate_fails_before_mutation(self):
        result = self.run_installer("--password-stdin", "--tls-mode", "existing", success=False)
        self.assertIn("No valid certificate/key pairs", result.stderr)
        self.assertFalse((self.root / "usr/local/bin/lnmp-gopanel").exists())

    def test_certificate_outside_scanned_directories_is_rejected(self):
        directory = "/usr/local/nginx/conf/ssl/example.com"
        write(self.root, directory + "/fullchain.pem", "existing cert\n")
        write(self.root, directory + "/privkey.pem", "existing key\n")
        result = self.run_installer("--password-stdin", "--certificate-dir", "/other/cert", success=False)
        self.assertIn("not a usable scanned directory", result.stderr)
        self.assertFalse((self.root / "usr/local/bin/lnmp-gopanel").exists())

    def test_tls_readability_failure_restores_previous_files(self):
        previous = self.old_installation()
        write(self.root, "/run/fail-tls-check", "")
        result = self.run_installer(success=False)
        self.assertIn("Panel account cannot read a valid HTTPS certificate", result.stderr)
        for target in TARGETS:
            self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])

    def test_missing_helper_permissions_rolls_back(self):
        previous = self.old_installation()
        for command in ("nginx status", "docker snapshot", "components snapshot", "storage scan", "cron list", "logs list", "logs read") + VHOST_COMMANDS:
            with self.subTest(command=command):
                write(self.root, "/run/deny-helper-command", f"/usr/local/libexec/lnmp-gopanel-control {command}\n")
                result = self.run_installer(success=False)
                self.assertIn(f"Panel account cannot use helper: {command}", result.stderr)
                self.assertIn("sudo: command not allowed", result.stderr)
                for target in TARGETS:
                    self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])
                self.assertFalse((self.root / "run/helper.log").exists())

    def test_helper_execution_failure_rolls_back(self):
        previous = self.old_installation()
        write(self.root, "/run/fail-helper", "")
        result = self.run_installer(success=False)
        self.assertIn("helper execution failed", result.stderr)
        self.assertIn("helper self-check failed", result.stderr)
        self.assertIn("Panel account cannot execute the helper", result.stderr)
        for target in TARGETS:
            self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])

    def test_components_helper_execution_failure_rolls_back(self):
        previous = self.old_installation(https=True)
        write(self.root, "/run/fail-components-helper", "")
        result = self.run_installer(success=False, automatic_tls=False)
        self.assertIn("components helper execution failed", result.stderr)
        self.assertIn("Panel account cannot execute the helper: components snapshot", result.stderr)
        for target in TARGETS:
            self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])
        self.assertTrue((self.root / "run/panel.active").exists())
        self.assertTrue((self.root / "run/panel.enabled").exists())

    def test_no_new_privileges_rolls_back_despite_successful_shell_helper(self):
        previous = self.old_installation()
        write(self.root, "/proc/4242/status", "NoNewPrivs:\t1\n")
        result = self.run_installer(success=False)
        self.assertIn("Panel process has NoNewPrivs=1", result.stderr)
        self.assertIn("implicit NoNewPrivileges", result.stderr)
        self.assertEqual((self.root / "run/helper.log").read_text(), "docker snapshot\ncomponents snapshot\n")
        for target in TARGETS:
            self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])
        self.assertTrue((self.root / "run/panel.active").exists())

    def test_unreadable_process_status_rolls_back(self):
        (self.root / "proc/4242/status").unlink()
        result = self.run_installer("--password-stdin", success=False)
        self.assertIn("Cannot read panel process status", result.stderr)
        self.assertFalse((self.root / "usr/local/bin/lnmp-gopanel").exists())
        self.assertFalse((self.root / "run/panel.active").exists())

    def test_invalid_main_pid_rolls_back(self):
        write(self.root, "/run/panel.pid", "0\n")
        result = self.run_installer("--password-stdin", success=False)
        self.assertIn("Cannot determine panel MainPID", result.stderr)
        self.assertFalse((self.root / "usr/local/bin/lnmp-gopanel").exists())

    def test_no_start_skips_running_process_check(self):
        write(self.root, "/proc/4242/status", "NoNewPrivs:\t1\n")
        result = self.run_installer("--password-stdin", "--no-start")
        self.assertIn("Helper permissions checked", result.stdout)
        self.assertNotIn("show lnmp-gopanel", (self.root / "run/systemctl.log").read_text())
        self.assertFalse((self.root / "run/panel.active").exists())

    def test_failed_restart_restores_previous_files(self):
        previous = self.old_installation()
        write(self.root, "/run/fail-restart", "")
        self.run_installer(success=False)
        for target in TARGETS:
            self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])

    def test_check_only_does_not_install(self):
        self.run_installer("--check")
        self.assertFalse((self.root / "usr/local/bin/lnmp-gopanel").exists())
        self.assertFalse((self.root / "etc/lnmp-gopanel").exists())
        self.assertFalse((self.root / "run/systemctl.log").exists())
        self.assertFalse((self.root / "run/sudo.log").exists())

    def test_legacy_package_is_rejected_before_prompts_or_changes(self):
        previous = self.old_installation(https=True)
        self.package(panel='''#!/bin/bash
if [[ "$1" == init && "$2" == --help ]]; then
  printf 'Usage of init:\\n  -dir string\\n  -listen string\\n' >&2
  exit 1
fi
printf 'run as a dedicated unprivileged user' >&2
exit 1
''')
        for options in ((), ("--check",)):
            with self.subTest(options=options):
                result = self.run_installer(*options, success=False, automatic_tls=False)
                self.assertIn("does not support HTTPS initialization", result.stderr)
                self.assertIn("--archive", result.stderr)
                self.assertNotIn("dedicated unprivileged user", result.stderr)
                self.assertNotIn("Certificate selection", result.stderr)
                self.assertNotIn("管理员密码", result.stderr)
                self.assertNotIn("Validated package", result.stdout)
                for target, content in previous.items():
                    self.assertEqual((self.root / target.lstrip("/")).read_bytes(), content)
                self.assertFalse((self.root / "run/systemctl.log").exists())
                self.assertFalse((self.root / "var/backups/lnmp-gopanel").exists())

    def test_screenshot_dependencies_and_smoke_check(self):
        self.run_installer("--password-stdin", "--with-screenshots")
        self.assertEqual((self.root / "run/apt.log").read_text(), "update\ninstall -y --no-install-recommends chromium chromium-sandbox fonts-noto-cjk ca-certificates\n")
        self.assertEqual((self.root / "run/screenshot-check.log").read_text(), "check-screenshots\n")
        self.assertEqual((self.root / "var/lib/lnmp-gopanel").stat().st_mode & 0o777, 0o700)

    def test_existing_screenshot_browser_is_reused(self):
        write(self.root, "/usr/bin/chromium", "#!/bin/bash\nexit 0\n", 0o755)
        self.run_installer("--password-stdin", "--with-screenshots")
        self.assertFalse((self.root / "run/apt.log").exists())
        self.assertTrue((self.root / "run/screenshot-check.log").exists())

    def test_screenshot_check_only_never_installs_dependencies(self):
        self.run_installer("--with-screenshots", "--check")
        self.assertFalse((self.root / "run/apt.log").exists())
        self.assertFalse((self.root / "var/lib/lnmp-gopanel").exists())

    def test_screenshot_unsupported_distribution_and_package_failure(self):
        write(self.root, "/etc/os-release", 'ID=ubuntu\nVERSION_ID="24.04"\n')
        result = self.run_installer("--password-stdin", "--with-screenshots", success=False)
        self.assertIn("supports Debian 12/13", result.stderr)
        self.assertFalse((self.root / "run/apt.log").exists())
        write(self.root, "/etc/os-release", 'ID=debian\nVERSION_ID="12"\n')
        write(self.root, "/run/fail-apt", "")
        self.run_installer("--password-stdin", "--with-screenshots", success=False)
        self.assertFalse((self.root / "usr/local/bin/lnmp-gopanel").exists())

    def test_screenshot_smoke_failure_rolls_back_and_preserves_state(self):
        previous = self.old_installation()
        write(self.root, "/usr/bin/chromium", "#!/bin/bash\nexit 0\n", 0o755)
        write(self.root, "/run/fail-screenshot-check", "")
        for name in ("screenshots.key", "screenshot-settings.json", "saved.jpg"):
            write(self.root, f"/var/lib/lnmp-gopanel/{name}", "existing screenshot state", 0o600)
        result = self.run_installer("--with-screenshots", success=False)
        self.assertIn("Screenshot component check failed", result.stderr)
        for target in TARGETS:
            self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])
        for name in ("screenshots.key", "screenshot-settings.json", "saved.jpg"):
            self.assertEqual((self.root / f"var/lib/lnmp-gopanel/{name}").read_text(), "existing screenshot state")

    def test_checksum_failure_leaves_installation_untouched(self):
        previous = self.old_installation()
        with self.archive.open("ab") as output:
            output.write(b"corrupt")
        self.run_installer(success=False)
        for target in TARGETS:
            self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])

    def test_rejects_traversal_and_links(self):
        for name, kind in (("lnmp-gopanel/../../outside", tarfile.REGTYPE), ("lnmp-gopanel/evil", tarfile.SYMTYPE)):
            with self.subTest(name=name):
                extra = tarfile.TarInfo(name)
                extra.type = kind
                extra.linkname = "/etc/passwd" if kind == tarfile.SYMTYPE else ""
                self.package(extra=extra)
                self.run_installer("--check", success=False)
                self.assertFalse((self.root / "outside").exists())

    def test_missing_payload_and_incomplete_existing_config(self):
        self.package(omit="bin/lnmp-gopanel-control")
        self.run_installer("--check", success=False)
        self.package()
        write(self.root, "/etc/lnmp-gopanel/config.yaml", "existing credentials\n")
        self.run_installer("--password-stdin", success=False)
        self.assertEqual((self.root / "etc/lnmp-gopanel/config.yaml").read_text(), "existing credentials\n")


if __name__ == "__main__":
    unittest.main(verbosity=2)
