#!/usr/bin/env bash
set -Eeuo pipefail

project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
target_arch="${1:-amd64}"
build_version="${2:-dev-$(git -C "$project_root" rev-parse --short HEAD 2>/dev/null || echo unknown)}"
build_time="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
output_dir="${3:-$project_root/dist}"
case "$target_arch" in amd64|arm64) ;; *) echo "Supported architectures: amd64, arm64" >&2; exit 1 ;; esac
[[ "$build_version" =~ ^[a-zA-Z0-9._-]+$ ]] || { echo "Invalid build version" >&2; exit 1; }
[[ "$build_time" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] || { echo "BUILD_TIME must be a UTC timestamp (YYYY-MM-DDTHH:MM:SSZ)" >&2; exit 1; }
[[ -f "$project_root/web/dist/index.html" ]] || { echo "Build the frontend first: cd web && npm ci && npm run build" >&2; exit 1; }
mkdir -p "$output_dir"
output_dir="$(cd -- "$output_dir" && pwd)"
stage_dir="$(mktemp -d)"
trap 'rm -rf -- "$stage_dir"' EXIT
payload="$stage_dir/lnmp-gopanel"
mkdir -p "$payload/bin"
cd "$project_root"
CGO_ENABLED=0 GOOS=linux GOARCH="$target_arch" go build -trimpath -tags embed \
  -ldflags="-s -w -X github.com/azpanel/lnmp-gopanel/internal/buildinfo.Version=$build_version -X github.com/azpanel/lnmp-gopanel/internal/buildinfo.BuiltAt=$build_time" \
  -o "$payload/bin/lnmp-gopanel" ./cmd/panel
CGO_ENABLED=0 GOOS=linux GOARCH="$target_arch" go build -trimpath -ldflags='-s -w' -o "$payload/bin/lnmp-gopanel-control" ./cmd/control
cp -R deploy "$payload/deploy"
cp install.sh README.md "$payload/"
printf '%s\n' "$build_version" > "$payload/VERSION"
chmod 0755 "$payload/install.sh" "$payload/bin/lnmp-gopanel" "$payload/bin/lnmp-gopanel-control"
archive="lnmp-gopanel_linux_${target_arch}.tar.gz"
tar --owner=0 --group=0 --numeric-owner -czf "$output_dir/$archive" -C "$stage_dir" lnmp-gopanel
cd "$output_dir"
sha256sum "$archive" > "$archive.sha256"
printf 'Created %s\n' "$output_dir/$archive"
