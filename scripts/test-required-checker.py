#!/usr/bin/env python3
"""Prove the CI gate cannot pass on an empty, skipped or partial test stream."""

import runpy
from pathlib import Path
import unittest

validate = runpy.run_path(str(Path(__file__).with_name("test-required.py")))["validate"]


class RequiredTestGate(unittest.TestCase):
    def test_requires_parent_pass_and_rejects_skipped_children(self):
        required = {("package", "TestRoot")}
        def event(action, test="TestRoot", package="package"):
            return {"Action": action, "Test": test, "Package": package}
        self.assertFalse(validate([event("pass")], required))
        for events in [[], [event("skip")], [event("pass", "TestRoot/child")],
                       [event("pass", package="other")],
                       [event("skip", "TestRoot/child"), event("pass")],
                       [event("fail", "TestRoot/child"), event("pass")]]:
            with self.subTest(events=events):
                self.assertEqual(validate(events, required), sorted(required))


if __name__ == "__main__":
    unittest.main()
