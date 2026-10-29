#!/usr/bin/env bash
set -Eeuo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
umask 077

repository="azpanel/lnmp-gopanel"
action=""
auto_confirm=false
install_options=false
reset_incompatible=false
show_menu=false
if (($# == 0)); then show_menu=true; fi
release_version="latest"
archive_path=""
checksum_path=""
admin_username="admin"
password_stdin=false
start_service=true
check_only=false
with_screenshots=false
tls_mode=""
certificate_dir=""
tls_changed=false
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
  /etc/lnmp-gopanel/tls/panel.crt
  /etc/lnmp-gopanel/tls/panel.key
)

usage() {
  cat <<'EOF'
Usage: sudo bash install.sh [options]

  --install              Install the panel (existing installations are upgraded)
  --update               Update an installed panel from GitHub Release
  --reset-password       Reset administrator password and clear TOTP/recovery codes
  --configure-metrics    Interactively configure local service monitoring
  --uninstall            Completely remove the panel, configuration, data and backups
  --yes                  Confirm --uninstall without prompting
  --version TAG          Install a release tag such as v0.1.0 (default: latest)
  --archive FILE         Install a local lnmp-gopanel_linux_ARCH.tar.gz
  --checksum-file FILE   Local checksum file (default: FILE.sha256)
  --username NAME        Administrator name for a new installation (default: admin)
  --password-stdin       Read a new administrator password from stdin
  --no-start             Install without enabling or restarting the panel service
  --check                Download and validate the package without installing
  --with-screenshots     Install/check Chromium (Debian 12/13, Ubuntu 22.04/24.04/26.04)
  --tls-mode MODE        self-signed or existing (otherwise prompt when TLS is needed)
  --certificate-dir DIR  Use a certificate directory under /usr/local/nginx/conf/ssl
  -h, --help             Show this help

Run without options in a terminal for the Chinese management menu.
Uninstall keeps LNMP, websites, databases, source certificates and system packages.
Requires Linux amd64/arm64, systemd and root. New installations prompt for a
12-72 byte password and a certificate choice. Only HTTPS is supported.
Upgrades preserve credentials, services.yaml and an existing HTTPS listener.
Monitoring setup requires a terminal and a matching panel binary. Fresh interactive
installs optionally offer it; updates and --no-start never configure LNMP automatically.
EOF
}
die() { printf 'Error: %s\n' "$*" >&2; exit 1; }
need_value() { [[ $# -ge 2 && -n "$2" ]] || die "$1 requires a value"; }
set_action() {
  [[ -z "$action" || "$action" == "$1" ]] || die "Choose only one of --install, --update, --uninstall, --reset-password and --configure-metrics"
  action="$1"
}
while (($#)); do
  case "$1" in
    --install) set_action install; shift; continue ;;
    --update) set_action update; shift; continue ;;
    --uninstall) set_action uninstall; shift; continue ;;
    --reset-password) set_action reset-password; shift; continue ;;
    --configure-metrics) set_action configure-metrics; shift; continue ;;
    --yes) auto_confirm=true; shift; continue ;;
    --version) need_value "$@"; release_version="$2"; shift 2 ;;
    --archive) need_value "$@"; archive_path="$2"; shift 2 ;;
    --checksum-file) need_value "$@"; checksum_path="$2"; shift 2 ;;
    --username) need_value "$@"; admin_username="$2"; shift 2 ;;
    --password-stdin) password_stdin=true; install_options=true; shift; continue ;;
    --no-start) start_service=false; shift ;;
    --check) check_only=true; shift ;;
    --with-screenshots) with_screenshots=true; shift ;;
    --tls-mode) need_value "$@"; tls_mode="$2"; shift 2 ;;
    --certificate-dir) need_value "$@"; certificate_dir="${2%/}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown argument: $1" ;;
  esac
  install_options=true
  reset_incompatible=true
done
if "$show_menu"; then
  [[ -t 0 ]] || die "Use --install, --update, --reset-password --password-stdin or --uninstall --yes without a terminal; --configure-metrics requires a terminal"
  printf 'LNMP GoPanel 管理\n  1) 安装面板\n  2) 从 GitHub Release 更新\n  3) 完全卸载\n  4) 重置管理员密码（同时清除二步验证）\n  5) 配置服务监控指标\n  0) 退出\n'
  while true; do
    read -r -p '请选择 [0/1/2/3/4/5]：' choice || die "Menu cancelled"
    case "$choice" in
      1) action=install; break ;;
      2) action=update; break ;;
      3) action=uninstall; break ;;
      4) action=reset-password; break ;;
      5) action=configure-metrics; break ;;
      0) exit 0 ;;
      *) printf '请输入 0、1、2、3、4 或 5。\n' ;;
    esac
  done
fi
action="${action:-install}"
if [[ "$action" == configure-metrics ]]; then
  ! "$install_options" || die "--configure-metrics cannot be combined with installation options"
  ! "$auto_confirm" || die "--yes is only valid with --uninstall"
elif [[ "$action" == reset-password ]]; then
  ! "$reset_incompatible" || die "--reset-password only accepts --password-stdin"
  ! "$auto_confirm" || die "--yes is only valid with --uninstall"
elif [[ "$action" == uninstall ]]; then
  ! "$install_options" || die "--uninstall cannot be combined with installation options"
else
  ! "$auto_confirm" || die "--yes is only valid with --uninstall"
fi
[[ "$action" != update || -z "$archive_path" ]] || die "--update downloads from GitHub Release; use --install --archive for a local package"

[[ "$(uname -s)" == Linux ]] || die "Only Linux is supported"
case "$(uname -m)" in
  x86_64|amd64) target_arch=amd64 ;;
  aarch64|arm64) target_arch=arm64 ;;
  *) die "Only amd64 and arm64 are supported" ;;
esac
[[ "$release_version" == latest || "$release_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][a-zA-Z0-9.-]+)?$ ]] || die "Invalid release tag"
[[ -n "$admin_username" && ${#admin_username} -le 128 ]] || die "Invalid administrator name"
[[ -z "$tls_mode" || "$tls_mode" == self-signed || "$tls_mode" == existing ]] || die "--tls-mode must be self-signed or existing"
if [[ -n "$certificate_dir" ]]; then
  [[ "$tls_mode" != self-signed ]] || die "--certificate-dir cannot be used with self-signed mode"
  tls_mode=existing
fi
if ! "$check_only"; then
  [[ $EUID -eq 0 ]] || die "Run this installer as root"
  [[ -d /run/systemd/system ]] || die "A running systemd installation is required"
fi
# Keep the lock file in place: unlinking it would let another process lock a
# different inode while this invocation is still running.
if ! "$check_only"; then
  for program in systemctl flock; do
    command -v "$program" >/dev/null || die "Missing command: $program"
  done
  [[ ! -L /run/lnmp-gopanel-install.lock ]] || die "Refusing a symlink lock file"
  exec 9>/run/lnmp-gopanel-install.lock
  flock -n 9 || die "Another panel installation, update, uninstall, password reset or metrics setup is running"
fi
uninstall_panel() {
  local destination parent load_state confirmation
  local -a removals=(
    /usr/local/bin/lnmp-gopanel
    /usr/local/libexec/lnmp-gopanel-control
    /etc/systemd/system/lnmp-gopanel.service
    /etc/systemd/system/lnmp-gopanel.service.d
    /etc/sudoers.d/lnmp-gopanel
    /etc/lnmp-gopanel
    /var/lib/lnmp-gopanel
    /var/backups/lnmp-gopanel
  )
  for program in rm getent id userdel groupdel; do
    command -v "$program" >/dev/null || die "Missing command: $program"
  done
  # A leaf symlink is removed as a link; never traverse a symlink parent.
  for destination in "${removals[@]}"; do
    parent="${destination%/*}"
    while [[ "$parent" != / ]]; do
      [[ ! -L "$parent" ]] || die "Refusing a symlink parent: $parent"
      parent="${parent%/*}"
      parent="${parent:-/}"
    done
  done
  if getent passwd lnmp-panel >/dev/null; then
    [[ "$(id -u lnmp-panel)" != 0 ]] || die "Refusing to remove a root account"
  fi
  if getent group lnmp-panel >/dev/null; then
    [[ "$(getent group lnmp-panel)" != lnmp-panel:*:0:* ]] || die "Refusing to remove a root group"
  fi
  printf '完全卸载将删除面板程序、服务、权限规则、以下配置和数据，以及 lnmp-panel 专用账号和组：\n'
  printf '  %s\n' "${removals[@]}"
  printf '保留 LNMP、网站、数据库、网站证书及其自动续期任务、Chromium 等系统依赖。自定义到其他目录的数据请自行清理。\n'
  if ! "$auto_confirm"; then
    [[ -t 0 ]] || die "Uninstall needs a terminal confirmation; use --uninstall --yes"
    read -r -p '确认完全卸载？输入 yes 继续：' confirmation || die "Uninstall cancelled"
    if [[ "$confirmation" != yes ]]; then printf '已取消卸载。\n'; return; fi
  fi
  load_state="$(systemctl show lnmp-gopanel --property=LoadState --value)" || die "Cannot determine panel service state"
  if [[ "$load_state" != not-found ]]; then
    systemctl stop lnmp-gopanel || die "Cannot stop panel; no panel files were removed"
    systemctl disable lnmp-gopanel || die "Cannot disable panel; no panel files were removed"
    systemctl reset-failed lnmp-gopanel || die "Cannot reset panel service state; no panel files were removed"
  fi
  for destination in "${removals[@]}"; do
    rm -rf -- "$destination" || die "Cannot remove $destination; uninstall is incomplete"
  done
  systemctl daemon-reload || die "Cannot reload systemd; uninstall is incomplete"
  if getent passwd lnmp-panel >/dev/null; then
    userdel lnmp-panel || die "Cannot remove lnmp-panel account; uninstall is incomplete"
  fi
  if getent group lnmp-panel >/dev/null; then
    groupdel lnmp-panel || die "Cannot remove lnmp-panel group; uninstall is incomplete"
  fi
  printf '面板已完全卸载。\n'
}
if [[ "$action" == uninstall ]]; then
  uninstall_panel
  exit 0
fi
reset_panel_password() (
  local reset_was_active=false reset_saved=false reset_help
  local -a reset_args=(auth reset-password --config /etc/lnmp-gopanel/config.yaml)
  [[ -x /usr/local/bin/lnmp-gopanel && -f /etc/lnmp-gopanel/config.yaml ]] || die "Panel is not installed"
  [[ -x /usr/bin/sudo ]] || die "Install sudo first"
  getent passwd lnmp-panel >/dev/null || die "Panel service account does not exist"
  # Probe before stopping the service: older binaries do not have this subcommand.
  reset_help="$(/usr/bin/sudo -n -u lnmp-panel -- /usr/local/bin/lnmp-gopanel auth reset-password --help 2>&1 || true)"
  [[ "$reset_help" == *'Usage of auth reset-password:'* && "$reset_help" == *'-password-stdin'* ]] || \
    die "Installed panel does not support password reset; update the panel first"
  if "$password_stdin"; then
    reset_args+=(--password-stdin)
  else
    [[ -t 0 ]] || die "Password reset needs a terminal or --password-stdin"
  fi
  printf '将重置管理员密码，同时清除 TOTP 和恢复码。\n'
  if systemctl is-active --quiet lnmp-gopanel; then reset_was_active=true; fi
  # Invoked indirectly by the EXIT trap below.
  # shellcheck disable=SC2317
  restore_reset_service() {
    local reset_status=$?
    trap - EXIT INT TERM
    if "$reset_was_active"; then
      if ! systemctl restart lnmp-gopanel; then
        if "$reset_saved"; then
          printf '密码已重置且二步验证已清除，但面板启动失败；请检查服务后使用新密码登录。\n' >&2
        else
          printf '密码重置失败，恢复面板运行状态也失败；请检查服务。\n' >&2
        fi
        reset_status=1
      fi
    fi
    exit "$reset_status"
  }
  trap restore_reset_service EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  systemctl stop lnmp-gopanel || die "Cannot stop panel for offline password reset"
  /usr/bin/sudo -n -u lnmp-panel -- /usr/local/bin/lnmp-gopanel "${reset_args[@]}" || die "Administrator password reset failed"
  reset_saved=true
)
if [[ "$action" == reset-password ]]; then
  reset_panel_password
  exit 0
fi
configure_panel_metrics() {
  local metrics_help
  if [[ ! -x /usr/local/bin/lnmp-gopanel || ! -f /etc/lnmp-gopanel/config.yaml ]]; then
    printf '请先安装面板，再配置服务监控指标。\n' >&2
    return 1
  fi
  # Capability detection never changes service files or starts a download.
  metrics_help="$(/usr/local/bin/lnmp-gopanel metrics configure --help 2>&1 || true)"
  if [[ "$metrics_help" != *'Usage of metrics configure:'* || "$metrics_help" != *'-config string'* ]]; then
    printf '当前面板不支持服务监控配置向导，请先升级面板；仅更新 install.sh 不会更新二进制。\n' >&2
    return 1
  fi
  if [[ ! -t 0 ]]; then
    printf '服务监控配置需要交互终端，请运行 sudo bash install.sh --configure-metrics。\n' >&2
    return 1
  fi
  /usr/local/bin/lnmp-gopanel metrics configure --config /etc/lnmp-gopanel/config.yaml
}
if [[ "$action" == configure-metrics ]]; then
  configure_panel_metrics
  exit $?
fi
if [[ "$action" == update ]]; then
  [[ -f /etc/lnmp-gopanel/config.yaml && -f /etc/lnmp-gopanel/services.yaml && -f /usr/local/bin/lnmp-gopanel ]] || die "No complete panel installation found; use --install to install or repair it"
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
choose_certificate() {
  local choice index found=false
  local -a certificate_dirs=()
  if [[ -z "$tls_mode" ]]; then
    [[ -t 0 ]] || die "Certificate selection needs a terminal; use --tls-mode self-signed or --tls-mode existing --certificate-dir DIR"
    printf '面板仅提供 HTTPS，请选择 SSL 证书：\n  1) 自动生成自签名证书（无需填写表单）\n  2) 借用服务器现有证书（扫描 /usr/local/nginx/conf/ssl）\n'
    while true; do
      read -r -p '请选择 [1/2，默认 1]：' choice || die "Certificate selection cancelled"
      case "$choice" in
        ""|1) tls_mode=self-signed; break ;;
        2) tls_mode=existing; break ;;
        *) printf '请输入 1 或 2。\n' ;;
      esac
    done
  fi
  if [[ "$tls_mode" == existing ]]; then
    "$payload/bin/lnmp-gopanel" https --list-certificates > "$work_dir/certificates"
    mapfile -t certificate_dirs < "$work_dir/certificates"
    ((${#certificate_dirs[@]})) || die "No valid certificate/key pairs found under /usr/local/nginx/conf/ssl; use --tls-mode self-signed"
    if [[ -z "$certificate_dir" ]]; then
      [[ -t 0 ]] || die "Existing certificate selection needs a terminal or --certificate-dir DIR"
      printf '可用的证书目录（已校验证书有效期及私钥匹配）：\n'
      for index in "${!certificate_dirs[@]}"; do
        printf '  %d) %s\n' "$((index + 1))" "${certificate_dirs[$index]}"
      done
      while true; do
        read -r -p '请选择证书目录编号：' choice || die "Certificate selection cancelled"
        if [[ "$choice" =~ ^[1-9][0-9]{0,5}$ ]] && ((choice <= ${#certificate_dirs[@]})); then
          certificate_dir="${certificate_dirs[$((choice - 1))]}"
          break
        fi
        printf '请输入列表中的编号。\n'
      done
    fi
    for choice in "${certificate_dirs[@]}"; do
      if [[ "$choice" == "$certificate_dir" ]]; then found=true; break; fi
    done
    "$found" || die "Certificate directory is not a usable scanned directory: $certificate_dir"
    tls_args+=(--certificate-dir "$certificate_dir")
  fi
}
check_helper() {
  local name action
  # Query exact permissions as the panel account; never run a scan or change a
  # managed service during installation. Syntax validation alone is insufficient
  # when the system sudoers does not include /etc/sudoers.d.
  # Only check authorization for cron, logs and virtual hosts: these sources
  # are optional. Never read site/account data, create sites, issue certificates
  # or change managed services as an installation self-check.
  for name in nginx nginx-repair-preview nginx-repair docker components storage cron logs-list logs-read lnmp-install-inspect lnmp-install-prepare lnmp-install-start lnmp-install-read \
    vhosts-list vhosts-config vhosts-format vhosts-usage vhosts-toggle vhosts-options vhosts-create vhosts-ssl-renew-preview vhosts-ssl-renew \
    vhosts-basic-auth-get vhosts-basic-auth-set vhosts-basic-auth-disable; do
    case "$name" in
      nginx) action=status ;;
      nginx-repair-*) action="${name#nginx-}"; name=nginx ;;
      nginx-repair) name=nginx; action=repair ;;
      docker|components) action=snapshot ;;
      storage) action=scan ;;
      cron) action=list ;;
      logs-list) name=logs; action=list ;;
      logs-read) name=logs; action="read" ;;
      lnmp-install-*) action="${name#lnmp-install-}"; name=lnmp-install ;;
      vhosts-*) action="${name#vhosts-}"; name=vhosts ;;
    esac
    # The installer is root; redirections intentionally stay outside sudo.
    # shellcheck disable=SC2024
    if ! /usr/bin/sudo -n -u lnmp-panel -- /usr/bin/sudo -n -l -- \
      /usr/local/libexec/lnmp-gopanel-control "$name" "$action" > "$work_dir/helper-check.log" 2>&1; then
      cat "$work_dir/helper-check.log" >&2
      die "Panel account cannot use helper: $name $action; check /etc/sudoers includes /etc/sudoers.d/lnmp-gopanel"
    fi
  done
  # Both snapshots are read-only and succeed when the optional tools are absent.
  # Exercise each entry point to catch an outdated helper or runtime restrictions.
  # Keep successful inventories in the private temporary directory until cleanup.
  for name in docker components; do
    # shellcheck disable=SC2024
    if ! /usr/bin/sudo -n -u lnmp-panel -- /usr/bin/sudo -n -- \
      /usr/local/libexec/lnmp-gopanel-control "$name" snapshot > "$work_dir/helper-check.json" 2> "$work_dir/helper-check.log"; then
      cat "$work_dir/helper-check.json" "$work_dir/helper-check.log" >&2
      die "Panel account cannot execute the helper: $name snapshot; check sudo, executable permissions and privilege restrictions"
    fi
  done
}
check_panel_privileges() {
  local panel_pid no_new_privs
  panel_pid="$(systemctl show lnmp-gopanel --property=MainPID --value)"
  [[ "$panel_pid" =~ ^[1-9][0-9]*$ ]] || die "Cannot determine panel MainPID"
  [[ -r "/proc/$panel_pid/status" ]] || die "Cannot read panel process status: /proc/$panel_pid/status"
  no_new_privs="$(awk '$1 == "NoNewPrivs:" { print $2 }' "/proc/$panel_pid/status")"
  [[ "$no_new_privs" == 0 ]] || die "Panel process has NoNewPrivs=$no_new_privs; sudo cannot elevate. Check systemctl cat lnmp-gopanel and overrides for implicit NoNewPrivileges restrictions"
}
configure_ubuntu_chromium_repository() {
  local codename="$1" destination
  local fingerprint=5301FA4FD93244FBC6F6149982BB6851C64F6880
  local keyring=/etc/apt/keyrings/lnmp-gopanel-chromium.asc
  local source=/etc/apt/sources.list.d/lnmp-gopanel-chromium.sources
  local preferences=/etc/apt/preferences.d/lnmp-gopanel-chromium
  command -v curl >/dev/null || die "Missing command: curl"
  for destination in /etc/apt /etc/apt/keyrings /etc/apt/sources.list.d /etc/apt/preferences.d "$keyring" "$source" "$preferences"; do
    [[ ! -L "$destination" ]] || die "Refusing a symlink Chromium repository destination: $destination"
  done
  printf 'Installing native Chromium from the third-party XtraDeb PPA (Snap is incompatible with the screenshot account and profile directory).\n'
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates
  curl --proto '=https' --tlsv1.2 -fsSL --retry 3 --connect-timeout 15 --max-time 60 \
    "https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x$fingerprint" -o "$work_dir/chromium-key.asc"
  cat > "$work_dir/chromium.sources" <<EOF
Types: deb
URIs: https://ppa.launchpadcontent.net/xtradeb/apps/ubuntu
Suites: $codename
Components: main
Architectures: $target_arch
Signed-By: $keyring $fingerprint
EOF
  # Only Chromium packages may come from this PPA; keep other system packages
  # on their existing repositories. APT also verifies the full signing fingerprint.
  cat > "$work_dir/chromium.preferences" <<'EOF'
Package: chromium*
Pin: release o=LP-PPA-xtradeb-apps
Pin-Priority: 700

Package: *
Pin: release o=LP-PPA-xtradeb-apps
Pin-Priority: -1
EOF
  install -d -o root -g root -m 0755 /etc/apt/keyrings /etc/apt/sources.list.d /etc/apt/preferences.d
  atomic_install "$work_dir/chromium-key.asc" "$keyring" 0644
  atomic_install "$work_dir/chromium.preferences" "$preferences" 0644
  atomic_install "$work_dir/chromium.sources" "$source" 0644
}
configure_chromium_apparmor() {
  local profile=/etc/apparmor.d/lnmp-gopanel-chromium
  # Ubuntu 24.04+ can restrict user namespaces even for an unsandboxed
  # AppArmor label. Grant userns only to the native Chromium executable.
  [[ -x /usr/lib/chromium/chromium ]] || return 0
  [[ -r /proc/sys/kernel/apparmor_restrict_unprivileged_userns ]] || return 0
  [[ "$(cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns)" == 1 ]] || return 0
  if ! command -v apparmor_parser >/dev/null; then
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends apparmor
  fi
  if [[ -f /etc/apparmor.d/chromium ]]; then
    apparmor_parser -r /etc/apparmor.d/chromium
    return
  fi
  [[ ! -L /etc/apparmor.d && ! -L "$profile" ]] || die "Refusing a symlink Chromium AppArmor destination"
  cat > "$work_dir/chromium.apparmor" <<'EOF'
abi <abi/4.0>,
include <tunables/global>

profile lnmp-gopanel-chromium /usr/lib/chromium/chromium flags=(unconfined) {
  userns,
}
EOF
  install -d -o root -g root -m 0755 /etc/apparmor.d
  atomic_install "$work_dir/chromium.apparmor" "$profile" 0644
  apparmor_parser -r "$profile"
}
install_screenshot_dependencies() {
  local browser browser_path distro_id distro_version codename
  for browser in chromium chromium-browser google-chrome google-chrome-stable; do
    if browser_path="$(command -v "$browser")"; then
      # Ubuntu's /usr/bin/chromium-browser can be a wrapper for the Snap.
      # Install native Chromium so runtime detection selects /usr/bin/chromium.
      if [[ "$browser_path" == /snap/* ]] || grep -aqE '(/snap/|snap[[:space:]]+run[[:space:]]+chromium)' "$browser_path"; then
        printf 'Skipping Snap screenshot browser: %s\n' "$browser_path"
        break
      fi
      printf 'Using installed screenshot browser: %s\n' "$browser"
      if [[ "$browser" == chromium ]]; then configure_chromium_apparmor; fi
      return
    fi
  done
  [[ -r /etc/os-release ]] || die "Cannot identify distribution; install Chromium manually and configure screenshots.browser"
  # The target distribution provides this file at runtime.
  # shellcheck disable=SC1091
  distro_id="$(. /etc/os-release && printf '%s' "${ID:-}")"
  # shellcheck disable=SC1091
  distro_version="$(. /etc/os-release && printf '%s' "${VERSION_ID:-}")"
  case "$distro_id:$distro_version" in
    debian:12|debian:13) ;;
    ubuntu:22.04) codename=jammy ;;
    ubuntu:24.04) codename=noble ;;
    ubuntu:26.04) codename=resolute ;;
    *) die "--with-screenshots automatic browser installation supports Debian 12/13 and Ubuntu 22.04/24.04/26.04. Install native Chromium manually and configure screenshots.browser on other systems" ;;
  esac
  command -v apt-get >/dev/null || die "Missing command: apt-get"
  if [[ "$distro_id" == ubuntu ]]; then configure_ubuntu_chromium_repository "$codename"; fi
  printf 'Installing optional Chromium screenshot dependencies...\n'
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends chromium chromium-sandbox fonts-noto-cjk ca-certificates
  configure_chromium_apparmor
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
# Older releases support init but silently treat an unknown https subcommand as
# server arguments. Probe init help before asking for certificates or passwords.
# Go's flag package prints help and returns a nonzero status; inspect the flags.
chmod 0755 "$payload/bin/lnmp-gopanel"
init_help="$("$payload/bin/lnmp-gopanel" init --help 2>&1 || true)"
[[ "$init_help" == *'-tls-dir '* && "$init_help" == *'-certificate-dir '* ]] || \
  die "Package $(cat "$payload/VERSION") does not support HTTPS initialization and is incompatible with this installer. Use a newer Release, or build a package from the same source and install with --archive (see README). Updating install.sh alone does not update the panel binary."
printf 'Validated package: %s (%s)\n' "$(cat "$payload/VERSION")" "$target_arch"
if "$check_only"; then installation_complete=true; exit 0; fi

for program in systemctl visudo getent groupadd useradd flock; do
  command -v "$program" >/dev/null || die "Missing command: $program"
done
[[ -x /usr/bin/sudo ]] || die "Install sudo first (the panel requires /usr/bin/sudo)"
for destination in "${targets[@]}" /etc/lnmp-gopanel /etc/lnmp-gopanel/tls; do
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
tls_args=(--tls-dir /etc/lnmp-gopanel/tls)
if "$fresh_install" || [[ -n "$tls_mode" ]] || ! "$payload/bin/lnmp-gopanel" https --check >/dev/null 2>&1; then
  tls_changed=true
  choose_certificate
fi
if "$fresh_install"; then
  init_args=(init --dir "$work_dir/config" --username "$admin_username" --listen 0.0.0.0:0 "${tls_args[@]}")
  if "$password_stdin"; then
    init_args+=(--password-stdin)
  elif [[ ! -t 0 ]]; then
    die "A new installation needs a terminal or --password-stdin"
  fi
  "$payload/bin/lnmp-gopanel" "${init_args[@]}"
elif "$tls_changed"; then
  mkdir -p "$work_dir/config"
  cp -- /etc/lnmp-gopanel/config.yaml "$work_dir/config/config.yaml"
  "$payload/bin/lnmp-gopanel" https --config "$work_dir/config/config.yaml" "${tls_args[@]}"
fi
if ! getent group lnmp-panel >/dev/null; then groupadd --system lnmp-panel; fi
if ! getent passwd lnmp-panel >/dev/null; then
  nologin_shell="$(command -v nologin)"
  useradd --system --gid lnmp-panel --no-create-home --home-dir /nonexistent --shell "$nologin_shell" lnmp-panel
fi
[[ "$(id -u lnmp-panel)" != 0 ]] || die "The panel account must not be root"
if "$with_screenshots"; then install_screenshot_dependencies; fi
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
[[ ! -L /var/lib/lnmp-gopanel ]] || die "Refusing a symlink screenshot state directory"
install -d -o lnmp-panel -g lnmp-panel -m 0700 /var/lib/lnmp-gopanel
atomic_install "$payload/bin/lnmp-gopanel" /usr/local/bin/lnmp-gopanel 0755
atomic_install "$payload/bin/lnmp-gopanel-control" /usr/local/libexec/lnmp-gopanel-control 0755
atomic_install "$payload/deploy/lnmp-gopanel.service" /etc/systemd/system/lnmp-gopanel.service 0644
atomic_install "$payload/deploy/lnmp-gopanel.sudoers" /etc/sudoers.d/lnmp-gopanel 0440
if "$tls_changed"; then
  install -d -o root -g lnmp-panel -m 0750 /etc/lnmp-gopanel/tls
  atomic_install "$work_dir/config/tls/panel.crt" /etc/lnmp-gopanel/tls/panel.crt 0640 lnmp-panel
  atomic_install "$work_dir/config/tls/panel.key" /etc/lnmp-gopanel/tls/panel.key 0640 lnmp-panel
  atomic_install "$work_dir/config/config.yaml" /etc/lnmp-gopanel/config.yaml 0640 lnmp-panel
fi
if "$fresh_install"; then
  atomic_install "$work_dir/config/services.yaml" /etc/lnmp-gopanel/services.yaml 0644
fi
chown root:lnmp-panel /etc/lnmp-gopanel/config.yaml
chmod 0640 /etc/lnmp-gopanel/config.yaml
chown root:root /etc/lnmp-gopanel/services.yaml
chmod 0644 /etc/lnmp-gopanel/services.yaml
visudo -cf /etc/sudoers
panel_address="$(/usr/bin/sudo -n -u lnmp-panel -- /usr/local/bin/lnmp-gopanel https --check)" || die "Panel account cannot read a valid HTTPS certificate and key"
check_helper
if "$with_screenshots"; then
  if ! /usr/bin/sudo -n -u lnmp-panel -- /usr/local/bin/lnmp-gopanel check-screenshots; then
    die "Screenshot component check failed; verify Chromium, sandbox and screenshots.state_dir permissions"
  fi
else
  printf 'Screenshot browser is optional; use --with-screenshots on Debian 12/13 or Ubuntu 22.04/24.04/26.04 to install and check it.\n'
fi
systemctl daemon-reload
if "$start_service"; then
  systemctl enable lnmp-gopanel
  systemctl restart lnmp-gopanel
  sleep 1
  systemctl is-active --quiet lnmp-gopanel || die "Panel did not start; check journalctl -u lnmp-gopanel"
  check_panel_privileges
else
  printf 'Service was not enabled or restarted (--no-start).\n'
  printf 'Helper permissions checked; verify /proc/<MainPID>/status has NoNewPrivs: 0 after starting the panel.\n'
fi
installation_complete=true
printf 'Installation complete. Backups: %s\n' "$backup_dir"
printf 'Configuration: /etc/lnmp-gopanel/config.yaml\n'
printf 'Check /etc/lnmp-gopanel/services.yaml and use systemctl status lnmp-gopanel.\n'
printf 'HTTPS listener: %s\n' "$panel_address"
printf '访问地址：https://服务器IP或证书域名:%s/；请在安全组和防火墙中放行该 TCP 端口。\n' "${panel_address##*:}"
if "$tls_changed"; then
  if [[ "$tls_mode" == self-signed ]]; then
    printf '已自动生成 ECDSA P-256 自签名证书（有效期一年），浏览器需要信任该证书。\n'
  else
    printf '已复制证书：%s；请使用证书覆盖的域名访问。原证书续期后，使用相同 --certificate-dir 参数重新安装以同步证书。\n' "$certificate_dir"
  fi
fi

# Optional setup has its own backups and rollback. The panel installation is
# already committed, so a declined or failed wizard cannot roll it back.
if "$fresh_install" && "$start_service" && [[ -t 0 ]]; then
  if read -r -p '是否现在配置 Nginx、PHP-FPM、数据库监控？[y/N]：' metrics_choice; then
    case "$metrics_choice" in
      y|Y|yes|YES)
        if ! configure_panel_metrics; then
          printf '面板已安装成功，服务监控配置未完成；可稍后运行 --configure-metrics 重试。\n' >&2
          exit 1
        fi
        ;;
      *) printf '已跳过，可稍后运行 sudo bash install.sh --configure-metrics。\n' ;;
    esac
  else
    printf '\n已跳过服务监控配置。\n'
  fi
fi
