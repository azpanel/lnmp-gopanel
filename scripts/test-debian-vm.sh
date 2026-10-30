#!/usr/bin/env bash
set -Eeuo pipefail

[[ "${GITHUB_ACTIONS:-}" == true && "${RUNNER_ENVIRONMENT:-}" == github-hosted ]] || {
  echo 'Debian VM integration is only enabled on disposable GitHub-hosted runners' >&2
  exit 1
}
project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
archive="$(realpath -- "${1:?Pass the amd64 release archive}")"
work="$(mktemp -d)"
vm_pid=''
# Invoked indirectly by the EXIT trap.
# shellcheck disable=SC2317
cleanup() {
  if [[ -n "$vm_pid" ]]; then kill "$vm_pid" 2>/dev/null || true; wait "$vm_pid" 2>/dev/null || true; fi
  python3 - "$work" <<'PY'
import shutil, sys
shutil.rmtree(sys.argv[1])
PY
}
trap cleanup EXIT
mkdir -p "$project_root/test-results"
image=debian-12-genericcloud-amd64.qcow2
source_url=https://cloud.debian.org/images/cloud/bookworm/latest
curl --fail --location --retry 3 "$source_url/SHA512SUMS" -o "$work/SHA512SUMS"
curl --fail --location --retry 3 "$source_url/$image" -o "$work/$image"
# Debian publishes SHA-512 manifests, rather than SHA-256, for these cloud images.
python3 - "$work" "$image" <<'PY'
import hashlib, pathlib, sys
root, name = pathlib.Path(sys.argv[1]), sys.argv[2]
entries = [line.split() for line in (root / 'SHA512SUMS').read_text().splitlines()]
expected = [digest for digest, path in entries if path.lstrip('*./') == name]
if len(expected) != 1 or hashlib.file_digest((root / name).open('rb'), 'sha512').hexdigest() != expected[0]:
    raise SystemExit('Debian cloud image checksum mismatch')
PY
ssh-keygen -q -t ed25519 -N '' -f "$work/key"
public_key="$(cat "$work/key.pub")"
cat > "$work/user-data" <<EOF
#cloud-config
users:
  - name: ci
    groups: [sudo]
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - $public_key
ssh_pwauth: false
manage_etc_hosts: true
runcmd:
  - [sh, -c, "printf 'disposable\\n' > /run/gopanel-ci-guest"]
EOF
printf 'instance-id: gopanel-ci\nlocal-hostname: gopanel-ci\n' > "$work/meta-data"
genisoimage -quiet -output "$work/seed.iso" -volid cidata -joliet -rock "$work/user-data" "$work/meta-data"
qemu-img create -q -f qcow2 -F qcow2 -b "$work/$image" "$work/disk.qcow2" 12G
acceleration=(-accel "tcg,thread=multi" -cpu max)
if [[ -r /dev/kvm && -w /dev/kvm ]]; then acceleration=(-accel kvm -cpu host); fi
# Allocate the forwarded port dynamically; this runner owns the QEMU process.
port="$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    print(sock.getsockname()[1])
PY
)"
qemu-system-x86_64 "${acceleration[@]}" -m 2048 -smp 2 -display none -no-reboot \
  -drive "file=$work/disk.qcow2,if=virtio" -drive "file=$work/seed.iso,media=cdrom,readonly=on" \
  -netdev "user,id=net0,hostfwd=tcp:127.0.0.1:$port-:22" -device virtio-net-pci,netdev=net0 \
  -serial "file:$project_root/test-results/debian-serial.log" &
vm_pid=$!
ssh_options=(-i "$work/key" -p "$port" -o BatchMode=yes -o ConnectTimeout=5 \
  -o StrictHostKeyChecking=accept-new -o "UserKnownHostsFile=$work/known_hosts")
ready=false
for ((attempt=0; attempt<120; attempt++)); do
  kill -0 "$vm_pid" || { echo 'Debian VM exited before SSH was ready' >&2; exit 1; }
  if ssh "${ssh_options[@]}" ci@127.0.0.1 true 2>/dev/null; then ready=true; break; fi
  sleep 3
done
"$ready" || { echo 'Debian VM did not boot in time' >&2; exit 1; }
ssh "${ssh_options[@]}" ci@127.0.0.1 'sudo cloud-init status --wait && mkdir -p source/scripts source/dist source/test-results'
scp_options=(-i "$work/key" -P "$port" -o BatchMode=yes -o StrictHostKeyChecking=accept-new \
  -o "UserKnownHostsFile=$work/known_hosts")
scp "${scp_options[@]}" "$project_root/install.sh" ci@127.0.0.1:source/
scp "${scp_options[@]}" "$project_root/scripts/test-systemd.py" ci@127.0.0.1:source/scripts/
scp "${scp_options[@]}" "$archive" "$archive.sha256" ci@127.0.0.1:source/dist/
status=0
ssh "${ssh_options[@]}" ci@127.0.0.1 \
  'sudo apt-get update && sudo apt-get install -y sudo curl ca-certificates tar gzip python3 chromium chromium-sandbox fonts-noto-cjk && sudo python3 source/scripts/test-systemd.py --archive source/dist/lnmp-gopanel_linux_amd64.tar.gz' || status=$?
scp -r "${scp_options[@]}" ci@127.0.0.1:source/test-results/. "$project_root/test-results/" || true
exit "$status"
