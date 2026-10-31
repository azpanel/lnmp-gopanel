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
backup_root=/var/backups/lnmp-gopanel
pending_file="$backup_root/pending"
transaction_status=""
transaction_id=""
transaction_error=""
declare -a saved_presence=() saved_digest=() saved_uid=() saved_gid=() saved_mode=() recovery_errors=()
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
  --recover              Recover an interrupted installation offline, without upgrading
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
  [[ -z "$action" || "$action" == "$1" ]] || die "Choose only one of --install, --update, --recover, --uninstall, --reset-password and --configure-metrics"
  action="$1"
}
while (($#)); do
  case "$1" in
    --install) set_action install; shift; continue ;;
    --update) set_action update; shift; continue ;;
    --recover) set_action recover; shift; continue ;;
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
if [[ "$action" == recover ]]; then
  ! "$install_options" || die "--recover cannot be combined with installation options"
  ! "$auto_confirm" || die "--yes is only valid with --uninstall"
elif [[ "$action" == configure-metrics ]]; then
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
  flock -n 9 || die "Another panel installation, update, recovery, uninstall, password reset or metrics setup is running"
fi
# Recovery is available before downloads, package checks or configuration probes.
# Records are data, never shell code. Only the fixed targets above are writable.
check_safe_parents() {
  local path="${1%/*}" info owner mode
  while [[ -n "$path" ]]; do
    [[ ! -L "$path" ]] || { printf 'Symlink parent: %s\n' "$path" >&2; return 1; }
    if [[ -e "$path" ]]; then
      [[ -d "$path" ]] || return 1
      info="$(stat -c '%u %a' -- "$path")" || return 1
      read -r owner mode <<< "$info"
      [[ "$owner" == 0 && "$mode" =~ ^[0-7]{3,4}$ ]] && (( (8#$mode & 0022) == 0 )) || return 1
    fi
    [[ "$path" != / ]] || break
    path="${path%/*}"
    path="${path:-/}"
  done
}
private_record() {
  local path="$1" kind="${2:-file}" info
  check_safe_parents "$path" || return 1
  [[ ! -L "$path" ]] || return 1
  if [[ "$kind" == directory ]]; then
    [[ -d "$path" ]] || return 1
    info="$(stat -c '%u %a' -- "$path")" || return 1
    [[ "$info" == '0 700' ]]
  else
    [[ -f "$path" ]] || return 1
    info="$(stat -c '%u %a %h' -- "$path")" || return 1
    [[ "$info" == '0 600 1' ]]
  fi
}
durable_record() {
  local destination="$1" contents="$2" staged
  check_safe_parents "$destination" || return 1
  if [[ -e "$destination" || -L "$destination" ]]; then private_record "$destination" || return 1; fi
  staged="$(mktemp "${destination}.XXXXXXXX")" || return 1
  if ! { printf '%s\n' "$contents" > "$staged" && sync -f "$staged" && mv -fT -- "$staged" "$destination"; }; then
    rm -f -- "$staged"
    return 1
  fi
  sync -f "${destination%/*}"
}
file_digest() {
  local result
  result="$(sha256sum -- "$1")" || return 1
  printf '%s\n' "${result%% *}"
}
load_transaction() {
  local info index presence digest uid gid mode extra line
  local -a pointer=() record=() status_lines=()
  transaction_error="$pending_file"
  private_record "$backup_root" directory && private_record "$pending_file" || return 1
  info="$(stat -c %s -- "$pending_file")" || return 1
  (( info <= 80 )) || return 1
  mapfile -t pointer < "$pending_file"
  [[ ${#pointer[@]} == 1 && "${pointer[0]}" =~ ^[0-9]{8}T[0-9]{6}Z-[0-9]+$ ]] || return 1
  transaction_id="${pointer[0]}"
  backup_dir="$backup_root/$transaction_id"
  transaction_error="$backup_dir/transaction or status"
  private_record "$backup_dir" directory && private_record "$backup_dir/transaction" && private_record "$backup_dir/status" || return 1
  info="$(stat -c %s -- "$backup_dir/transaction")" || return 1
  (( info <= 4096 )) || return 1
  info="$(stat -c %s -- "$backup_dir/status")" || return 1
  (( info <= 32 )) || return 1
  mapfile -t record < "$backup_dir/transaction"
  mapfile -t status_lines < "$backup_dir/status"
  [[ ${#status_lines[@]} == 1 && ${#record[@]} == $((${#targets[@]} + 3)) && "${record[0]}" == lnmp-gopanel-transaction-v1 ]] || return 1
  case "${status_lines[0]}" in pending|committed|rolled-back) transaction_status="${status_lines[0]}" ;; *) return 1 ;; esac
  [[ "${record[1]}" == true || "${record[1]}" == false ]] || return 1
  [[ "${record[2]}" == true || "${record[2]}" == false ]] || return 1
  was_active="${record[1]}"; was_enabled="${record[2]}"
  for index in "${!targets[@]}"; do
    transaction_error="$backup_dir/transaction (target ${targets[$index]})"
    line="${record[$((index + 3))]}"
    IFS=$'\t' read -r info presence digest uid gid mode extra <<< "$line"
    [[ "$info" == "$index" && -z "$extra" && "$line" == "$index"$'\t'"$presence"$'\t'"$digest"$'\t'"$uid"$'\t'"$gid"$'\t'"$mode" ]] || return 1
    case "$presence" in
      present)
        [[ "$digest" =~ ^[a-f0-9]{64}$ && "$uid" =~ ^[0-9]{1,10}$ && "$gid" =~ ^[0-9]{1,10}$ && "$mode" =~ ^[0-7]{3,4}$ ]] || return 1
        transaction_error="$backup_dir/$index (backup of ${targets[$index]})"
        private_record "$backup_dir/$index" || return 1
        [[ "$(file_digest "$backup_dir/$index")" == "$digest" ]] || return 1
        ;;
      absent) [[ "$digest:$uid:$gid:$mode" == '-:-:-:-' && ! -e "$backup_dir/$index" && ! -L "$backup_dir/$index" ]] || return 1 ;;
      *) return 1 ;;
    esac
    saved_presence[index]="$presence"; saved_digest[index]="$digest"
    saved_uid[index]="$uid"; saved_gid[index]="$gid"; saved_mode[index]="$mode"
    transaction_error="${targets[$index]}"
    check_safe_parents "${targets[$index]}" || return 1
    [[ ! -L "${targets[$index]}" ]] || return 1
    [[ ! -e "${targets[$index]}" || -f "${targets[$index]}" ]] || return 1
  done
}
clear_pending() {
  rm -f -- "$pending_file" || return 1
  if ! sync -f "$backup_root"; then
    # Best effort: keep the retry entry point when unlink persistence fails.
    durable_record "$pending_file" "$transaction_id" || return 1
    return 1
  fi
}
recovery_step() {
  local label="$1" details
  shift
  if details="$("$@" 2>&1)"; then return 0; fi
  recovery_errors+=("$label${details:+: ${details:0:4096}}")
  return 1
}
restore_target() {
  local index="$1" destination="${targets[$1]}" staged="${targets[$1]}.restore.$transaction_id"
  check_safe_parents "$destination" || return 1
  [[ ! -L "$destination" && ( ! -e "$destination" || -f "$destination" ) ]] || return 1
  if [[ "${saved_presence[$index]}" == present ]]; then
    [[ ! -L "$staged" && ( ! -e "$staged" || -f "$staged" ) ]] || return 1
    cp -- "$backup_dir/$index" "$staged" || return 1
    chown "${saved_uid[$index]}:${saved_gid[$index]}" "$staged" || return 1
    chmod "${saved_mode[$index]}" "$staged" || return 1
    [[ "$(file_digest "$staged")" == "${saved_digest[$index]}" ]] || return 1
    sync -f "$staged" || return 1
    mv -fT -- "$staged" "$destination" || return 1
  else
    rm -f -- "$destination" || return 1
  fi
  rm -f -- "${destination}.new.${transaction_id##*-}" "$staged" || return 1
  # Absent targets may also have absent parent directories on a fresh install.
  local parent="${destination%/*}"
  while [[ ! -d "$parent" ]]; do parent="${parent%/*}"; done
  sync -f "$parent"
}
verify_service_state() {
  local expected="$1" operation="$2" result
  if systemctl "$operation" --quiet lnmp-gopanel; then result=true; else
    local code=$?
    case "$operation:$code" in is-active:3|is-active:4|is-enabled:1|is-enabled:4) ;; *) return "$code" ;; esac
    result=false
  fi
  [[ "$result" == "$expected" ]] || { printf '%s expected=%s actual=%s\n' "$operation" "$expected" "$result"; return 1; }
}
# Old helpers started init daemons in the panel's cgroup. Replacing the helper
# cannot move those existing processes, and even rollback stops the panel.
# Read /proc rather than assuming a cgroup mount layout (v1, v2 or hybrid).
check_panel_cgroup_safety() {
  local unit="$1" properties key value group="" main_pid="" load_state=""
  local proc entry hierarchy controllers membership name executable member main_seen=false
  local -a entries=() unsafe=()
  if ! properties="$(systemctl show "$unit" --property=LoadState,ControlGroup,MainPID)"; then
    printf '无法检查面板控制组，已阻止可能停止或重启面板的操作。\n' >&2
    return 1
  fi
  while IFS='=' read -r key value; do
    case "$key" in
      LoadState) load_state="$value" ;;
      ControlGroup) group="$value" ;;
      MainPID) main_pid="$value" ;;
    esac
  done <<< "$properties"
  if [[ -z "$load_state" || ! "$main_pid" =~ ^[0-9]+$ ]]; then
    printf '面板控制组状态不完整，已阻止可能停止或重启面板的操作。\n' >&2
    return 1
  fi
  if [[ -z "$group" && "$main_pid" == 0 ]]; then return 0; fi
  if [[ "$group" != /* || "$group" == / || "$group" == *'/../'* || "$group" == */.. || ! -r /proc/self/cgroup ]] || ! command -v readlink >/dev/null; then
    printf '无法可靠读取面板控制组或 /proc，已阻止可能停止或重启面板的操作。\n' >&2
    return 1
  fi
  if [[ "$main_pid" != 0 && ! -r "/proc/$main_pid/cgroup" ]]; then
    printf '无法读取面板主进程控制组：PID=%s；已阻止操作，请确认进程状态后重试。\n' "$main_pid" >&2
    return 1
  fi
  for proc in /proc/[0-9]*; do
    [[ -d "$proc" ]] || continue
    if ! mapfile -t entries 2>/dev/null < "$proc/cgroup" || ((${#entries[@]} == 0)); then
      [[ -d "$proc" ]] || continue
      printf '无法读取进程控制组：%s/cgroup；已阻止可能停止或重启面板的操作。\n' "$proc" >&2
      return 1
    fi
    member=false
    for entry in "${entries[@]}"; do
      IFS=: read -r hierarchy controllers membership <<< "$entry"
      if [[ ( "$hierarchy" == 0 && -z "$controllers" ) || ",$controllers," == *,name=systemd,* ]]; then
        if [[ "$membership" == "$group" || "$membership" == "$group/"* ]]; then member=true; break; fi
      fi
    done
    "$member" || continue
    if [[ "${proc##*/}" == "$main_pid" ]]; then main_seen=true; fi
    if ! IFS= read -r name 2>/dev/null < "$proc/comm"; then
      [[ -d "$proc" ]] || continue
      printf '无法读取面板控制组内的进程名称：%s；已阻止操作。\n' "$proc" >&2
      return 1
    fi
    # comm can differ from the executable name. Inspect both; never print argv,
    # which can contain database passwords. Chromium/helper children are normal.
    executable="$(readlink -- "$proc/exe" 2>/dev/null)" || executable=""
    executable="${executable% (deleted)}"
    executable="${executable##*/}"
    case "$name" in
      nginx|php-fpm*|php[0-9]*-fpm|mysqld*|mariadbd*|redis-server*) ;;
      *) case "$executable" in
        nginx|php-fpm*|php[0-9]*-fpm|mysqld*|mariadbd*|redis-server*) ;;
        *) continue ;;
      esac ;;
    esac
    unsafe+=("PID=${proc##*/} 进程=$name")
  done
  if [[ "$main_pid" != 0 ]] && ! "$main_seen"; then
    printf '面板主进程与 systemd 控制组记录不一致，已阻止操作，请确认进程状态后重试。\n' >&2
    return 1
  fi
  if ((${#unsafe[@]})); then
    printf '发现业务进程仍属于面板控制组 %s：\n' "$group" >&2
    printf '  %s\n' "${unsafe[@]}" >&2
    printf '已阻止操作，避免停止或重启面板时带停业务服务。请勿直接重启面板；在维护窗口通过独立 systemd 单元或 scope 重新托管这些服务，确认进程已离开面板控制组后重试。仅 reload 不会迁移已有进程。\n' >&2
    return 1
  fi
  return 0
}
stop_recovery_service() {
  local load_state
  check_panel_cgroup_safety lnmp-gopanel.service || return 1
  load_state="$(systemctl show lnmp-gopanel --property=LoadState --value)" || return 1
  [[ -n "$load_state" ]] || return 1
  if [[ "$load_state" != not-found ]]; then systemctl stop lnmp-gopanel || return 1; fi
  verify_service_state false is-active
}
report_recovery() {
  local files="$1" service="$2" cleanup_result="$3" result="$4" report
  printf -v report '恢复%s。文件恢复：%s；服务恢复：%s；事务清理：%s。\n备份：%s' "$result" "$files" "$service" "$cleanup_result" "$backup_dir"
  if ((${#recovery_errors[@]})); then
    printf -v report '%s\n失败步骤：\n%s\n重试：sudo bash install.sh --recover\n排查：systemctl status lnmp-gopanel; journalctl -u lnmp-gopanel' "$report" "$(printf '  %s\n' "${recovery_errors[@]}")"
  fi
  printf '%s\n' "$report" >&2
  if ! durable_record "$backup_dir/recovery-report.txt" "$report"; then
    printf '无法保存恢复报告：%s/recovery-report.txt\n' "$backup_dir" >&2
  fi
}
recover_transaction() {
  recovery_errors=()
  if ! load_transaction; then
    printf '恢复未完成：事务记录、备份或目标路径无效：%s；未恢复任何文件。保留 %s，请检查备份后运行 sudo bash install.sh --recover。\n' "$transaction_error" "$pending_file" >&2
    if private_record "$backup_dir" directory; then
      recovery_errors+=("校验事务或备份失败：$transaction_error")
      report_recovery '未执行' '未执行' '保留待恢复标记' '未完成'
    fi
    return 1
  fi
  if [[ "$transaction_status" == committed || "$transaction_status" == rolled-back ]]; then
    if ! recovery_step '清除已完成事务标记' clear_pending; then
      report_recovery '已完成' '已完成' '失败' '未完成'
      return 1
    fi
    if [[ "$transaction_status" == committed ]]; then
      printf '升级已提交，仅清理遗留事务标记，未回滚新版。\n'
    else
      printf '恢复已完成，仅清理遗留事务标记。\n'
    fi
    return 0
  fi
  printf 'Installation failed or interrupted; restoring files from %s\n' "$backup_dir" >&2
  if ! recovery_step '停止当前面板' stop_recovery_service; then
    report_recovery '未执行' '停止失败' '保留待恢复标记' '未完成'
    return 1
  fi
  local index files=成功 service=成功 load_state
  for index in "${!targets[@]}"; do
    if ! recovery_step "恢复文件 ${targets[$index]}" restore_target "$index"; then files=失败; fi
  done
  if ! recovery_step '校验恢复后的 sudoers' visudo -cf /etc/sudoers; then files=失败; fi
  if ! recovery_step '重新加载 systemd' systemctl daemon-reload; then service=失败; fi
  if [[ "$files" == 成功 && "$service" == 成功 ]]; then
    if ! load_state="$(systemctl show lnmp-gopanel --property=LoadState --value 2>&1)"; then
      recovery_errors+=("查询恢复后的服务状态: $load_state"); service=失败
    elif [[ "$load_state" != not-found ]]; then
      if "$was_enabled"; then
        recovery_step '恢复服务启用状态' systemctl enable lnmp-gopanel || service=失败
      else
        recovery_step '恢复服务禁用状态' systemctl disable lnmp-gopanel || service=失败
      fi
    elif "$was_enabled" || "$was_active"; then
      recovery_errors+=('原服务应存在，但恢复后 systemd 找不到服务'); service=失败
    fi
    if [[ "$service" == 成功 ]]; then
      if "$was_active"; then recovery_step '重新启动旧面板' systemctl restart lnmp-gopanel || service=失败; fi
      recovery_step '检查面板运行状态' verify_service_state "$was_active" is-active || service=失败
      recovery_step '检查面板启用状态' verify_service_state "$was_enabled" is-enabled || service=失败
    fi
  else
    service=未启动
  fi
  if [[ "$files" != 成功 || "$service" != 成功 ]]; then
    report_recovery "$files" "$service" '保留待恢复标记' '未完成'
    return 1
  fi
  if ! recovery_step '持久化恢复完成状态' durable_record "$backup_dir/status" rolled-back || ! recovery_step '清除待恢复标记' clear_pending; then
    report_recovery "$files" "$service" '失败' '未完成'
    return 1
  fi
  report_recovery "$files" "$service" '成功' '成功'
}
prepare_transaction() {
  local index destination metadata uid gid mode digest record
  check_safe_parents "$backup_root/placeholder" || return 1
  if [[ -e "$backup_root" || -L "$backup_root" ]]; then private_record "$backup_root" directory || return 1; fi
  transaction_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
  backup_dir="$backup_root/$transaction_id"
  install -d -o root -g root -m 0700 "$backup_root" || return 1
  mkdir -m 0700 -- "$backup_dir" || return 1
  printf -v record 'lnmp-gopanel-transaction-v1\n%s\n%s' "$was_active" "$was_enabled"
  for index in "${!targets[@]}"; do
    destination="${targets[$index]}"
    check_safe_parents "$destination" || return 1
    [[ ! -L "$destination" ]] || return 1
    if [[ -e "$destination" ]]; then
      [[ -f "$destination" ]] || return 1
      metadata="$(stat -c '%u %g %a' -- "$destination")" || return 1
      read -r uid gid mode <<< "$metadata"
      cp -- "$destination" "$backup_dir/$index" || return 1
      chown root:root "$backup_dir/$index" && chmod 0600 "$backup_dir/$index" || return 1
      digest="$(file_digest "$backup_dir/$index")" || return 1
      printf -v record '%s\n%s\tpresent\t%s\t%s\t%s\t%s' "$record" "$index" "$digest" "$uid" "$gid" "$mode"
    else
      printf -v record '%s\n%s\tabsent\t-\t-\t-\t-' "$record" "$index"
    fi
    printf '%s\t%s\n' "$index" "$destination" >> "$backup_dir/manifest" || return 1
  done
  durable_record "$backup_dir/transaction" "$record" && durable_record "$backup_dir/status" pending || return 1
  # sync -f above flushes all backups on their filesystem before publishing pending.
  durable_record "$pending_file" "$transaction_id" || return 1
  transaction_status=pending
}
if ! "$check_only"; then
  if [[ -e "$pending_file" || -L "$pending_file" ]]; then
    case "$action" in install|update|recover) ;; *) die "Unfinished installation; run sudo bash install.sh --recover first" ;; esac
    for program in stat cp mv chown chmod sha256sum sync mktemp rm visudo; do
      command -v "$program" >/dev/null || die "Missing recovery command: $program"
    done
    recover_transaction || exit 1
    backup_dir=""; transaction_id=""; transaction_status=""; was_active=false; was_enabled=false
  elif [[ "$action" == recover ]]; then
    printf '没有待恢复的安装事务。\n'
  fi
  if [[ "$action" == recover ]]; then exit 0; fi
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
    check_panel_cgroup_safety lnmp-gopanel.service || die "卸载已中止，未删除面板文件"
    systemctl stop lnmp-gopanel || die "Cannot stop panel; no panel files were removed"
    systemctl disable lnmp-gopanel || die "Cannot disable panel; no panel files were removed"
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
  check_panel_cgroup_safety lnmp-gopanel.service || die "密码重置已中止"
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
  check_panel_cgroup_safety lnmp-gopanel.service || return 1
  /usr/local/bin/lnmp-gopanel metrics configure --config /etc/lnmp-gopanel/config.yaml
}
if [[ "$action" == configure-metrics ]]; then
  configure_panel_metrics
  exit $?
fi
if [[ "$action" == update ]]; then
  [[ -f /etc/lnmp-gopanel/config.yaml && -f /etc/lnmp-gopanel/services.yaml && -f /usr/local/bin/lnmp-gopanel ]] || die "No complete panel installation found; use --install to install or repair it"
fi
for program in tar gzip sha256sum awk install stat cp mv mkdir rm mktemp sync chown chmod date readlink; do
  command -v "$program" >/dev/null || die "Missing command: $program"
done

atomic_install() {
  local source="$1" destination="$2" mode="$3" group="${4:-root}"
  local staged="${destination}.new.$$"
  install -o root -g "$group" -m "$mode" "$source" "$staged" || return 1
  if "$mutation_started"; then sync -f "$staged" || return 1; fi
  mv -fT -- "$staged" "$destination" || return 1
  if "$mutation_started"; then sync -f "${destination%/*}" || return 1; fi
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
  for name in nginx nginx-repair-preview nginx-repair docker components storage cron logs-list logs-read logging-status logging-enable-php logging-enable-database lnmp-install-inspect lnmp-install-prepare lnmp-install-start lnmp-install-read \
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
      logging-*) action="${name#logging-}"; name=logging ;;
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
    if ! recover_transaction; then
      printf '安装失败且恢复未完成；保留待恢复事务，请运行 sudo bash install.sh --recover。\n' >&2
      if ((status == 0)); then status=1; fi
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
# Always check, including --no-start: a later failure can enter rollback, which
# stops the panel even when normal installation was not going to restart it.
check_panel_cgroup_safety lnmp-gopanel.service || die "安装或升级已中止，未替换面板文件"
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
prepare_transaction || die "Cannot persist installation transaction; no panel files were replaced"
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
  check_panel_cgroup_safety lnmp-gopanel.service || die "面板重启已中止，请按提示处理后运行 --recover"
  systemctl enable lnmp-gopanel
  systemctl restart lnmp-gopanel
  sleep 1
  systemctl is-active --quiet lnmp-gopanel || die "Panel did not start; check journalctl -u lnmp-gopanel"
  check_panel_privileges
else
  printf 'Service was not enabled or restarted (--no-start).\n'
  printf 'Helper permissions checked; verify /proc/<MainPID>/status has NoNewPrivs: 0 after starting the panel.\n'
fi
# Flush metadata changes as well as each atomically installed file before commit.
for destination in "${targets[@]}"; do
  if [[ -f "$destination" ]]; then sync -f "$destination"; fi
done
if ! durable_record "$backup_dir/status" committed; then
  # A successful rename with a failed final sync must never trigger a rollback
  # in this process after the committed record has become visible.
  if [[ "$(< "$backup_dir/status")" == committed ]]; then
    installation_complete=true
    recovery_errors=('提交记录已写入，但持久化失败')
    report_recovery '已完成' '安装检查已通过' '失败' '未完成'
    die "升级已完成，但提交状态持久化失败；保留事务，请运行 sudo bash install.sh --recover"
  fi
  die "Cannot persist installation commit"
fi
installation_complete=true
if ! recovery_step '清除已提交事务标记' clear_pending; then
  report_recovery '已完成' '安装检查已通过' '失败' '未完成'
  die "升级已完成，但事务清理失败；请运行 sudo bash install.sh --recover"
fi
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
