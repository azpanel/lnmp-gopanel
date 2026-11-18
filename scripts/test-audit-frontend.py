#!/usr/bin/env python3
"""Verify strict audit enforcement and report preservation."""

import contextlib
import io
import json
from pathlib import Path
import runpy
import subprocess
import tempfile
import unittest
from unittest import mock


module = runpy.run_path(str(Path(__file__).with_name("audit-frontend.py")))
evaluate = module["evaluate"]


def finding(name, via):
    return {"name": name, "via": via}


def report():
    return {"vulnerabilities": {
        "picomatch": finding("picomatch", [{"name": "picomatch", "url": "https://github.com/advisories/GHSA-fc5q-39xc-vjpf", "range": "<=4.0.7"}]),
        "fdir": finding("fdir", ["picomatch"]),
        "tinyglobby": finding("tinyglobby", ["fdir", "picomatch"]),
        "vite": finding("vite", ["fdir", "picomatch", "tinyglobby"]),
        "vitest": finding("vitest", ["picomatch", "tinyglobby", "vite"]),
    }}


class FrontendAuditTests(unittest.TestCase):
    def test_clean_audit_passes(self):
        self.assertEqual(evaluate({"vulnerabilities": {}}), [])

    def test_previously_exempt_findings_and_new_vulnerabilities_block_release(self):
        for value in (report(), {"vulnerabilities": {"unknown": finding("unknown", [])}}):
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, "发布已阻止"):
                evaluate(value)

    def test_invalid_reports_fail_closed(self):
        for value in ({}, {"error": {"code": "offline"}, "vulnerabilities": {}},
                      {"vulnerabilities": []}, {"vulnerabilities": {"vite": finding("vite", [])}},
                      {"vulnerabilities": {"vite": finding("vite", ["missing"])}},
                      {"vulnerabilities": {"vite": finding("vite", ["vitest"]), "vitest": finding("vitest", ["vite"])}}):
            with self.subTest(value=value), self.assertRaises(ValueError):
                evaluate(value)

    def test_old_release_environment_cannot_bypass_audit_and_report_is_preserved(self):
        env = {"GITHUB_EVENT_NAME": "workflow_dispatch", "FRONTEND_AUDIT_MODE": "fast", "FRONTEND_AUDIT_RELEASE": "v0.1.27"}
        value = report()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = root / "web/node_modules/picomatch/package.json"
            manifest.parent.mkdir(parents=True)
            manifest.write_text('{"version": "4.0.7"}')
            result = subprocess.CompletedProcess([], 1, json.dumps(value))
            with mock.patch.dict("os.environ", env), \
                    mock.patch.dict(module["main"].__globals__, {"__file__": str(root / "scripts/audit-frontend.py")}), \
                    mock.patch.object(module["subprocess"], "run", return_value=result), \
                    self.assertRaisesRegex(ValueError, "发布已阻止"):
                module["main"]()
            self.assertEqual(json.loads((root / "test-results/frontend-audit.json").read_text()), value)

    def test_command_failures_and_invalid_output_block_release(self):
        clean = json.dumps({"vulnerabilities": {}})
        for code, output in ((2, clean), (1, clean), (0, "not json"), (0, "[]")):
            with self.subTest(code=code, output=output), tempfile.TemporaryDirectory() as directory:
                result = subprocess.CompletedProcess([], code, output)
                with mock.patch.dict(module["main"].__globals__, {"__file__": str(Path(directory) / "scripts/audit-frontend.py")}), \
                        mock.patch.object(module["subprocess"], "run", return_value=result), \
                        self.assertRaisesRegex(ValueError, "发布已阻止"):
                    module["main"]()

    def test_clean_command_preserves_report_without_installed_picomatch(self):
        value = {"vulnerabilities": {}}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            result = subprocess.CompletedProcess([], 0, json.dumps(value))
            with mock.patch.dict(module["main"].__globals__, {"__file__": str(root / "scripts/audit-frontend.py")}), \
                    mock.patch.object(module["subprocess"], "run", return_value=result) as run, \
                    contextlib.redirect_stdout(io.StringIO()):
                module["main"]()
            run.assert_called_once_with(["npm", "audit", "--json"], cwd=root / "web", capture_output=True, text=True)
            self.assertEqual(json.loads((root / "test-results/frontend-audit.json").read_text()), value)


if __name__ == "__main__":
    unittest.main()
