#!/usr/bin/env bash
#
# Tests for the multi process mode of V2bX.sh (one process per node).
#
# Everything runs in a temporary directory with a fake systemctl in PATH: no
# root, no network and no systemd are required.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

failures=0

ok() {
  printf 'ok   %s\n' "$1"
}

fail() {
  printf 'FAIL %s: %s\n' "$1" "$2"
  failures=$((failures + 1))
}

check_eq() {
  if [[ "$2" == "$3" ]]; then
    ok "$1"
  else
    fail "$1" "got [$2], want [$3]"
  fi
}

check_contains() {
  if [[ "$2" == *"$3"* ]]; then
    ok "$1"
  else
    fail "$1" "[$2] does not contain [$3]"
  fi
}

# Captured output carries the colour escapes of the menu script.
strip_colors() {
  printf '%s' "$1" | sed -e "s/$(printf '\033')\[[0-9;]*m//g"
}

check_not_contains() {
  if [[ "$2" != *"$3"* ]]; then
    ok "$1"
  else
    fail "$1" "[$2] unexpectedly contains [$3]"
  fi
}

# Builds a sandbox with a fake systemctl that only reports the units listed in
# FAKE_ACTIVE / FAKE_ENABLED and logs every call.
setup_sandbox() {
  local root="$1"
  mkdir -p "$root/bin" "$root/nodes"
  cat >"$root/bin/systemctl" <<'FAKE'
#!/usr/bin/env bash
echo "systemctl $*" >>"${FAKE_LOG:-/dev/null}"
case "$1" in
  is-active)
    [[ " ${FAKE_ACTIVE:-} " == *" ${3%.service} "* ]] && exit 0
    exit 3
    ;;
  is-enabled)
    [[ " ${FAKE_ENABLED:-} " == *" ${3%.service} "* ]] && exit 0
    exit 1
    ;;
  list-unit-files)
    local unit
    for unit in ${FAKE_ENABLED:-}; do echo "$unit.service enabled"; done
    exit 0
    ;;
  *)
    exit "${FAKE_SYSTEMCTL_RC:-0}"
    ;;
esac
FAKE
  printf '#!/usr/bin/env bash\nexit 0\n' >"$root/bin/journalctl"
  chmod +x "$root/bin/systemctl" "$root/bin/journalctl"
  PATH="$root/bin:$PATH"
  export PATH
}

# Loads the menu script with the paths redirected into the sandbox. The
# redirection happens after the script was loaded: V2bX.sh sets its own paths.
load_menu() {
  local root="$1"
  # The working copy of the script can carry CRLF line endings (a Windows
  # checkout), which bash does not accept, so they are normalised here. CI
  # checks the syntax of the file itself with "bash -n".
  # shellcheck disable=SC1090
  source <(tr -d '\r' <"$REPO_ROOT/V2bX.sh")

  INSTALL_DIR="$root/install"
  CONFIG_DIR="$root/etc"
  NODES_DIR="$CONFIG_DIR/nodes"
  CONFIG_FILE="$CONFIG_DIR/config.json"
  BIN_PATH="$INSTALL_DIR/V2bX"
  SERVICE_NAME="V2bX"
  INSTANCE_PREFIX="v2bx@"
  INSTANCE_TEMPLATE_FILE="$root/v2bx@.service"
  mkdir -p "$INSTALL_DIR" "$NODES_DIR"
  printf '#!/usr/bin/env bash\nexit 0\n' >"$BIN_PATH"
  chmod +x "$BIN_PATH"

  # A test that writes outside of the sandbox would touch a real installation.
  case "$NODES_DIR" in
    "$root"/*) ;;
    *)
      echo "refusing to run: NODES_DIR is $NODES_DIR" >&2
      exit 1
      ;;
  esac
}

write_node_configs() {
  local dir="$1"
  shift
  local name
  mkdir -p "$dir"
  for name in "$@"; do
    printf '{"Nodes":[]}\n' >"$dir/$name.json"
  done
}

root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
setup_sandbox "$root"
# The fake systemctl is a child process, so the state has to be exported.
export FAKE_ACTIVE="" FAKE_ENABLED="" FAKE_SYSTEMCTL_RC=""
export FAKE_LOG="$root/systemctl.log"
: >"$FAKE_LOG"
load_menu "$root"

echo "== the instance list =="
FAKE_ACTIVE=""
FAKE_ENABLED=""
check_eq "no configs is not multi process" "$(multi_configs_exist && echo yes || echo no)" "no"
check_eq "an empty instance list is empty" "$(instance_names | tr '\n' ' ')" ""

write_node_configs "$NODES_DIR" 45678 45679 45680
mkdir -p "$NODES_DIR/dns" "$NODES_DIR/log"
printf '{}\n' >"$NODES_DIR/dns/dns_45678.json"
check_eq "the configs of the instances are listed" "$(instance_names | sort | tr '\n' ' ')" "45678 45679 45680 "
check_eq "generated configs are detected" "$(multi_configs_exist && echo yes || echo no)" "yes"
check_eq "generated configs alone are not multi mode" "$(multi_mode && echo yes || echo no)" "no"
check_eq "the summary counts the instances" "$(instance_summary)" "3 个实例, 0 运行中"

echo
echo "== the mode follows systemd, not the files =="
FAKE_ACTIVE="v2bx@45678 v2bx@45680"
FAKE_ENABLED="v2bx@45679"
check_eq "an active instance means multi mode" "$(multi_mode && echo yes || echo no)" "yes"
check_eq "the summary counts the running instances" "$(instance_summary)" "3 个实例, 2 运行中"

echo
echo "== the status line =="
status="$(strip_colors "$(show_status_line)")"
check_contains "multi mode is reported" "$status" "运行模式: 多进程"
check_contains "the instance count is reported" "$status" "3 个实例, 2 运行中"
check_not_contains "the single process state is not reported" "$status" "V2bX 状态"

FAKE_ACTIVE=""
FAKE_ENABLED=""
status="$(strip_colors "$(show_status_line)")"
check_contains "single mode is reported" "$status" "V2bX 状态"
check_contains "the generated configs are offered" "$status" "v2bx multi migrate"

echo
echo "== start / stop / restart target the instances in multi mode =="
FAKE_ACTIVE="v2bx@45678 v2bx@45679 v2bx@45680"
FAKE_ENABLED="v2bx@45678 v2bx@45679 v2bx@45680"
: >"$FAKE_LOG"
restart_service >/dev/null
log="$(cat "$FAKE_LOG")"
check_contains "the first instance is restarted" "$log" "systemctl restart v2bx@45678"
check_contains "the last instance is restarted" "$log" "systemctl restart v2bx@45680"
check_not_contains "the single process unit is not restarted" "$log" "restart V2bX"

: >"$FAKE_LOG"
stop_service >/dev/null
log="$(cat "$FAKE_LOG")"
check_contains "the instances are stopped" "$log" "systemctl stop v2bx@45679"
check_not_contains "the single process unit is not stopped" "$log" "stop V2bX"

: >"$FAKE_LOG"
start_service >/dev/null
log="$(cat "$FAKE_LOG")"
check_contains "the instances are started" "$log" "systemctl start v2bx@45678"
check_not_contains "the single process unit is not started" "$log" "start V2bX"

: >"$FAKE_LOG"
status_service >/dev/null
log="$(cat "$FAKE_LOG")"
check_contains "the status of the first instance is read" "$log" "is-active --quiet v2bx@45678"

echo
echo "== a failing instance is reported and never falls back to V2bX =="
: >"$FAKE_LOG"
if FAKE_SYSTEMCTL_RC=1 restart_service >/dev/null 2>&1; then
  fail "a failing restart is reported" "the command reported success"
else
  ok "a failing restart is reported"
fi
log="$(cat "$FAKE_LOG")"
check_not_contains "no fall back to the single process unit" "$log" "restart V2bX"

echo
echo "== single process mode still drives V2bX =="
FAKE_ACTIVE="V2bX"
FAKE_ENABLED="V2bX"
: >"$FAKE_LOG"
start_service >/dev/null
log="$(cat "$FAKE_LOG")"
check_contains "the single process unit is checked" "$log" "is-active --quiet V2bX"
check_not_contains "no instance is started" "$log" "start v2bx@"

FAKE_ACTIVE=""
FAKE_ENABLED=""
: >"$FAKE_LOG"
start_service >/dev/null 2>&1 || true
log="$(cat "$FAKE_LOG")"
check_contains "an inactive single process unit is started" "$log" "systemctl start V2bX"

echo
echo "== instances that cannot start are reported =="
FAKE_ACTIVE="v2bx@45678"
FAKE_ENABLED="v2bx@45678 v2bx@99999"
warning="$(strip_colors "$(warn_stale_instances 2>&1)")"
check_contains "the instance without a config is reported" "$warning" "99999"
check_not_contains "the healthy instance is not reported" "$warning" "45678"

echo
echo "== the menu and the usage mention the multi process mode =="
menu="$(strip_colors "$(show_menu)")"
check_contains "the menu has an entry" "$menu" "多进程模式"
check_contains "the menu shows the state" "$menu" "V2bX 状态"
usage="$(show_usage)"
check_contains "the usage documents v2bx multi" "$usage" "v2bx multi"
check_contains "the usage documents the rollback" "$usage" "rollback"

echo
if [[ "$failures" -eq 0 ]]; then
  echo "all menu tests passed"
  exit 0
fi
echo "$failures menu test(s) failed"
exit 1
