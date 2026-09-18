#!/usr/bin/env bash
#
# Tests for the acme.sh helper: the certificate reload has to restart the
# services that actually serve the traffic (the instances in the multi process
# mode, V2bX.service otherwise).
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

root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT

mkdir -p "$root/bin" "$root/config/nodes"
cat >"$root/bin/systemctl" <<'FAKE'
#!/usr/bin/env bash
case "$1" in
  is-active)
    [[ " ${FAKE_ACTIVE:-} " == *" ${3%.service} "* ]] && exit 0
    exit 3
    ;;
  is-enabled)
    [[ " ${FAKE_ENABLED:-} " == *" ${3%.service} "* ]] && exit 0
    exit 1
    ;;
  *) exit 0 ;;
esac
FAKE
chmod +x "$root/bin/systemctl"
export PATH="$root/bin:$PATH"
export FAKE_ACTIVE="" FAKE_ENABLED=""

# shellcheck disable=SC1090
source <(tr -d '\r' <"$REPO_ROOT/acme_cf.sh")
CONFIG_DIR="$root/config"
SERVICE_NAME="V2bX"

echo "== without instances the single process unit is reloaded =="
check_eq "the single process unit is reloaded" "$(reload_cmd)" "systemctl restart V2bX.service"

printf '{}\n' >"$CONFIG_DIR/nodes/45678.json"
printf '{}\n' >"$CONFIG_DIR/nodes/45679.json"
mkdir -p "$CONFIG_DIR/nodes/dns"
printf '{}\n' >"$CONFIG_DIR/nodes/dns/dns_45678.json"

echo
echo "== generated configs alone do not change the reload =="
check_eq "a stopped instance is ignored" "$(reload_cmd)" "systemctl restart V2bX.service"

echo
echo "== a running instance is reloaded =="
FAKE_ACTIVE="v2bx@45678"
check_eq "only the running instance is reloaded" "$(reload_cmd)" "systemctl restart v2bx@45678.service"

FAKE_ACTIVE="v2bx@45678 v2bx@45679"
check_eq "every running instance is reloaded" "$(reload_cmd)" \
  "systemctl restart v2bx@45678.service v2bx@45679.service"

echo
echo "== an enabled instance is reloaded too =="
FAKE_ACTIVE=""
FAKE_ENABLED="v2bx@45679"
check_eq "the enabled instance is reloaded" "$(reload_cmd)" "systemctl restart v2bx@45679.service"

echo
if [[ "$failures" -eq 0 ]]; then
  echo "all acme tests passed"
  exit 0
fi
echo "$failures acme test(s) failed"
exit 1
