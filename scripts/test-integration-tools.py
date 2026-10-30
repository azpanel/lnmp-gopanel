#!/usr/bin/env python3
"""Check installation-test safety and real-package fault injection without installing."""

import hashlib
import os
from pathlib import Path
import runpy
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

SCRIPT = Path(__file__).with_name("test-systemd.py").resolve()
module = runpy.run_path(str(SCRIPT))
mutated_archive = module["mutated_archive"]
guard = module["guard"]


class IntegrationTools(unittest.TestCase):
    def test_unmarked_machine_is_rejected_before_archive_access(self):
        env = {key: value for key, value in os.environ.items()
               if key not in ("GITHUB_ACTIONS", "RUNNER_ENVIRONMENT")}
        result = subprocess.run([sys.executable, str(SCRIPT), "--archive", "/nonexistent/test.tar.gz"],
                                env=env, text=True, capture_output=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("require a disposable", result.stderr)
        self.assertNotIn("Traceback", result.stderr)

    def test_even_a_hosted_runner_cannot_overwrite_an_existing_install(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            old = root / "existing-panel"
            old.write_bytes(b"untouched")
            globals_override = {"TARGETS": [old], "STATE": root / "state", "CONFIG": root / "etc/config.yaml",
                                "UNIT": root / "unit.service", "BACKUPS": root / "backups"}
            with mock.patch.dict(guard.__globals__, globals_override), \
                 mock.patch.dict(os.environ, {"GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "github-hosted"}), \
                 mock.patch.object(os, "geteuid", return_value=0), \
                 mock.patch.object(Path, "read_text", return_value="systemd"):
                with self.assertRaisesRegex(SystemExit, "Refusing to overwrite an existing installation"):
                    guard()
            self.assertEqual(old.read_bytes(), b"untouched")

    def test_fault_packages_change_real_unit_or_policy_and_have_matching_checksums(self):
        project = SCRIPT.parents[1]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            payload = root / "lnmp-gopanel/deploy"
            payload.mkdir(parents=True)
            for name in ("lnmp-gopanel.service", "lnmp-gopanel.sudoers"):
                (payload / name).write_bytes((project / "deploy" / name).read_bytes())
            original = root / "package.tar.gz"
            with tarfile.open(original, "w:gz") as package:
                package.add(root / "lnmp-gopanel", arcname="lnmp-gopanel")
            baseline = original.read_bytes()
            for fault in ("sudo", "certificate", "privileges"):
                destination = root / fault
                destination.mkdir()
                archive = mutated_archive(original, destination, fault)
                self.assertEqual(original.read_bytes(), baseline)
                self.assertEqual(archive.with_name(archive.name + ".sha256").read_text().split()[0],
                                 hashlib.sha256(archive.read_bytes()).hexdigest())
                with tarfile.open(archive) as package:
                    unit = package.extractfile("lnmp-gopanel/deploy/lnmp-gopanel.service").read().decode()
                    policy = package.extractfile("lnmp-gopanel/deploy/lnmp-gopanel.sudoers").read().decode()
                if fault == "sudo":
                    self.assertNotIn("components snapshot", policy)
                    self.assertIn("components denied", policy)
                elif fault == "certificate":
                    self.assertIn("ExecStartPre=+/usr/bin/chmod 0600", unit)
                else:
                    self.assertIn("NoNewPrivileges=true", unit)
                    self.assertNotIn("NoNewPrivileges=false", unit)


if __name__ == "__main__":
    unittest.main()
