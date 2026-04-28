#!/usr/bin/env bash
set -euo pipefail

CONFIG_DIR="${CONFIG_DIR:-/etc/V2bX}"
CONFIG_FILE="${V2BX_ACME_CONFIG:-${CONFIG_DIR}/acme_cf.env}"
ACME_HOME_DEFAULT="/root/.acme.sh"
CERT_FILE_DEFAULT="${CONFIG_DIR}/fullchain.cer"
KEY_FILE_DEFAULT="${CONFIG_DIR}/cert.key"
SERVICE_NAME_DEFAULT="V2bX"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
PLAIN='\033[0m'

info() {
  printf "%b[INFO]%b %s\n" "$GREEN" "$PLAIN" "$1"
}

warn() {
  printf "%b[WARN]%b %s\n" "$YELLOW" "$PLAIN" "$1"
}

error() {
  printf "%b[ERROR]%b %s\n" "$RED" "$PLAIN" "$1" >&2
}

usage() {
  cat <<'EOF'
V2bX Cloudflare DNS certificate helper

Usage:
  acme_cf.sh setup        Create /etc/V2bX/acme_cf.env and issue cert
  acme_cf.sh issue        Install acme.sh if needed, issue/install cert
  acme_cf.sh renew        Renew cert now and reinstall/restart on success
  acme_cf.sh status       Show config and certificate files
  acme_cf.sh edit         Edit /etc/V2bX/acme_cf.env

Config file:
  /etc/V2bX/acme_cf.env

Required values:
  CF_Token=your_cloudflare_api_token
  CF_Account_ID=your_cloudflare_account_id
  DOMAIN=domain.com
EOF
}

require_root() {
  if [[ ${EUID:-0} -ne 0 ]]; then
    error "Please run as root"
    exit 1
  fi
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

install_packages() {
  if [[ $# -eq 0 ]]; then
    return 0
  fi

  if has_cmd apt-get; then
    apt-get update -y
    DEBIAN_FRONTEND=noninteractive apt-get install -y "$@"
    return
  fi
  if has_cmd dnf; then
    dnf install -y "$@"
    return
  fi
  if has_cmd yum; then
    yum install -y "$@"
    return
  fi
  if has_cmd apk; then
    apk add --no-cache "$@"
    return
  fi
  if has_cmd pacman; then
    pacman -Sy --noconfirm --needed "$@"
    return
  fi

  warn "No supported package manager found. Missing packages: $*"
}

ensure_runtime_tools() {
  local -a pkgs
  pkgs=()

  if ! has_cmd curl && ! has_cmd wget; then
    pkgs+=(curl)
  fi
  if ! has_cmd openssl; then
    pkgs+=(openssl)
  fi
  if ! has_cmd crontab; then
    pkgs+=(cron)
  fi

  if [[ ${#pkgs[@]} -gt 0 ]]; then
    info "Installing required tools: ${pkgs[*]}"
    install_packages "${pkgs[@]}" || true
  fi
}

pick_editor() {
  if [[ -n "${EDITOR:-}" ]] && has_cmd "$EDITOR"; then
    echo "$EDITOR"
    return
  fi
  for editor in vim nvim vi nano; do
    if has_cmd "$editor"; then
      echo "$editor"
      return
    fi
  done
  echo ""
}

prompt_value() {
  local prompt="$1"
  local default_value="${2:-}"
  local secret="${3:-0}"
  local value

  if [[ "$secret" == "1" ]]; then
    if [[ -n "$default_value" ]]; then
      read -r -s -p "${prompt} [keep current]: " value
    else
      read -r -s -p "${prompt}: " value
    fi
    printf '\n' >&2
  else
    if [[ -n "$default_value" ]]; then
      read -r -p "${prompt} [${default_value}]: " value
    else
      read -r -p "${prompt}: " value
    fi
  fi

  if [[ -z "$value" ]]; then
    value="$default_value"
  fi
  printf '%s' "$value"
}

write_config() {
  mkdir -p "$CONFIG_DIR"

  local old_cf_token=""
  local old_cf_account_id=""
  local old_domain=""
  local old_acme_email=""
  local old_cert_file="$CERT_FILE_DEFAULT"
  local old_key_file="$KEY_FILE_DEFAULT"
  local old_acme_home="$ACME_HOME_DEFAULT"
  local old_service_name="$SERVICE_NAME_DEFAULT"

  if [[ -f "$CONFIG_FILE" ]]; then
    # shellcheck disable=SC1090
    source "$CONFIG_FILE"
    old_cf_token="${CF_Token:-}"
    old_cf_account_id="${CF_Account_ID:-}"
    old_domain="${DOMAIN:-}"
    old_acme_email="${ACME_EMAIL:-}"
    old_cert_file="${CERT_FILE:-$CERT_FILE_DEFAULT}"
    old_key_file="${KEY_FILE:-$KEY_FILE_DEFAULT}"
    old_acme_home="${ACME_HOME:-$ACME_HOME_DEFAULT}"
    old_service_name="${SERVICE_NAME:-$SERVICE_NAME_DEFAULT}"
  fi

  local cf_token
  local cf_account_id
  local domain
  local acme_email

  cf_token="$(prompt_value 'CF_Token' "$old_cf_token" 1)"
  cf_account_id="$(prompt_value 'CF_Account_ID' "$old_cf_account_id")"
  domain="$(prompt_value 'DOMAIN' "$old_domain")"
  acme_email="$(prompt_value 'ACME_EMAIL' "$old_acme_email")"

  if [[ -z "$cf_token" || -z "$cf_account_id" || -z "$domain" ]]; then
    error "CF_Token, CF_Account_ID and DOMAIN are required"
    return 1
  fi
  if [[ -z "$acme_email" ]]; then
    acme_email="admin@${domain}"
  fi

  umask 077
  cat > "$CONFIG_FILE" <<EOF
CF_Token='${cf_token}'
CF_Account_ID='${cf_account_id}'
DOMAIN='${domain}'
ACME_EMAIL='${acme_email}'
ACME_HOME='${old_acme_home}'
CERT_FILE='${old_cert_file}'
KEY_FILE='${old_key_file}'
SERVICE_NAME='${old_service_name}'
EOF
  chmod 600 "$CONFIG_FILE"
  info "Saved config: $CONFIG_FILE"
}

load_config() {
  if [[ ! -f "$CONFIG_FILE" ]]; then
    error "Missing config: $CONFIG_FILE"
    error "Run: v2bx acme setup"
    return 1
  fi

  # shellcheck disable=SC1090
  source "$CONFIG_FILE"

  : "${ACME_HOME:=$ACME_HOME_DEFAULT}"
  : "${CERT_FILE:=$CERT_FILE_DEFAULT}"
  : "${KEY_FILE:=$KEY_FILE_DEFAULT}"
  : "${SERVICE_NAME:=$SERVICE_NAME_DEFAULT}"
  : "${ACME_EMAIL:=admin@${DOMAIN:-example.com}}"

  if [[ -z "${CF_Token:-}" || -z "${CF_Account_ID:-}" || -z "${DOMAIN:-}" ]]; then
    error "CF_Token, CF_Account_ID and DOMAIN must be set in $CONFIG_FILE"
    return 1
  fi
}

acme_sh() {
  printf '%s/acme.sh' "$ACME_HOME"
}

download_to_shell() {
  local url="$1"
  shift

  if has_cmd curl; then
    curl -fsSL "$url" | sh -s "$@"
    return $?
  fi
  if has_cmd wget; then
    wget -qO- "$url" | sh -s "$@"
    return $?
  fi

  error "curl or wget is required to install acme.sh"
  return 1
}

ensure_acme() {
  ensure_runtime_tools

  local acme_bin
  acme_bin="$(acme_sh)"

  if [[ -x "$acme_bin" ]]; then
    "$acme_bin" --install-cronjob || true
    return 0
  fi

  info "acme.sh not found, installing to ${ACME_HOME}"
  mkdir -p "$ACME_HOME"
  download_to_shell "https://get.acme.sh" "email=${ACME_EMAIL}" "--home" "$ACME_HOME"

  if [[ ! -x "$acme_bin" ]]; then
    error "acme.sh install failed: $acme_bin"
    return 1
  fi

  "$acme_bin" --set-default-ca --server letsencrypt || true
  "$acme_bin" --install-cronjob || true
}

reload_cmd() {
  if has_cmd systemctl; then
    printf 'systemctl restart %s.service' "$SERVICE_NAME"
  else
    printf 'service %s restart' "$SERVICE_NAME"
  fi
}

install_cert() {
  local acme_bin
  local reload
  acme_bin="$(acme_sh)"
  reload="$(reload_cmd)"

  mkdir -p "$(dirname "$CERT_FILE")" "$(dirname "$KEY_FILE")"

  "$acme_bin" --install-cert -d "$DOMAIN" \
    --fullchain-file "$CERT_FILE" \
    --key-file "$KEY_FILE" \
    --reloadcmd "$reload"

  chmod 600 "$KEY_FILE" || true
  chmod 644 "$CERT_FILE" || true
  info "Certificate installed: $CERT_FILE"
  info "Private key installed: $KEY_FILE"
}

issue_cert() {
  load_config
  ensure_acme

  export CF_Token
  export CF_Account_ID

  local acme_bin
  acme_bin="$(acme_sh)"

  info "Issuing certificate for ${DOMAIN} by Cloudflare DNS"
  "$acme_bin" --issue --dns dns_cf -d "$DOMAIN"
  install_cert
}

renew_cert() {
  load_config
  ensure_acme

  export CF_Token
  export CF_Account_ID

  local acme_bin
  acme_bin="$(acme_sh)"

  info "Renewing certificate for ${DOMAIN}"
  "$acme_bin" --renew -d "$DOMAIN" || {
    warn "Renew command did not complete. If the cert is not due, acme.sh may skip renewal."
    return 1
  }
  install_cert
}

show_status() {
  load_config

  local acme_bin
  acme_bin="$(acme_sh)"

  cat <<EOF
Config:      $CONFIG_FILE
Domain:      $DOMAIN
ACME home:   $ACME_HOME
acme.sh:     $acme_bin
Cert file:   $CERT_FILE
Key file:    $KEY_FILE
Reload cmd:  $(reload_cmd)
EOF

  if [[ -f "$CERT_FILE" ]]; then
    if has_cmd openssl; then
      openssl x509 -in "$CERT_FILE" -noout -subject -issuer -dates || true
    else
      info "Certificate file exists"
    fi
  else
    warn "Certificate file does not exist"
  fi
}

edit_config() {
  mkdir -p "$CONFIG_DIR"
  if [[ ! -f "$CONFIG_FILE" ]]; then
    write_config
    return
  fi

  local editor
  editor="$(pick_editor)"
  if [[ -z "$editor" ]]; then
    error "No editor found. Install vim/nano or set EDITOR."
    return 1
  fi
  "$editor" "$CONFIG_FILE"
}

setup() {
  write_config
  issue_cert
}

main() {
  require_root

  case "${1:-setup}" in
    setup) setup ;;
    issue) issue_cert ;;
    renew) renew_cert ;;
    status) show_status ;;
    edit) edit_config ;;
    help|-h|--help) usage ;;
    *)
      usage
      return 1
      ;;
  esac
}

main "$@"
