#!/usr/bin/env python3
"""Smoke-test a native release package outside the source tree, without installing."""

import gzip
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import re
import ssl
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request


def main():
    archive = Path(sys.argv[1]).resolve()
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    assert archive.with_name(archive.name + ".sha256").read_text().split()[0] == digest
    with tempfile.TemporaryDirectory(prefix="gopanel-package-") as work:
        root = Path(work)
        with tarfile.open(archive) as package:
            package.extractall(root, filter="data")
        payload = root / "lnmp-gopanel"
        binary = payload / "bin/lnmp-gopanel"
        config_dir = root / "config"
        password = "Package-Only-Panel-Password-2026"
        subprocess.run([str(binary), "init", "--dir", str(config_dir), "--listen", "127.0.0.1:0", "--password-stdin"],
                       cwd=root, input=password + "\n", text=True, check=True, timeout=30)
        config = config_dir / "config.yaml"
        content = config.read_text()
        listen = re.search(r"^listen: (.+)$", content, re.M).group(1)
        # These package checks never invoke a host helper or managed host service.
        assert len(re.findall(r"^helper:.*$", content, re.M)) == 1, "Cannot safely disable the host helper"
        config.write_text(re.sub(r"^helper:.*$", "helper: /nonexistent/package-test-helper", content, flags=re.M))
        (config_dir / "services.yaml").write_text("services: []\n")
        command = [str(binary), "--config", str(config), "--services", str(config_dir / "services.yaml")]
        if os.geteuid() == 0:
            command.append("--allow-root-for-development")
        with (root / "panel.log").open("w+") as log:
            child = subprocess.Popen(command, cwd=root, stdout=log, stderr=log)
            try:
                context = ssl.create_default_context(cafile=str(config_dir / "tls/panel.crt"))
                client = urllib.request.build_opener(urllib.request.HTTPSHandler(context=context),
                    urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
                base = "https://" + listen
                def get(path, headers=None, method=None, data=None):
                    return client.open(urllib.request.Request(base + path, headers=headers or {}, method=method, data=data), timeout=10)
                deadline = time.monotonic() + 20
                while True:
                    try:
                        with get("/") as response:
                            html = response.read().decode()
                            assert response.headers["Cache-Control"] == "no-store"
                        break
                    except urllib.error.URLError:
                        if child.poll() is not None or time.monotonic() >= deadline:
                            raise
                        time.sleep(0.1)
                assets = re.findall(r'(?:src|href)="(/assets/[^\"]+)"', html)
                assert assets, "No embedded asset references"
                for path in assets:
                    with get(path, {"Accept-Encoding": "identity"}) as response:
                        plain = response.read()
                        assert plain and "immutable" in response.headers["Cache-Control"]
                    with get(path, {"Accept-Encoding": "gzip"}) as response:
                        compressed = response.read()
                        if len(plain) >= 1024 and len(gzip.compress(plain, compresslevel=9)) < len(plain):
                            assert response.headers["Content-Encoding"] == "gzip"
                            assert gzip.decompress(compressed) == plain and len(compressed) < len(plain)
                        else:
                            assert not response.headers.get("Content-Encoding") and compressed == plain
                    with get(path, {"Accept-Encoding": "gzip"}, "HEAD") as response:
                        assert not response.read() and int(response.headers["Content-Length"]) == len(compressed)
                    with get(path, {"Accept-Encoding": "gzip;q=0, *;q=1"}) as response:
                        assert not response.headers.get("Content-Encoding") and response.read() == plain
                    with get(path, {"Accept-Encoding": "gzip", "Range": "bytes=0-15"}) as response:
                        assert response.status == 206 and response.read() == plain[:16]
                        assert not response.headers.get("Content-Encoding")
                with get("/vhosts") as response:
                    assert response.read().decode() == html
                login = json.dumps({"username": "admin", "password": password}).encode()
                with get("/api/v1/auth/login", {"Content-Type": "application/json", "Origin": base}, "POST", login) as response:
                    assert response.status == 200 and "Secure" in response.headers["Set-Cookie"]
                with get("/api/v1/build") as response:
                    assert json.load(response)["version"] == (payload / "VERSION").read_text().strip()
                print("Native package HTTPS, embedded assets, compression, ranges and version passed")
            except BaseException:
                log.flush()
                print((root / "panel.log").read_text(), file=sys.stderr)
                raise
            finally:
                child.terminate()
                try:
                    child.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait()


if __name__ == "__main__":
    main()
