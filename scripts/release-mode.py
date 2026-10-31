#!/usr/bin/env python3
"""Resolve manual release modes and publish new tags from the tested commit."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import urllib.error
import urllib.request

TAG = re.compile(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\Z")
ASSETS = ["lnmp-gopanel_linux_amd64.tar.gz", "lnmp-gopanel_linux_amd64.tar.gz.sha256",
          "lnmp-gopanel_linux_arm64.tar.gz", "lnmp-gopanel_linux_arm64.tar.gz.sha256",
          "SHA256SUMS", "install.sh"]


def github(path, method="GET", data=None):
    headers = {"Accept": "application/vnd.github+json", "Content-Type": "application/json",
               "User-Agent": "lnmp-gopanel-release", "X-GitHub-Api-Version": "2022-11-28",
               "Authorization": "Bearer " + os.environ["GH_TOKEN"]}
    request = urllib.request.Request("https://api.github.com/repos/" + os.environ["GH_REPO"] + path,
                                     headers=headers, method=method,
                                     data=None if data is None else json.dumps(data).encode())
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        if error.code == 404 and method == "GET":
            return None
        raise RuntimeError(f"GitHub 请求失败（HTTP {error.code}），发布已停止") from error


def prepare(env, request=github):
    mode = "full"
    version = "dev-" + env["GITHUB_SHA"][:12]
    publish, new_tag = False, False
    if env["GITHUB_EVENT_NAME"] == "workflow_dispatch":
        mode = env.get("RELEASE_MODE", "full") or "full"
        if mode not in ("full", "fast"):
            raise RuntimeError("发布模式仅支持 full 或 fast")
        tag = env.get("RELEASE_TAG_INPUT", "")
        if mode == "fast" and not tag:
            raise RuntimeError("快速发布需要填写尚未使用的 release_tag，例如 v0.1.20；请勿预先推送标签")
        if tag:
            if not TAG.fullmatch(tag):
                raise RuntimeError("release_tag 必须为 v主版本.中版本.补丁版本，例如 v0.1.20")
            if request("/git/ref/tags/" + tag) is not None or request("/releases/tags/" + tag) is not None:
                raise RuntimeError("该版本的标签或 Release 已存在，请使用新版本号")
            version, publish, new_tag = tag, True, True
    if not new_tag and env.get("GITHUB_REF_TYPE") == "tag" and env["GITHUB_REF_NAME"].startswith("v"):
        version, publish = env["GITHUB_REF_NAME"], True
    return {"mode": mode, "version": version, "publish": str(publish).lower(),
            "new_tag": str(new_tag).lower()}


def verify_assets(directory):
    directory = Path(directory)
    if any(not (directory / name).is_file() or (directory / name).stat().st_size == 0 for name in ASSETS):
        raise RuntimeError("发布附件缺失或为空")
    sums = {}
    for line in (directory / "SHA256SUMS").read_text().splitlines():
        parts = line.split()
        if len(parts) != 2 or parts[1] not in ASSETS or parts[1] in sums:
            raise RuntimeError("SHA256SUMS 格式无效")
        sums[parts[1]] = parts[0]
    packages = {name for name in ASSETS if name.endswith(".tar.gz")}
    if set(sums) != packages:
        raise RuntimeError("SHA256SUMS 未包含全部安装包")
    for name in packages:
        individual = (directory / (name + ".sha256")).read_text().split()
        if individual != [sums[name], name]:
            raise RuntimeError("独立校验文件与 SHA256SUMS 不一致：" + name)
        digest = hashlib.sha256()
        with (directory / name).open("rb") as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(chunk)
        if digest.hexdigest() != sums[name]:
            raise RuntimeError("安装包摘要不一致：" + name)


def publish(env, directory="dist", request=github, run=subprocess.run):
    verify_assets(directory)
    tag = env["RELEASE_TAG"]
    assets = [str(Path(directory) / name) for name in ASSETS]
    if env["NEW_TAG"] != "true":
        # Keep the existing pushed-tag release / retry behavior.
        if request("/releases/tags/" + tag) is not None:
            run(["gh", "release", "upload", tag, *assets, "--clobber"], check=True)
        else:
            run(["gh", "release", "create", tag, *assets, "--verify-tag", "--generate-notes", "--title", tag], check=True)
        return
    if not TAG.fullmatch(tag):
        raise RuntimeError("手动发布版本号无效")
    release = request("/releases/tags/" + tag)
    if release is not None and not release["draft"]:
        raise RuntimeError("该版本已正式发布，不覆盖已有正式附件")
    ref = request("/git/ref/tags/" + tag)
    if ref is None:
        # GITHUB_TOKEN-created refs do not trigger another push workflow.
        request("/git/refs", "POST", {"ref": "refs/tags/" + tag, "sha": env["RELEASE_SHA"]})
    elif ref["object"]["type"] != "commit" or ref["object"]["sha"] != env["RELEASE_SHA"]:
        raise RuntimeError("标签未指向本轮已验证提交，发布已停止")
    if release is None:
        command = ["gh", "release", "create", tag, *assets, "--verify-tag", "--draft", "--generate-notes", "--title", tag, "--target", env["RELEASE_SHA"]]
        if env["RELEASE_MODE"] == "fast":
            command += ["--notes", "本版本使用快速发布模式：通过基础测试、漏洞扫描、双架构打包和安装包冒烟验证；完整浏览器、systemd 及 Debian 12 回归未作为发布条件。"]
        run(command, check=True)
    else:
        run(["gh", "release", "upload", tag, *assets, "--clobber"], check=True)
    # Publish only after every asset has been uploaded; interrupted uploads stay draft.
    run(["gh", "release", "edit", tag, "--draft=false", "--latest"], check=True)


def main():
    if sys.argv[1:] == ["prepare"]:
        output = prepare(os.environ)
        with open(os.environ["GITHUB_OUTPUT"], "a") as stream:
            for key, value in output.items():
                stream.write(f"{key}={value}\n")
        print(f"模式：{output['mode']}；版本：{output['version']}；发布：{output['publish']}")
    elif sys.argv[1:] == ["publish"]:
        publish(os.environ)
    else:
        raise RuntimeError("用法：release-mode.py {prepare|publish}")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
