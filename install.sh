#!/usr/bin/env bash
set -euo pipefail

REPO_OWNER="yamatu"
REPO_NAME="yav2bx"
BIN_NAME="V2bX"
INSTALL_DIR="/usr/local/V2bX"
CONFIG_DIR="/etc/V2bX"
SERVICE_FILE="/etc/systemd/system/V2bX.service"
INSTALL_MODE="release"
VERSION=""
SOURCE_REF="main"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
PLAIN='\033[0m'

log_info() {
  printf "%b[INFO]%b %s\n" "$GREEN" "$PLAIN" "$1"
}

log_warn() {
  printf "%b[WARN]%b %s\n" "$YELLOW" "$PLAIN" "$1"
}

log_error() {
  printf "%b[ERROR]%b %s\n" "$RED" "$PLAIN" "$1"
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

require_root() {
  if [[ ${EUID:-0} -ne 0 ]]; then
    log_error "Please run this script as root"
    exit 1
  fi
}

detect_release() {
  if [[ -f /etc/os-release ]]; then
    . /etc/os-release
    local id_like
    id_like="${ID_LIKE:-}"
    case "${ID:-}" in
      debian|ubuntu)
        RELEASE="debian"
        ;;
      centos|rhel|rocky|almalinux|ol|fedora)
        RELEASE="centos"
        ;;
      arch)
        RELEASE="arch"
        ;;
      alpine)
        RELEASE="alpine"
        ;;
      *)
        case "$id_like" in
          *debian*) RELEASE="debian" ;;
          *rhel*|*fedora*) RELEASE="centos" ;;
          *arch*) RELEASE="arch" ;;
          *) RELEASE="unknown" ;;
        esac
        ;;
    esac
  else
    RELEASE="unknown"
  fi

  if [[ "$RELEASE" == "unknown" ]]; then
    log_error "Unsupported Linux distribution"
    exit 1
  fi
}

install_base() {
  if [[ "${V2BX_SKIP_BASE_INSTALL:-0}" == "1" ]]; then
    log_warn "Skip base package install (V2BX_SKIP_BASE_INSTALL=1)"
    return
  fi

  local -a missing_pkgs
  missing_pkgs=()

  case "$RELEASE" in
    debian)
      has_cmd curl || missing_pkgs+=(curl)
      has_cmd wget || missing_pkgs+=(wget)
      has_cmd unzip || missing_pkgs+=(unzip)
      has_cmd tar || missing_pkgs+=(tar)
      [[ -f /etc/ssl/certs/ca-certificates.crt ]] || missing_pkgs+=(ca-certificates)

      if [[ ${#missing_pkgs[@]} -eq 0 ]]; then
        log_info "Base dependencies already present, skip apt install"
        return
      fi

      apt-get update -y
      DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing_pkgs[@]}"
      update-ca-certificates || true
      ;;
    centos)
      has_cmd curl || missing_pkgs+=(curl)
      has_cmd wget || missing_pkgs+=(wget)
      has_cmd unzip || missing_pkgs+=(unzip)
      has_cmd tar || missing_pkgs+=(tar)

      if [[ ${#missing_pkgs[@]} -eq 0 ]]; then
        log_info "Base dependencies already present, skip yum/dnf install"
        return
      fi

      if command -v dnf >/dev/null 2>&1; then
        dnf install -y "${missing_pkgs[@]}" ca-certificates
      else
        yum install -y epel-release || true
        yum install -y "${missing_pkgs[@]}" ca-certificates
      fi
      update-ca-trust force-enable || true
      ;;
    alpine)
      has_cmd curl || missing_pkgs+=(curl)
      has_cmd wget || missing_pkgs+=(wget)
      has_cmd unzip || missing_pkgs+=(unzip)
      has_cmd tar || missing_pkgs+=(tar)

      if [[ ${#missing_pkgs[@]} -eq 0 ]]; then
        log_info "Base dependencies already present, skip apk install"
        return
      fi

      apk add --no-cache "${missing_pkgs[@]}" ca-certificates
      update-ca-certificates || true
      ;;
    arch)
      has_cmd curl || missing_pkgs+=(curl)
      has_cmd wget || missing_pkgs+=(wget)
      has_cmd unzip || missing_pkgs+=(unzip)
      has_cmd tar || missing_pkgs+=(tar)

      if [[ ${#missing_pkgs[@]} -eq 0 ]]; then
        log_info "Base dependencies already present, skip pacman install"
        return
      fi

      pacman -Sy --noconfirm --needed "${missing_pkgs[@]}" ca-certificates
      ;;
  esac
}

detect_asset_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|x64|amd64)
      ASSET_ARCH="linux-64"
      ;;
    i386|i686)
      ASSET_ARCH="linux-32"
      ;;
    aarch64|arm64)
      ASSET_ARCH="linux-arm64-v8a"
      ;;
    armv7l|armv7)
      ASSET_ARCH="linux-arm32-v7a"
      ;;
    armv6l|armv6)
      ASSET_ARCH="linux-arm32-v6"
      ;;
    armv5l|armv5)
      ASSET_ARCH="linux-arm32-v5"
      ;;
    s390x)
      ASSET_ARCH="linux-s390x"
      ;;
    mips64le)
      ASSET_ARCH="linux-mips64le"
      ;;
    mips64)
      ASSET_ARCH="linux-mips64"
      ;;
    mipsle)
      ASSET_ARCH="linux-mips32le"
      ;;
    mips)
      ASSET_ARCH="linux-mips32"
      ;;
    ppc64le)
      ASSET_ARCH="linux-ppc64le"
      ;;
    ppc64)
      ASSET_ARCH="linux-ppc64"
      ;;
    riscv64)
      ASSET_ARCH="linux-riscv64"
      ;;
    *)
      log_error "Unsupported architecture: $arch"
      exit 1
      ;;
  esac
}

resolve_version() {
  SOURCE_REF="${1:-main}"
  if [[ $# -gt 0 && -n "${1:-}" ]]; then
    VERSION="$1"
    INSTALL_MODE="release"
    return
  fi

  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest" 2>/dev/null | sed -n 's/.*"tag_name": "\([^"]*\)".*/\1/p' | head -n 1 || true)"
  if [[ -z "$VERSION" ]]; then
    INSTALL_MODE="source"
    log_warn "No GitHub release found. Fallback to source build from main branch."
    return
  fi
  INSTALL_MODE="release"
}

install_binary() {
  local version="$1"
  local zip_file
  local download_url

  zip_file="$(mktemp /tmp/v2bx.XXXXXX.zip)"
  download_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${version}/${BIN_NAME}-${ASSET_ARCH}.zip"

  log_info "Downloading ${download_url}"
  if ! curl -fL --retry 3 --connect-timeout 15 -o "$zip_file" "$download_url"; then
    rm -f "$zip_file"
    log_warn "Release asset not found or download failed: ${download_url}"
    return 1
  fi

  rm -rf "$INSTALL_DIR"
  mkdir -p "$INSTALL_DIR"
  unzip -q -o "$zip_file" -d "$INSTALL_DIR"
  rm -f "$zip_file"

  chmod +x "$INSTALL_DIR/$BIN_NAME"
  ln -sf "$INSTALL_DIR/$BIN_NAME" /usr/bin/V2bX
  ln -sf /usr/bin/V2bX /usr/bin/v2bx
}

install_build_tools() {
  if [[ "${V2BX_SKIP_BUILD_DEPS:-0}" == "1" ]]; then
    log_warn "Skip build dependencies install (V2BX_SKIP_BUILD_DEPS=1)"
    return
  fi

  local -a missing_build
  missing_build=()
  has_cmd git || missing_build+=(git)
  has_cmd go || missing_build+=(golang)

  if [[ ${#missing_build[@]} -eq 0 ]]; then
    log_info "Build dependencies already present"
    return
  fi

  case "$RELEASE" in
    debian)
      DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing_build[@]}"
      ;;
    centos)
      if command -v dnf >/dev/null 2>&1; then
        dnf install -y "${missing_build[@]}"
      else
        yum install -y "${missing_build[@]}"
      fi
      ;;
    alpine)
      for i in "${!missing_build[@]}"; do
        if [[ "${missing_build[$i]}" == "golang" ]]; then
          missing_build[$i]="go"
        fi
      done
      apk add --no-cache "${missing_build[@]}"
      ;;
    arch)
      for i in "${!missing_build[@]}"; do
        if [[ "${missing_build[$i]}" == "golang" ]]; then
          missing_build[$i]="go"
        fi
      done
      pacman -Sy --noconfirm --needed "${missing_build[@]}"
      ;;
  esac
}

install_from_source() {
  local ref="$1"
  local src_dir
  local build_tags

  build_tags="${V2BX_BUILD_TAGS:-xray with_reality_server with_quic with_grpc with_utls with_wireguard with_acme}"
  src_dir="$(mktemp -d /tmp/v2bx-src.XXXXXX)"

  log_info "Installing from source ref: ${ref}"
  install_build_tools

  if ! git clone --depth 1 "https://github.com/${REPO_OWNER}/${REPO_NAME}.git" "$src_dir"; then
    rm -rf "$src_dir"
    log_error "Failed to clone source repository"
    exit 1
  fi

  rm -rf "$INSTALL_DIR"
  mkdir -p "$INSTALL_DIR"

  (
    cd "$src_dir"
    if [[ "$ref" != "main" ]]; then
      git fetch --depth 1 origin "$ref" >/dev/null 2>&1 || true
      if ! git checkout "$ref" >/dev/null 2>&1; then
        log_warn "Cannot checkout '${ref}', continue with main branch"
      fi
    fi
    export CGO_ENABLED=0
    go mod download
    go build -v -o "$INSTALL_DIR/$BIN_NAME" -tags "$build_tags" -trimpath -ldflags "-s -w -buildid="
  )

  chmod +x "$INSTALL_DIR/$BIN_NAME"
  ln -sf "$INSTALL_DIR/$BIN_NAME" /usr/bin/V2bX
  ln -sf /usr/bin/V2bX /usr/bin/v2bx

  if [[ -d "$src_dir/example" ]]; then
    for file in config.json dns.json route.json custom_outbound.json custom_inbound.json config_xhttp_reality.json geoip.dat geosite.dat; do
      if [[ -f "$src_dir/example/$file" ]]; then
        cp -f "$src_dir/example/$file" "$INSTALL_DIR/$file"
      fi
    done
  fi
  if [[ -f "$src_dir/xhttp配置模板.conf" ]]; then
    cp -f "$src_dir/xhttp配置模板.conf" "$INSTALL_DIR/xhttp配置模板.conf"
  fi

  rm -rf "$src_dir"
}

copy_if_missing() {
  local src="$1"
  local dst="$2"
  if [[ -f "$src" && ! -f "$dst" ]]; then
    cp -f "$src" "$dst"
  fi
}

install_assets() {
  mkdir -p "$CONFIG_DIR"

  if [[ -f "$INSTALL_DIR/geoip.dat" ]]; then
    cp -f "$INSTALL_DIR/geoip.dat" "$CONFIG_DIR/geoip.dat"
  fi
  if [[ -f "$INSTALL_DIR/geosite.dat" ]]; then
    cp -f "$INSTALL_DIR/geosite.dat" "$CONFIG_DIR/geosite.dat"
  fi

  copy_if_missing "$INSTALL_DIR/config.json" "$CONFIG_DIR/config.json"
  copy_if_missing "$INSTALL_DIR/dns.json" "$CONFIG_DIR/dns.json"
  copy_if_missing "$INSTALL_DIR/route.json" "$CONFIG_DIR/route.json"
  copy_if_missing "$INSTALL_DIR/custom_outbound.json" "$CONFIG_DIR/custom_outbound.json"
  copy_if_missing "$INSTALL_DIR/custom_inbound.json" "$CONFIG_DIR/custom_inbound.json"
  copy_if_missing "$INSTALL_DIR/config_xhttp_reality.json" "$CONFIG_DIR/config_xhttp_reality.json"

  if [[ ! -f "$CONFIG_DIR/xhttp_template.conf" ]]; then
    if [[ -f "$INSTALL_DIR/xhttp配置模板.conf" ]]; then
      cp -f "$INSTALL_DIR/xhttp配置模板.conf" "$CONFIG_DIR/xhttp_template.conf"
    else
      curl -fsSL "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/xhttp%E9%85%8D%E7%BD%AE%E6%A8%A1%E6%9D%BF.conf" -o "$CONFIG_DIR/xhttp_template.conf" || true
    fi
  fi
}

install_service() {
  if ! command -v systemctl >/dev/null 2>&1; then
    log_error "systemd is required. OpenRC-only systems are not supported by this script."
    exit 1
  fi

  cat > "$SERVICE_FILE" <<'EOF'
[Unit]
Description=V2bX Service
Documentation=https://github.com/yamatu/yav2bx
After=network-online.target nss-lookup.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/usr/local/V2bX
ExecStart=/usr/local/V2bX/V2bX server -c /etc/V2bX/config.json
Restart=on-failure
RestartSec=5s
LimitNOFILE=51200

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable V2bX >/dev/null 2>&1 || true
}

main() {
  require_root
  detect_release
  detect_asset_arch
  install_base
  resolve_version "${1:-}"

  local had_config="0"
  if [[ -f "$CONFIG_DIR/config.json" ]]; then
    had_config="1"
  fi

  if [[ "$INSTALL_MODE" == "release" ]]; then
    log_info "Installing ${BIN_NAME} ${VERSION} (${ASSET_ARCH})"
    if ! install_binary "$VERSION"; then
      log_warn "Fallback to source build because release package is unavailable"
      install_from_source "$SOURCE_REF"
    fi
  else
    install_from_source "$SOURCE_REF"
  fi

  install_assets
  install_service

  if [[ "$had_config" == "1" ]]; then
    if systemctl restart V2bX; then
      log_info "V2bX restarted successfully"
    else
      log_warn "V2bX restart failed, run: journalctl -u V2bX -e --no-pager"
    fi
  else
    log_warn "First install detected. Edit /etc/V2bX/config.json before starting service."
    log_warn "XHTTP examples: /etc/V2bX/config_xhttp_reality.json and /etc/V2bX/xhttp_template.conf"
    log_info "Start command: systemctl start V2bX"
  fi

  log_info "Done. Management commands: V2bX start|stop|restart|log|update"
}

main "$@"
