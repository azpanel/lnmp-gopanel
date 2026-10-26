#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
umask 077

repository="azpanel/lnmp-gopanel"
release_version="latest"
archive_path=""
checksum_path=""
admin_username="admin"
password_stdin=false
start_service=true
check_only=false
work_dir=""
backup_dir=""
mutation_started=false
installation_complete=false
was_active=false
was_enabled=false
targets=(
  /usr/local/bin/lnmp-gopanel
  /usr/local/libexec/lnmp-gopanel-control
  /etc/systemd/system/lnmp-gopanel.service
  /etc/sudoers.d/lnmp-gopanel
  /etc/lnmp-gopanel/config.yaml
  /etc/lnmp-gopanel/services.yaml
)

usage() {
  cat <<'EOF'
Usage: sudo bash install.sh [options]

  --version TAG          Install a release tag such as v0.1.0 (default: latest)
  --archive FILE         Install a local lnmp-gopanel_linux_ARCH.tar.gz
  --checksum-file FILE   Local checksum file (default: FILE.sha256)
  --username NAME        Administrator name for a new installation (default: admin)
  --password-stdin       Read a new administrator password from stdin
  --no-start             Install without enabling or restarting the panel service
  --check                Download and validate the package without installing
  -h, --help             Show this help

Requires Linux amd64/arm64, systemd and root. New installations prompt for a
12-72 byte password. Upgrades preserve existing credentials and services.yaml.
EOF
}
die() { printf 'Error: %s\n' "$*" >&2; exit 1; }
need_value() { [[ $# -ge 2 && -n "$2" ]] || die "$1 requires a value"; }
while (($#)); do
  case "$1" in
    --version) need_value "$@"; release_version="$2"; shift 2 ;;
    --archive) need_value "$@"; archive_path="$2"; shift 2 ;;
    --checksum-file) need_value "$@"; checksum_path="$2"; shift 2 ;;
    --username) need_value "$@"; admin_username="$2"; shift 2 ;;
    --password-stdin) password_stdin=true; shift ;;
    --no-start) start_service=false; shift ;;
    --check) check_only=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown argument: $1" ;;
  esac
done

[[ "$(uname -s)" == Linux ]] || die "Only Linux is supported"
case "$(uname -m)" in
  x86_64|amd64) target_arch=amd64 ;;
  aarch64|arm64) target_arch=arm64 ;;
  *) die "Only amd64 and arm64 are supported" ;;
esac
[[ "$release_version" == latest || "$release_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][a-zA-Z0-9.-]+)?$ ]] || die "Invalid release tag"
[[ -n "$admin_username" && ${#admin_username} -le 128 ]] || die "Invalid administrator name"
if ! "$check_only"; then
  [[ $EUID -eq 0 ]] || die "Run this installer as root"
  [[ -d /run/systemd/system ]] || die "A running systemd installation is required"
fi
for program in tar gzip sha256sum awk install stat cp mv mkdir rm mktemp; do
  command -v "$program" >/dev/null || die "Missing command: $program"
done

atomic_install() {
  local source="$1" destination="$2" mode="$3" group="${4:-root}"
  local staged="${destination}.new.$$"
  install -o root -g "$group" -m "$mode" "$source" "$staged"
  mv -fT -- "$staged" "$destination"
}
cleanup() {
  local status=$?
  set +e
  trap - EXIT INT TERM
  if "$mutation_started" && ! "$installation_complete"; then
    printf 'Installation failed; restoring files from %s\n' "$backup_dir" >&2
    local index destination
    for index in "${!targets[@]}"; do
      destination="${targets[$index]}"
      if [[ -f "$backup_dir/$index" ]]; then
        cp -p -- "$backup_dir/$index" "${destination}.restore.$$"
        mv -fT -- "${destination}.restore.$$" "$destination"
      else
        rm -f -- "$destination"
      fi
      rm -f -- "${destination}.new.$$"
    done
    systemctl daemon-reload || true
    if ! "$was_enabled"; then systemctl disable lnmp-gopanel >/dev/null 2>&1 || true; fi
    if "$was_active"; then
      systemctl restart lnmp-gopanel || true
    else
      systemctl stop lnmp-gopanel >/dev/null 2>&1 || true
    fi
  fi
  [[ -z "$work_dir" ]] || rm -rf -- "$work_dir"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
work_dir="$(mktemp -d /tmp/lnmp-gopanel-install.XXXXXXXX)"
asset="lnmp-gopanel_linux_${target_arch}.tar.gz"
if [[ -n "$archive_path" ]]; then
  [[ -f "$archive_path" && ! -L "$archive_path" ]] || die "Archive is not a regular file"
  checksum_path="${checksum_path:-$archive_path.sha256}"
  [[ -f "$checksum_path" ]] || die "Checksum file not found: $checksum_path"
  cp -- "$archive_path" "$work_dir/$asset"
  cp -- "$checksum_path" "$work_dir/checksum"
else
  [[ -z "$checksum_path" ]] || die "--checksum-file requires --archive"
  command -v curl >/dev/null || die "Missing command: curl"
  release_url="https://github.com/$repository/releases"
  if [[ "$release_version" == latest ]]; then
    release_url+="/latest/download"
  else
    release_url+="/download/$release_version"
  fi
  printf 'Downloading %s (%s)...\n' "$release_version" "$target_arch"
  curl --proto '=https' --tlsv1.2 -fsSL --retry 3 --connect-timeout 15 --max-time 300 "$release_url/$asset" -o "$work_dir/$asset"
  curl --proto '=https' --tlsv1.2 -fsSL --retry 3 --connect-timeout 15 --max-time 60 "$release_url/$asset.sha256" -o "$work_dir/checksum"
fi
checksum="$(awk -v file="$asset" '{ name=$2; sub(/^\*/, "", name); if (name==file) print $1 }' "$work_dir/checksum")"
[[ "$checksum" =~ ^[a-fA-F0-9]{64}$ ]] || die "Missing or ambiguous SHA-256 checksum for $asset"
(cd "$work_dir" && printf '%s  %s\n' "$checksum" "$asset" | sha256sum --check --status) || die "SHA-256 verification failed"

# Accept only the expected package root, regular files and directories. Reject
# links, traversal and special files before extracting a release as root.
tar -tzf "$work_dir/$asset" > "$work_dir/members"
while IFS= read -r member; do
  [[ "$member" == lnmp-gopanel/ || "$member" == lnmp-gopanel/* ]] || die "Unexpected archive path"
  [[ ! "$member" =~ (^|/)\.\.(/|$) ]] || die "Unsafe archive path"
done < "$work_dir/members"
tar -tvzf "$work_dir/$asset" | awk 'substr($0,1,1)!="-" && substr($0,1,1)!="d" { bad=1 } END { exit bad }' || die "Archive contains links or special files"
tar --no-same-owner --no-same-permissions -xzf "$work_dir/$asset" -C "$work_dir"
payload="$work_dir/lnmp-gopanel"
for required in bin/lnmp-gopanel bin/lnmp-gopanel-control deploy/lnmp-gopanel.service deploy/lnmp-gopanel.sudoers VERSION; do
  [[ -f "$payload/$required" ]] || die "Package is missing $required"
done
printf 'Validated package: %s (%s)\n' "$(cat "$payload/VERSION")" "$target_arch"
if "$check_only"; then installation_complete=true; exit 0; fi

for program in systemctl visudo getent groupadd useradd flock; do
  command -v "$program" >/dev/null || die "Missing command: $program"
done
[[ -x /usr/bin/sudo ]] || die "Install sudo first (the panel requires /usr/bin/sudo)"
exec 9>/run/lnmp-gopanel-install.lock
flock -n 9 || die "Another installation is running"
for destination in "${targets[@]}" /etc/lnmp-gopanel; do
  [[ ! -L "$destination" ]] || die "Refusing a symlink destination: $destination"
done
fresh_install=false
if [[ -f /etc/lnmp-gopanel/config.yaml && -f /etc/lnmp-gopanel/services.yaml ]]; then
  printf 'Upgrading; preserving existing credentials and service configuration.\n'
elif [[ ! -e /etc/lnmp-gopanel/config.yaml && ! -e /etc/lnmp-gopanel/services.yaml ]]; then
  fresh_install=true
else
  die "Incomplete configuration: expected both config.yaml and services.yaml"
fi
visudo -cf "$payload/deploy/lnmp-gopanel.sudoers"
if "$fresh_install"; then
  chmod 0755 "$payload/bin/lnmp-gopanel"
  init_args=(init --dir "$work_dir/config" --username "$admin_username")
  if "$password_stdin"; then
    init_args+=(--password-stdin)
  elif [[ ! -t 0 ]]; then
    die "A new installation needs a terminal or --password-stdin"
  fi
  "$payload/bin/lnmp-gopanel" "${init_args[@]}"
fi
if ! getent group lnmp-panel >/dev/null; then groupadd --system lnmp-panel; fi
if ! getent passwd lnmp-panel >/dev/null; then
  nologin_shell="$(command -v nologin)"
  useradd --system --gid lnmp-panel --no-create-home --home-dir /nonexistent --shell "$nologin_shell" lnmp-panel
fi
[[ "$(id -u lnmp-panel)" != 0 ]] || die "The panel account must not be root"
if systemctl is-active --quiet lnmp-gopanel >/dev/null 2>&1; then was_active=true; fi
if systemctl is-enabled --quiet lnmp-gopanel >/dev/null 2>&1; then was_enabled=true; fi
backup_dir="/var/backups/lnmp-gopanel/$(date -u +%Y%m%dT%H%M%SZ)-$$"
install -d -o root -g root -m 0700 /var/backups/lnmp-gopanel "$backup_dir"
for index in "${!targets[@]}"; do
  destination="${targets[$index]}"
  if [[ -f "$destination" ]]; then cp -p -- "$destination" "$backup_dir/$index"; fi
  printf '%s\t%s\n' "$index" "$destination" >> "$backup_dir/manifest"
done
mutation_started=true
install -d -o root -g root -m 0755 /usr/local/bin /usr/local/libexec /etc/systemd/system /etc/sudoers.d
install -d -o root -g lnmp-panel -m 0750 /etc/lnmp-gopanel
atomic_install "$payload/bin/lnmp-gopanel" /usr/local/bin/lnmp-gopanel 0755
atomic_install "$payload/bin/lnmp-gopanel-control" /usr/local/libexec/lnmp-gopanel-control 0755
atomic_install "$payload/deploy/lnmp-gopanel.service" /etc/systemd/system/lnmp-gopanel.service 0644
atomic_install "$payload/deploy/lnmp-gopanel.sudoers" /etc/sudoers.d/lnmp-gopanel 0440
if "$fresh_install"; then
  atomic_install "$work_dir/config/config.yaml" /etc/lnmp-gopanel/config.yaml 0640 lnmp-panel
  atomic_install "$work_dir/config/services.yaml" /etc/lnmp-gopanel/services.yaml 0644
fi
chown root:lnmp-panel /etc/lnmp-gopanel/config.yaml
chmod 0640 /etc/lnmp-gopanel/config.yaml
chown root:root /etc/lnmp-gopanel/services.yaml
chmod 0644 /etc/lnmp-gopanel/services.yaml
visudo -cf /etc/sudoers
systemctl daemon-reload
if "$start_service"; then
  systemctl enable lnmp-gopanel
  systemctl restart lnmp-gopanel
  sleep 1
  systemctl is-active --quiet lnmp-gopanel || die "Panel did not start; check journalctl -u lnmp-gopanel"
else
  printf 'Service was not enabled or restarted (--no-start).\n'
fi
installation_complete=true
printf 'Installation complete. Backups: %s\n' "$backup_dir"
printf 'Configuration: /etc/lnmp-gopanel/config.yaml\n'
printf 'Check /etc/lnmp-gopanel/services.yaml and use systemctl status lnmp-gopanel.\n'
if "$fresh_install"; then
  printf 'Default address: http://127.0.0.1:8090 (local access only).\n'
  printf 'For direct public access, set listen: 0.0.0.0:8090 in the configuration, restart lnmp-gopanel, and allow TCP 8090 in your cloud security group and host firewall.\n'
  printf 'For HTTPS access, see the reverse proxy example in the README.\n'
fi
