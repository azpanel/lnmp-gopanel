#!/usr/bin/env python3
"""Exercise the installer in disposable chroots; never change the host install."""

import hashlib
import io
import os
from pathlib import Path
import platform
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
)


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
  is-enabled) [[ -f /run/panel.enabled ]] ;;
  enable) touch /run/panel.enabled ;;
  disable) rm -f /run/panel.enabled ;;
  restart) [[ ! -f /run/fail-restart ]] || exit 1; touch /run/panel.active ;;
  stop) rm -f /run/panel.active ;;
  daemon-reload) exit 0 ;;
  *) exit 1 ;;
esac
"""
PANEL = """#!/bin/bash
[[ "$1" == init ]] || exit 64
shift
while (($#)); do
  case "$1" in
    --dir) config_dir="$2"; shift 2 ;;
    --username) username="$2"; shift 2 ;;
    --password-stdin) shift ;;
    *) exit 64 ;;
  esac
done
IFS= read -r password
[[ ${#password} -ge 12 ]] || exit 65
mkdir -p "$config_dir"
printf 'username: %s\\npassword_hash: test-only-placeholder\\n' "$username" > "$config_dir/config.yaml"
printf 'services: []\\n' > "$config_dir/services.yaml"
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
            "date", "sleep", "getent", "groupadd", "useradd", "flock", "nologin",
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
        write(cls.template, "/usr/bin/sudo", "#!/bin/bash\nexit 0\n", 0o755)
        write(cls.template, "/usr/bin/systemctl", SYSTEMCTL, 0o755)
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

    def package(self, extra=None, omit=None):
        files = {
            "bin/lnmp-gopanel": PANEL,
            "bin/lnmp-gopanel-control": "#!/bin/bash\nexit 0\n",
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

    def run_installer(self, *options, success=True):
        result = subprocess.run(
            ["chroot", str(self.root), "/bin/bash", "/install.sh", "--archive", f"/tmp/{ASSET}", *options],
            input="installer-test-password\n", text=True, capture_output=True, timeout=20,
        )
        if success:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def old_installation(self):
        # Let real shadow utilities create the dedicated account inside chroot.
        subprocess.run(["chroot", str(self.root), "groupadd", "--system", "lnmp-panel"], check=True)
        subprocess.run(["chroot", str(self.root), "useradd", "--system", "--gid", "lnmp-panel", "--no-create-home", "lnmp-panel"], check=True)
        for target in TARGETS:
            if "sudoers" in target:
                content = "# previous permission configuration\n"
            else:
                content = "previous:" + target + "\n"
            write(self.root, target, content, 0o640 if target.endswith("config.yaml") else 0o644)
        write(self.root, "/run/panel.active", "")
        write(self.root, "/run/panel.enabled", "")
        return {target: (self.root / target.lstrip("/")).read_bytes() for target in TARGETS}

    def test_fresh_install(self):
        self.run_installer("--password-stdin", "--username", "operator")
        config = self.root / "etc/lnmp-gopanel/config.yaml"
        self.assertIn("username: operator", config.read_text())
        self.assertEqual(config.stat().st_mode & 0o777, 0o640)
        self.assertEqual((self.root / "etc/sudoers.d/lnmp-gopanel").stat().st_mode & 0o777, 0o440)
        self.assertTrue((self.root / "run/panel.active").exists())
        self.assertTrue((self.root / "run/panel.enabled").exists())
        self.assertTrue((self.root / "usr/local/bin/lnmp-gopanel").read_text().startswith("#!/bin/bash"))

    def test_upgrade_preserves_credentials_and_no_start(self):
        previous = self.old_installation()
        self.run_installer("--no-start")
        for target in TARGETS[-2:]:
            self.assertEqual((self.root / target.lstrip("/")).read_bytes(), previous[target])
        log = (self.root / "run/systemctl.log").read_text()
        self.assertNotIn("restart lnmp-gopanel", log)
        self.assertNotIn("enable lnmp-gopanel", log)
        self.assertTrue(list((self.root / "var/backups/lnmp-gopanel").glob("*/manifest")))

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
