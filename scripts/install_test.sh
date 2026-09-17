#!/usr/bin/env bash
#
# Tests for the sidecar config handling of install.sh. Everything runs in a
# temporary directory: no root, no network and no systemd are required.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEMPLATES="dns.json route.json custom_outbound.json custom_inbound.json config_xhttp_reality.json config_naive.json xhttp_template.conf"

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

check_file_exists() {
  if [[ -f "$2" ]]; then
    ok "$1"
  else
    fail "$1" "$2 does not exist"
  fi
}

check_no_backup() {
  local backups
  backups="$(find "$1" -maxdepth 1 -name 'backup-*' | wc -l | tr -d ' ')"
  check_eq "$2" "$backups" "0"
}

# Loads install.sh with the paths redirected into a temporary directory.
load_installer() {
  local root="$1"
  INSTALL_DIR="$root/install"
  CONFIG_DIR="$root/config"
  SERVICE_FILE="$root/V2bX.service"
  mkdir -p "$INSTALL_DIR" "$CONFIG_DIR"
  # shellcheck disable=SC1090
  source "$REPO_ROOT/install.sh"
}

# Puts the templates shipped by this version into INSTALL_DIR, so no test ever
# downloads anything.
prepare_package() {
  local file
  for file in $TEMPLATES; do
    if [[ "$file" == "xhttp_template.conf" ]]; then
      write_default_xhttp_template "$INSTALL_DIR/$file"
    else
      cp "$REPO_ROOT/example/$file" "$INSTALL_DIR/$file"
    fi
  done
}

new_root() {
  mktemp -d /tmp/v2bx-install-test.XXXXXX
}

echo "== first install creates every sidecar =="
root="$(new_root)"
load_installer "$root"
prepare_package
sync_config_templates main ask </dev/null
for file in $TEMPLATES; do
  check_file_exists "$file is installed" "$CONFIG_DIR/$file"
done
check_no_backup "$CONFIG_DIR" "no backup on a fresh install"
cmp -s "$CONFIG_DIR/dns.json" "$REPO_ROOT/example/dns.json" &&
  ok "installed dns.json matches the template" ||
  fail "installed dns.json matches the template" "content differs"

echo
echo "== --configs=keep leaves a modified file alone =="
root="$(new_root)"
load_installer "$root"
prepare_package
sync_config_templates main ask </dev/null >/dev/null
printf '{}\n' >"$CONFIG_DIR/dns.json"
parse_args --configs=keep
sync_config_templates main "$CONFIG_MODE" </dev/null
check_eq "modified dns.json is kept" "$(cat "$CONFIG_DIR/dns.json")" "{}"
check_no_backup "$CONFIG_DIR" "keep does not create a backup"

echo
echo "== --configs=ask without a terminal keeps the files =="
root="$(new_root)"
load_installer "$root"
prepare_package
sync_config_templates main ask </dev/null >/dev/null
printf '{}\n' >"$CONFIG_DIR/dns.json"
output="$(sync_config_templates main ask </dev/null 2>&1)"
check_eq "modified dns.json is kept" "$(cat "$CONFIG_DIR/dns.json")" "{}"
case "$output" in
  *"--configs=overwrite"*) ok "the output points at --configs=overwrite" ;;
  *) fail "the output points at --configs=overwrite" "$output" ;;
esac

echo
echo "== --configs=overwrite replaces and backs up =="
root="$(new_root)"
load_installer "$root"
prepare_package
sync_config_templates main ask </dev/null >/dev/null
printf '{}\n' >"$CONFIG_DIR/dns.json"
parse_args --configs=overwrite
sync_config_templates main "$CONFIG_MODE" </dev/null
cmp -s "$CONFIG_DIR/dns.json" "$REPO_ROOT/example/dns.json" &&
  ok "dns.json was replaced by the template" ||
  fail "dns.json was replaced by the template" "content differs"
backup="$(find "$CONFIG_DIR" -maxdepth 1 -name 'backup-*' | head -n 1)"
check_file_exists "a backup directory was created" "$backup/dns.json"
check_eq "the backup holds the previous file" "$(cat "$backup/dns.json")" "{}"

echo
echo "== config.json is never replaced =="
root="$(new_root)"
load_installer "$root"
prepare_package
cp "$REPO_ROOT/example/config.json" "$INSTALL_DIR/config.json"
printf '{"keep":"me"}\n' >"$CONFIG_DIR/config.json"
parse_args --configs=overwrite
sync_config_templates main "$CONFIG_MODE" </dev/null
check_eq "config.json keeps the node credentials" "$(cat "$CONFIG_DIR/config.json")" '{"keep":"me"}'
case " $CONFIG_TEMPLATES " in
  *" config.json "*) fail "config.json is not a managed template" "it is listed in CONFIG_TEMPLATES" ;;
  *) ok "config.json is not a managed template" ;;
esac

echo
echo "== argument parsing =="
root="$(new_root)"
load_installer "$root"
parse_args --configs=overwrite
check_eq "--configs=overwrite sets the mode" "$CONFIG_MODE" "overwrite"
parse_args --configs keep v1.0.48
check_eq "--configs keep sets the mode" "$CONFIG_MODE" "keep"
check_eq "a bare argument is the version" "$VERSION_ARG" "v1.0.48"
parse_args --configs-only
check_eq "--configs-only is remembered" "$CONFIGS_ONLY" "1"
if (parse_args --configs=bogus) >/dev/null 2>&1; then
  fail "--configs=bogus is rejected" "the parser accepted it"
else
  ok "--configs=bogus is rejected"
fi
if (parse_args --nope) >/dev/null 2>&1; then
  fail "an unknown option is rejected" "the parser accepted it"
else
  ok "an unknown option is rejected"
fi

echo
echo "== diff summary =="
root="$(new_root)"
load_installer "$root"
printf 'a\nb\n' >"$root/current"
printf 'a\nc\n' >"$root/fresh"
check_eq "one changed line is reported" "$(config_diff_summary "$root/current" "$root/fresh")" "+1 -1"
printf 'a\nb\n' >"$root/fresh"
check_eq "identical files report no change" "$(config_diff_summary "$root/current" "$root/fresh")" "+0 -0"

echo
echo "== install.sh --configs-only =="
root="$(new_root)"
load_installer "$root"
prepare_package
printf '{}\n' >"$CONFIG_DIR/dns.json"
output="$(INSTALL_DIR="$INSTALL_DIR" CONFIG_DIR="$CONFIG_DIR" SERVICE_FILE="$SERVICE_FILE" \
  bash "$REPO_ROOT/install.sh" --configs-only --configs=overwrite 2>&1)"
status=$?
check_eq "the exit status is 0" "$status" "0"
case "$output" in
  *"Refreshing the files"*) ok "--configs-only refreshes the config dir" ;;
  *) fail "--configs-only refreshes the config dir" "$output" ;;
esac
cmp -s "$CONFIG_DIR/dns.json" "$REPO_ROOT/example/dns.json" &&
  ok "--configs-only replaced the modified file" ||
  fail "--configs-only replaced the modified file" "content differs"

if bash "$REPO_ROOT/install.sh" --help | grep -q -- '--configs=overwrite'; then
  ok "--help documents the config modes"
else
  fail "--help documents the config modes" "usage is missing"
fi

echo
if [[ "$failures" -eq 0 ]]; then
  echo "all installer tests passed"
  exit 0
fi
echo "$failures installer test(s) failed"
exit 1
