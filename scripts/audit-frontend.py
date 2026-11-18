#!/usr/bin/env python3
"""Run npm audit, with one explicitly approved exception for release v0.1.27."""

import json
import os
from pathlib import Path
import subprocess
import sys


ADVISORY = "https://github.com/advisories/GHSA-fc5q-39xc-vjpf"
APPROVED_RELEASE = "v0.1.27"
REVIEWED_PACKAGES = {"picomatch", "fdir", "tinyglobby", "vite", "vitest"}


def approved_exception(env, version):
    # Approved by the repository owner on 2026-11-18 for this release only.
    # Branch/PR checks, full releases and subsequent versions keep strict audit.
    return (env.get("GITHUB_EVENT_NAME") == "workflow_dispatch"
            and env.get("FRONTEND_AUDIT_MODE") == "fast"
            and env.get("FRONTEND_AUDIT_RELEASE") == APPROVED_RELEASE
            and version == "4.0.7")


def evaluate(report, approved):
    vulnerabilities = report.get("vulnerabilities")
    if report.get("error") or not isinstance(vulnerabilities, dict):
        raise ValueError("npm audit 未返回有效漏洞报告")
    if not vulnerabilities:
        return []
    if not approved:
        raise ValueError("依赖安全审计未通过；本次运行不适用已批准的临时豁免")

    def reviewed(name, ancestors=()):
        if name not in REVIEWED_PACKAGES or name in ancestors:
            return False
        item = vulnerabilities.get(name)
        if not isinstance(item, dict) or item.get("name") != name:
            return False
        via = item.get("via")
        if not isinstance(via, list) or not via:
            return False
        for source in via:
            if isinstance(source, str):
                if not reviewed(source, (*ancestors, name)):
                    return False
            elif isinstance(source, dict):
                if (name != "picomatch" or source.get("name") != "picomatch"
                        or source.get("url") != ADVISORY or source.get("range") != "<=4.0.7"):
                    return False
            else:
                return False
        return True

    blocked = sorted(name for name in vulnerabilities if not reviewed(name))
    if blocked:
        raise ValueError("发现未获豁免的漏洞，发布已阻止：" + ", ".join(blocked))
    return sorted(vulnerabilities)


def main():
    root = Path(__file__).resolve().parents[1]
    result = subprocess.run(["npm", "audit", "--json"], cwd=root / "web", capture_output=True, text=True)
    try:
        report = json.loads(result.stdout)
    except (ValueError, TypeError) as error:
        raise ValueError("npm audit 未能生成 JSON 报告，发布已阻止") from error
    if not isinstance(report, dict):
        raise ValueError("npm audit 报告格式异常，发布已阻止")
    destination = root / "test-results/frontend-audit.json"
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    if result.returncode not in (0, 1):
        raise ValueError("npm audit 执行失败，发布已阻止；请检查审计报告")
    if result.returncode == 1 and not report.get("vulnerabilities"):
        raise ValueError("npm audit 失败且没有有效漏洞条目，发布已阻止")
    manifest = root / "web/node_modules/picomatch/package.json"
    version = json.loads(manifest.read_text()).get("version") if manifest.is_file() else None
    allowed = evaluate(report, approved_exception(os.environ, version))
    if allowed:
        print("::warning title=已批准的单次发布豁免::仅 v0.1.27 快速发布豁免 GHSA-fc5q-39xc-vjpf；其余漏洞仍阻止发布。")
        print("受影响包：" + ", ".join(allowed))
        print("评估依据：生产面板使用 Go 与内嵌静态资源，不运行这条 Node.js 构建/测试依赖链。")
    else:
        print("依赖安全审计通过，无漏洞告警。")
    print("原始审计报告：" + str(destination))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
