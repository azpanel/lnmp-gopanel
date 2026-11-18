#!/usr/bin/env python3
"""Verify that the single-release audit exception cannot hide other findings."""

import copy
from pathlib import Path
import runpy
import unittest


module = runpy.run_path(str(Path(__file__).with_name("audit-frontend.py")))
evaluate = module["evaluate"]
approved_exception = module["approved_exception"]


def finding(name, via):
    return {"name": name, "via": via}


def report():
    return {"vulnerabilities": {
        "picomatch": finding("picomatch", [{"name": "picomatch", "url": module["ADVISORY"], "range": "<=4.0.7"}]),
        "fdir": finding("fdir", ["picomatch"]),
        "tinyglobby": finding("tinyglobby", ["fdir", "picomatch"]),
        "vite": finding("vite", ["fdir", "picomatch", "tinyglobby"]),
        "vitest": finding("vitest", ["picomatch", "tinyglobby", "vite"]),
    }}


class FrontendAuditTests(unittest.TestCase):
    def test_clean_audit_does_not_require_an_exception(self):
        self.assertEqual(evaluate({"vulnerabilities": {}}, False), [])

    def test_only_the_reviewed_advisory_and_its_dependency_chain_are_allowed(self):
        self.assertEqual(evaluate(report(), True), sorted(module["REVIEWED_PACKAGES"]))
        with self.assertRaises(ValueError):
            evaluate(report(), False)

    def test_new_advisories_unknown_packages_and_changed_ranges_block_release(self):
        original = report()
        for change in ("advisory", "package", "range", "root_name"):
            with self.subTest(change=change), self.assertRaises(ValueError):
                value = copy.deepcopy(original)
                root = value["vulnerabilities"]["picomatch"]["via"][0]
                if change == "advisory":
                    value["vulnerabilities"]["vite"]["via"].append({"name": "vite", "url": "https://github.com/advisories/GHSA-other"})
                elif change == "package":
                    value["vulnerabilities"]["unknown"] = finding("unknown", ["picomatch"])
                elif change == "range":
                    root["range"] = "<=5.0.0"
                else:
                    root["name"] = "another-package"
                evaluate(value, True)

    def test_invalid_reports_missing_findings_and_cycles_fail_closed(self):
        for value in ({}, {"error": {"code": "offline"}, "vulnerabilities": {}},
                      {"vulnerabilities": []}, {"vulnerabilities": {"vite": finding("vite", [])}},
                      {"vulnerabilities": {"vite": finding("vite", ["missing"])}},
                      {"vulnerabilities": {"vite": finding("vite", ["vitest"]), "vitest": finding("vitest", ["vite"])}}):
            with self.subTest(value=value), self.assertRaises(ValueError):
                evaluate(value, True)

    def test_exception_is_limited_to_the_approved_release_event_mode_and_version(self):
        env = {"GITHUB_EVENT_NAME": "workflow_dispatch", "FRONTEND_AUDIT_MODE": "fast", "FRONTEND_AUDIT_RELEASE": "v0.1.27"}
        self.assertTrue(approved_exception(env, "4.0.7"))
        for key, value in (("GITHUB_EVENT_NAME", "push"), ("GITHUB_EVENT_NAME", "pull_request"),
                           ("FRONTEND_AUDIT_MODE", "full"), ("FRONTEND_AUDIT_RELEASE", "v0.1.28")):
            with self.subTest(key=key, value=value):
                self.assertFalse(approved_exception({**env, key: value}, "4.0.7"))
        self.assertFalse(approved_exception({}, "4.0.7"))
        self.assertFalse(approved_exception(env, "4.0.8"))


if __name__ == "__main__":
    unittest.main()
