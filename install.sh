#!/usr/bin/env bash
set -euo pipefail

REPO_OWNER="yamatu"
REPO_NAME="yabx"
BIN_NAME="V2bX"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/V2bX}"
CONFIG_DIR="${CONFIG_DIR:-/etc/V2bX}"
SERVICE_FILE="${SERVICE_FILE:-/etc/systemd/system/V2bX.service}"
# Multi process mode: one process per node, started through this template unit.
INSTANCE_TEMPLATE_FILE="${INSTANCE_TEMPLATE_FILE:-/etc/systemd/system/v2bx@.service}"
NODES_DIR="${NODES_DIR:-${CONFIG_DIR}/nodes}"
INSTALL_MODE="${INSTALL_MODE:-release}"
VERSION="${VERSION:-}"
SOURCE_REF="${SOURCE_REF:-main}"

# Sidecar configs that are copied into CONFIG_DIR. They are never replaced
# silently: an update asks first unless --configs says otherwise. config.json is
# not in this list, it holds the node credentials and is only ever created.
CONFIG_TEMPLATES="dns.json route.json custom_outbound.json custom_inbound.json config_xhttp_reality.json config_naive.json xhttp_template.conf"
CONFIG_MODE="ask"
CONFIGS_ONLY="0"
VERSION_ARG=""

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

download_file() {
  local url="$1"
  local dst="$2"
  if has_cmd curl; then
    curl -fsSL "$url" -o "$dst"
    return $?
  fi
  if has_cmd wget; then
    wget -q -O "$dst" "$url"
    return $?
  fi
  return 1
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

  build_tags="${V2BX_BUILD_TAGS:-xray sing with_reality_server with_quic with_grpc with_utls with_wireguard with_acme}"
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

  if [[ -d "$src_dir/example" ]]; then
    for file in config.json dns.json route.json custom_outbound.json custom_inbound.json config_xhttp_reality.json config_naive.json geoip.dat geosite.dat; do
      if [[ -f "$src_dir/example/$file" ]]; then
        cp -f "$src_dir/example/$file" "$INSTALL_DIR/$file"
      fi
    done
  fi
  if [[ -f "$src_dir/V2bX.sh" ]]; then
    cp -f "$src_dir/V2bX.sh" "$INSTALL_DIR/V2bX.sh"
  fi
  if [[ -f "$src_dir/initconfig.sh" ]]; then
    cp -f "$src_dir/initconfig.sh" "$INSTALL_DIR/initconfig.sh"
  fi
  if [[ -f "$src_dir/acme_cf.sh" ]]; then
    cp -f "$src_dir/acme_cf.sh" "$INSTALL_DIR/acme_cf.sh"
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

write_default_xhttp_template() {
  local dst="$1"
  cat > "$dst" <<'EOF'
{
  "host": "example.com",
  "path": "/yourpath",
  "mode": "auto",
  "extra": {
    "headers": {
      "User-Agent": "Mozilla/5.0"
    },
    "xPaddingBytes": "100-1000",
    "noGRPCHeader": false,
    "noSSEHeader": false,
    "scMaxEachPostBytes": 1000000,
    "scMinPostsIntervalMs": 30,
    "scMaxBufferedPosts": 30,
    "xmux": {
      "maxConcurrency": "8-16",
      "maxConnections": 0,
      "cMaxReuseTimes": 0,
      "cMaxLifetimeMs": 0,
      "hMaxRequestTimes": "600-900",
      "hKeepAlivePeriod": 0
    },
    "downloadSettings": {
      "address": "example.com",
      "port": 443,
      "network": "xhttp",
      "security": "tls",
      "tlsSettings": {
        "serverName": "example.com"
      },
      "xhttpSettings": {
        "path": "/yourpath"
      },
      "sockopt": {
        "mark": 0
      }
    }
  }
}
EOF
}

ensure_example_asset() {
  local ref="$1"
  local file="$2"

  if [[ -f "$INSTALL_DIR/$file" ]]; then
    return 0
  fi

  if ! download_file "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/${ref}/example/${file}" "$INSTALL_DIR/$file"; then
    rm -f "$INSTALL_DIR/$file"
    download_file "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/example/${file}" "$INSTALL_DIR/$file" >/dev/null 2>&1 || true
  fi
}

# ---- sidecar configs -------------------------------------------------------

print_usage() {
  cat <<'EOF'
V2bX installer

Usage: install.sh [version] [options]

Options:
  --configs=keep       never replace an existing sidecar config file
  --configs=ask        ask before replacing files that differ (default)
  --configs=overwrite  replace every file that differs, keeping a backup
  --configs-only       only refresh the files in /etc/V2bX, do not touch the binary
  -h, --help           show this help
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --configs=*)
        CONFIG_MODE="${1#*=}"
        ;;
      --configs)
        shift
        CONFIG_MODE="${1:-ask}"
        ;;
      --configs-only)
        CONFIGS_ONLY="1"
        ;;
      -h|--help)
        print_usage
        exit 0
        ;;
      -*)
        log_error "Unknown option: $1"
        print_usage
        exit 1
        ;;
      *)
        VERSION_ARG="$1"
        ;;
    esac
    shift
  done

  case "$CONFIG_MODE" in
    keep|ask|overwrite) ;;
    *)
      log_error "Invalid --configs value: $CONFIG_MODE (expected keep, ask or overwrite)"
      exit 1
      ;;
  esac
}

# Diff summary of the installed file against the template, e.g. "+2 -1".
config_diff_summary() {
  local current="$1"
  local fresh="$2"
  local added removed

  added="$(diff "$current" "$fresh" 2>/dev/null | grep -c '^>' || true)"
  removed="$(diff "$current" "$fresh" 2>/dev/null | grep -c '^<' || true)"
  printf "+%s -%s" "${added:-0}" "${removed:-0}"
}

# Echo the path of the template shipped by this version. The release package is
# unpacked into INSTALL_DIR, everything else is fetched like install_assets does.
fresh_template_path() {
  local ref="$1"
  local file="$2"
  local workdir="$3"

  if [[ -f "$INSTALL_DIR/$file" ]]; then
    printf '%s' "$INSTALL_DIR/$file"
    return 0
  fi

  if [[ "$file" == "xhttp_template.conf" ]]; then
    write_default_xhttp_template "$workdir/$file"
    printf '%s' "$workdir/$file"
    return 0
  fi

  mkdir -p "$INSTALL_DIR"
  ensure_example_asset "$ref" "$file" >/dev/null 2>&1 || true
  if [[ -f "$INSTALL_DIR/$file" ]]; then
    printf '%s' "$INSTALL_DIR/$file"
    return 0
  fi

  return 1
}

# Read an answer from the terminal only. When the script is piped (curl | bash)
# stdin holds the script itself, so reading from it would swallow the rest of
# the installation.
ask_choice() {
  local prompt="$1"
  local fallback="$2"
  local answer=""

  if [[ ! -t 0 ]]; then
    printf '%s' "$fallback"
    return 0
  fi

  read -r -p "$prompt" answer </dev/tty || answer=""
  if [[ -z "$answer" ]]; then
    printf '%s' "$fallback"
  else
    printf '%s' "$answer"
  fi
}

confirm_overwrite() {
  local file="$1"
  local answer

  answer="$(ask_choice "Overwrite $file with the new template? [y/N]: " "n")"
  case "$answer" in
    y|Y|yes|YES|Yes) return 0 ;;
    *) return 1 ;;
  esac
}

# Copy the template over the installed file, keeping the old file in backup_dir.
replace_config_file() {
  local ref="$1"
  local workdir="$2"
  local file="$3"
  local backup_dir="$4"
  local fresh

  fresh="$(fresh_template_path "$ref" "$file" "$workdir")" || return 1

  mkdir -p "$backup_dir"
  cp -f "$CONFIG_DIR/$file" "$backup_dir/$file"
  cp -f "$fresh" "$CONFIG_DIR/$file"
  log_info "Replaced $CONFIG_DIR/$file (previous file saved in $backup_dir)"
}

sync_config_templates() {
  local ref="${1:-main}"
  local mode="${2:-ask}"
  local workdir file fresh choice backup_dir=""
  # Space separated lists: every managed file name is a single word, and plain
  # strings avoid the empty array pitfalls of older bash versions.
  local missing=""
  local differ=""
  local replaced="0"

  mkdir -p "$CONFIG_DIR"
  workdir="$(mktemp -d /tmp/v2bx-config.XXXXXX)"

  for file in $CONFIG_TEMPLATES; do
    fresh="$(fresh_template_path "$ref" "$file" "$workdir")" || continue
    if [[ ! -f "$CONFIG_DIR/$file" ]]; then
      missing="$missing $file"
    elif ! cmp -s "$CONFIG_DIR/$file" "$fresh"; then
      differ="$differ $file"
    fi
  done

  # A file that is not installed yet cannot conflict with anything.
  for file in $missing; do
    fresh="$(fresh_template_path "$ref" "$file" "$workdir")" || continue
    cp -f "$fresh" "$CONFIG_DIR/$file"
    log_info "Created $CONFIG_DIR/$file"
  done

  if [[ -z "${differ// /}" ]]; then
    rm -rf "$workdir"
    return 0
  fi

  log_warn "These files in $CONFIG_DIR differ from the templates of this release:"
  for file in $differ; do
    fresh="$(fresh_template_path "$ref" "$file" "$workdir")" || continue
    printf "  %-26s %s\n" "$file" "$(config_diff_summary "$CONFIG_DIR/$file" "$fresh")"
  done
  log_info "config.json is never replaced: it holds the node credentials."

  case "$mode" in
    keep)
      log_info "Keeping the existing files (--configs=keep)"
      ;;
    overwrite)
      backup_dir="$CONFIG_DIR/backup-$(date +%Y%m%d-%H%M%S)"
      for file in $differ; do
        replace_config_file "$ref" "$workdir" "$file" "$backup_dir" && replaced=$((replaced + 1))
      done
      ;;
    *)
      if [[ ! -t 0 ]]; then
        log_warn "Not running interactively, keeping the existing files (use --configs=overwrite to replace them)"
      else
        choice="$(ask_choice "Replace them with the new templates?
  [1] keep the existing files (default)
  [2] replace every file above (a backup is kept)
  [3] decide per file
Select [1]: " "1")"
        case "$choice" in
          2)
            backup_dir="$CONFIG_DIR/backup-$(date +%Y%m%d-%H%M%S)"
            for file in $differ; do
              replace_config_file "$ref" "$workdir" "$file" "$backup_dir" && replaced=$((replaced + 1))
            done
            ;;
          3)
            backup_dir="$CONFIG_DIR/backup-$(date +%Y%m%d-%H%M%S)"
            for file in $differ; do
              if confirm_overwrite "$file"; then
                replace_config_file "$ref" "$workdir" "$file" "$backup_dir" && replaced=$((replaced + 1))
              fi
            done
            ;;
          *)
            log_info "Keeping the existing files"
            ;;
        esac
      fi
      ;;
  esac

  if [[ "$replaced" -gt 0 ]]; then
    log_info "Replaced $replaced file(s), review the differences before restarting the service"
  fi

  rm -rf "$workdir"
}

install_assets() {
  local ref="${1:-main}"
  mkdir -p "$CONFIG_DIR"

  ensure_example_asset "$ref" "config_xhttp_reality.json"
  ensure_example_asset "$ref" "config_naive.json"

  if [[ -f "$INSTALL_DIR/geoip.dat" ]]; then
    cp -f "$INSTALL_DIR/geoip.dat" "$CONFIG_DIR/geoip.dat"
  fi
  if [[ -f "$INSTALL_DIR/geosite.dat" ]]; then
    cp -f "$INSTALL_DIR/geosite.dat" "$CONFIG_DIR/geosite.dat"
  fi

  # config.json holds the node credentials: create it, never replace it.
  copy_if_missing "$INSTALL_DIR/config.json" "$CONFIG_DIR/config.json"

  sync_config_templates "$ref" "$CONFIG_MODE"
}

install_manager_scripts() {
  local ref="$1"
  local menu_target="/usr/bin/v2bx"
  local core_cmd_target="/usr/bin/V2bX"
  local core_bin_link="/usr/bin/v2bx-bin"
  local helper_target="$INSTALL_DIR/initconfig.sh"
  local acme_target="$INSTALL_DIR/acme_cf.sh"
  local menu_source="$INSTALL_DIR/V2bX.sh"
  local helper_source="$INSTALL_DIR/initconfig.sh"
  local acme_source="$INSTALL_DIR/acme_cf.sh"

  # Clean old symlinks/files first to avoid cp following symlink
  # and accidentally overwriting /usr/local/V2bX/V2bX.
  rm -f "$menu_target" "$core_cmd_target" "$core_bin_link"

  if [[ -f "$menu_source" ]]; then
    cp -f "$menu_source" "$menu_target"
  else
    if ! download_file "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/${ref}/V2bX.sh" "$menu_target"; then
      download_file "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/V2bX.sh" "$menu_target"
    fi
  fi

  if [[ -f "$helper_source" ]]; then
    chmod +x "$helper_source"
  else
    if ! download_file "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/${ref}/initconfig.sh" "$helper_target"; then
      download_file "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/initconfig.sh" "$helper_target"
    fi
  fi

  if [[ -f "$acme_source" ]]; then
    chmod +x "$acme_source"
  else
    if ! download_file "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/${ref}/acme_cf.sh" "$acme_target"; then
      download_file "https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/acme_cf.sh" "$acme_target"
    fi
  fi

  chmod +x "$menu_target"
  chmod +x "$helper_target"
  chmod +x "$acme_target"
  ln -s "$INSTALL_DIR/$BIN_NAME" "$core_cmd_target"
  ln -s "$INSTALL_DIR/$BIN_NAME" "$core_bin_link"
}

install_service() {
  if ! command -v systemctl >/dev/null 2>&1; then
    log_error "systemd is required. OpenRC-only systems are not supported by this script."
    exit 1
  fi

  cat > "$SERVICE_FILE" <<'EOF'
[Unit]
Description=V2bX Service
Documentation=https://github.com/yamatu/yabx
After=network-online.target nss-lookup.target
Wants=network-online.target
# A permanently broken config must not be restarted forever.
StartLimitIntervalSec=60
StartLimitBurst=5

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

  # Multi process mode. The template is only installed, never enabled: the
  # migration is an explicit choice (menu item 16 / v2bx multi), and "V2bX split"
  # writes /etc/V2bX/nodes/*.json. Keeping both units means switching back and
  # forth costs one systemctl call and no reinstall.
  mkdir -p "$NODES_DIR"
  cat > "$INSTANCE_TEMPLATE_FILE" <<'EOF'
[Unit]
Description=V2bX node %i
Documentation=https://github.com/yamatu/yabx
After=network-online.target nss-lookup.target
Wants=network-online.target
# One process per node: a node that cannot start must never stop the other
# nodes, so an instance is restarted forever instead of being given up on.
StartLimitIntervalSec=0

[Service]
Type=simple
User=root
WorkingDirectory=/usr/local/V2bX
ExecStart=/usr/local/V2bX/V2bX server -c /etc/V2bX/nodes/%i.json
Restart=on-failure
RestartSec=5s
LimitNOFILE=51200

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable V2bX >/dev/null 2>&1 || true
}

# The instance names of the multi process configs in NODES_DIR.
installed_instance_names() {
  local dir="$1"
  local file name
  [[ -d "$dir" ]] || return 0
  for file in "$dir"/*.json; do
    [[ -e "$file" ]] || continue
    name="${file##*/}"
    printf '%s\n' "${name%.json}"
  done
}

main() {
  parse_args "$@"

  # --configs-only refreshes the files in CONFIG_DIR and nothing else, which is
  # what an existing installation needs when only the templates changed.
  if [[ "$CONFIGS_ONLY" == "1" ]]; then
    log_info "Refreshing the files in $CONFIG_DIR"
    sync_config_templates "$SOURCE_REF" "$CONFIG_MODE"
    log_info "Done"
    return 0
  fi

  require_root
  detect_release
  detect_asset_arch
  install_base
  resolve_version "$VERSION_ARG"

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

  local script_ref="main"
  if [[ "$INSTALL_MODE" == "release" && -n "$VERSION" ]]; then
    script_ref="$VERSION"
  elif [[ "$INSTALL_MODE" == "source" && -n "$SOURCE_REF" ]]; then
    script_ref="$SOURCE_REF"
  fi

  install_assets "$script_ref"
  install_manager_scripts "$script_ref"
  install_service

  # Multi process mode: restart every instance that is running, and never start
  # the single process unit. Starting V2bX as well would run the same nodes
  # twice and fail on the ports that are already bound.
  local instances=()
  local name
  while IFS= read -r name; do
    [[ -n "$name" ]] && instances+=("$name")
  done < <(installed_instance_names "$NODES_DIR")

  if [[ ${#instances[@]} -gt 0 ]]; then
    local restarted="0"
    for name in "${instances[@]}"; do
      if ! systemctl is-active --quiet "v2bx@${name}"; then
        continue
      fi
      if systemctl restart "v2bx@${name}"; then
        restarted="1"
        log_info "v2bx@${name} restarted"
      else
        log_warn "v2bx@${name} restart failed, run: journalctl -u v2bx@${name} -e --no-pager"
      fi
    done
    if [[ "$restarted" == "0" ]]; then
      log_info "Multi process mode: ${#instances[@]} instance(s) configured, none was running"
      log_info "Start them with: systemctl enable --now v2bx@<node>"
    fi
  elif [[ "$had_config" == "1" ]]; then
    if systemctl restart V2bX; then
      log_info "V2bX restarted successfully"
    else
      log_warn "V2bX restart failed, run: journalctl -u V2bX -e --no-pager"
    fi
  else
    log_warn "First install detected. Edit /etc/V2bX/config.json before starting service."
    log_warn "Examples: /etc/V2bX/config_xhttp_reality.json /etc/V2bX/config_naive.json /etc/V2bX/xhttp_template.conf"
    log_info "Start command: systemctl start V2bX"
  fi

  log_info "Done. Run 'v2bx' to open interactive menu"
  log_info "Direct binary command: /usr/local/V2bX/V2bX"
}

# Only run when executed, so the functions above can be sourced by tests.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
