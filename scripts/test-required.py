#!/usr/bin/env python3
"""Run existing privileged fixtures and reject missing or skipped required tests."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

MODULE = "github.com/azpanel/lnmp-gopanel/"
REQUIRED = {
    "cmd/control": ["TestNginxManagementRejectsInvalidArguments"],
    "cmd/panel": ["TestMetricsInstallLockRejectsConcurrentOperationsAndReusesInheritedLock"],
    "internal/control": [
        "TestNginxManagementStatusVerifiesUnitOwnership",
        "TestNginxManagementRepairRestoresPIDAndPreservesOtherServices",
        "TestNginxManagementRepairRejectsStalePreview",
        "TestNginxManagementRepairRollsBackFailures",
        "TestNginxManagementRepairRejectsUnsafePID",
        "TestNginxManagementRollbackPreservesExistingPID",
        "TestNginxManagementRollbackPreservesConcurrentAdministratorChanges",
        "TestPersistentLogCursor",
        "TestInstallationLogTailAndResume",
        "TestInstallationStartTimePersistsAcrossUpdatesAndDuplicateRequests",
        "TestBasicAuthNginxIntegration",
    ],
    "internal/vhosts": [
        "TestGeneratedNginxConfiguration",
        "TestRenewedConfigurationAcceptedByNginx",
    ],
    "internal/metricssetup": ["TestGeneratedNginxWithLocalParser"],
}


def validate(events, required):
    passed, skipped, failed = set(), set(), set()
    for event in events:
        package, test = event.get("Package", ""), event.get("Test", "")
        key = (package, test.split("/")[0])
        if key not in required:
            continue
        action = event.get("Action")
        if action == "pass" and "/" not in test:
            passed.add(key)
        elif action == "skip":
            skipped.add(key)
        elif action == "fail":
            failed.add(key)
    return sorted((required - passed) | skipped | failed)


def main():
    if os.geteuid() != 0:
        raise SystemExit("Run with sudo: required fixtures check root ownership")
    nginx = shutil.which("nginx")
    if not nginx or not shutil.which("openssl"):
        raise SystemExit("Install nginx and openssl; required integrations must not skip")
    project = Path(__file__).resolve().parents[1]
    output = project / "test-results/privileged.jsonl"
    output.parent.mkdir(parents=True, exist_ok=True)
    required = {(MODULE + package, name) for package, names in REQUIRED.items() for name in names}
    env = dict(os.environ, NGINX_TEST_BINARY=nginx, GOPANEL_TEST_NGINX=nginx)
    # Run the full selected packages as root as well, including tests whose
    # chown/permission branches depend on UID without explicitly skipping.
    command = ["go", "test", "-race", "-json", "-count=1"]
    command.extend("./" + package for package in REQUIRED)
    events = []
    with output.open("w") as log:
        process = subprocess.Popen(command, cwd=project, env=env, stdout=subprocess.PIPE, text=True)
        for line in process.stdout:
            log.write(line)
            event = json.loads(line)
            events.append(event)
            if event.get("Action") == "output":
                print(event["Output"], end="", flush=True)
        status = process.wait()
    problems = validate(events, required)
    if status or problems:
        for package, test in problems:
            print(f"Required test missing, skipped or failed: {package}/{test}", file=sys.stderr)
        return 1
    print(f"All {len(required)} required tests executed and passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
