#!/usr/bin/env python3
"""Run strict npm audit and preserve the original report."""

import json
from pathlib import Path
import subprocess
import sys


def evaluate(report):
    vulnerabilities = report.get("vulnerabilities")
    if report.get("error") or not isinstance(vulnerabilities, dict):
        raise ValueError("npm audit 未返回有效漏洞报告")
    if not vulnerabilities:
        return []
    raise ValueError("依赖安全审计未通过，发布已阻止：" + ", ".join(sorted(vulnerabilities)))


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
    evaluate(report)
    print("依赖安全审计通过，无漏洞告警。")
    print("原始审计报告：" + str(destination))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
