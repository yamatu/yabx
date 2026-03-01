#!/usr/bin/env bash
set -u

CONFIG_DIR="${CONFIG_DIR:-/etc/V2bX}"
CONFIG_FILE="${CONFIG_DIR}/config.json"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/V2bX}"
SERVICE_NAME="${SERVICE_NAME:-V2bX}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
PLAIN='\033[0m'

wizard_info() {
  printf "%b[INFO]%b %s\n" "$GREEN" "$PLAIN" "$1"
}

wizard_warn() {
  printf "%b[WARN]%b %s\n" "$YELLOW" "$PLAIN" "$1" >&2
}

wizard_error() {
  printf "%b[ERROR]%b %s\n" "$RED" "$PLAIN" "$1" >&2
}

prompt_non_empty() {
  local prompt="$1"
  local value
  while true; do
    read -r -p "$prompt" value
    if [[ -n "$value" ]]; then
      printf '%s' "$value"
      return
    fi
    wizard_warn "输入不能为空"
  done
}

prompt_yes_no() {
  local prompt="$1"
  local default_yes="$2"
  local value
  if [[ "$default_yes" == "1" ]]; then
    read -r -p "$prompt [Y/n]: " value
    [[ -z "$value" || "$value" =~ ^[Yy]$ ]] && return 0
    return 1
  fi
  read -r -p "$prompt [y/N]: " value
  [[ "$value" =~ ^[Yy]$ ]] && return 0
  return 1
}

prompt_node_type() {
  local n
  echo "请选择节点类型:" >&2
  echo "1) vless + xhttp (推荐)" >&2
  echo "2) vmess" >&2
  echo "3) trojan" >&2
  echo "4) shadowsocks" >&2
  echo "5) vless (手动模式)" >&2
  while true; do
    read -r -p "输入 [1-5]: " n
    case "$n" in
      1) printf 'vless_xhttp'; return ;;
      2) printf 'vmess'; return ;;
      3) printf 'trojan'; return ;;
      4) printf 'shadowsocks'; return ;;
      5) printf 'vless'; return ;;
      *) wizard_warn "请输入 1-5" ;;
    esac
  done
}

prompt_xhttp_security_mode() {
  local n
  echo "xhttp 安全模式:" >&2
  echo "1) reality (默认，推荐)" >&2
  echo "2) tls" >&2
  while true; do
    read -r -p "输入 [1-2，默认1]: " n
    case "$n" in
      ""|1) printf 'reality'; return ;;
      2) printf 'tls'; return ;;
      *) wizard_warn "请输入 1 或 2" ;;
    esac
  done
}

prompt_vless_security_mode() {
  local n
  echo "vless 安全模式:" >&2
  echo "1) reality" >&2
  echo "2) tls" >&2
  echo "3) none" >&2
  while true; do
    read -r -p "输入 [1-3，默认3]: " n
    case "$n" in
      1) printf 'reality'; return ;;
      2) printf 'tls'; return ;;
      ""|3) printf 'none'; return ;;
      *) wizard_warn "请输入 1-3" ;;
    esac
  done
}

prompt_cert_mode() {
  local n
  echo "请选择证书模式:" >&2
  echo "1) none" >&2
  echo "2) http" >&2
  echo "3) dns" >&2
  echo "4) file" >&2
  while true; do
    read -r -p "输入 [1-4]: " n
    case "$n" in
      1) printf 'none'; return ;;
      2) printf 'http'; return ;;
      3) printf 'dns'; return ;;
      4) printf 'file'; return ;;
      *) wizard_warn "请输入 1-4" ;;
    esac
  done
}

ensure_sidecar_files() {
  mkdir -p "$CONFIG_DIR"
  for file in dns.json route.json custom_outbound.json custom_inbound.json config_xhttp_reality.json; do
    if [[ ! -f "$CONFIG_DIR/$file" && -f "$INSTALL_DIR/$file" ]]; then
      cp -f "$INSTALL_DIR/$file" "$CONFIG_DIR/$file"
    fi
  done
  if [[ ! -f "$CONFIG_DIR/xhttp_template.conf" ]]; then
    if [[ -f "$INSTALL_DIR/xhttp_template.conf" ]]; then
      cp -f "$INSTALL_DIR/xhttp_template.conf" "$CONFIG_DIR/xhttp_template.conf"
    elif [[ -f "$INSTALL_DIR/xhttp配置模板.conf" ]]; then
      cp -f "$INSTALL_DIR/xhttp配置模板.conf" "$CONFIG_DIR/xhttp_template.conf"
    fi
  fi
}

restart_service_if_needed() {
  if ! command -v systemctl >/dev/null 2>&1; then
    wizard_warn "未检测到 systemd，请手动启动 V2bX"
    return
  fi
  if prompt_yes_no "是否立即重启 V2bX 服务" 1; then
    if systemctl restart "$SERVICE_NAME"; then
      wizard_info "V2bX 重启成功"
    else
      wizard_warn "V2bX 重启失败，请执行: journalctl -u V2bX -e --no-pager"
    fi
  fi
}

generate_config_file() {
  local api_host
  local api_key
  local fixed_api="0"
  local continue_add="1"
  local node_choice
  local cert_mode
  local cert_domain
  local node_name
  local node_type
  local transport_mode
  local security_mode
  local node_id
  local total_nodes
  local i

  declare -a NODE_BLOCKS
  declare -a XHTTP_HINTS

  echo "V2bX 配置向导"
  echo "- 主配置文件将写入: $CONFIG_FILE"
  echo "- 旧配置会备份为: ${CONFIG_FILE}.bak"
  echo "- xhttp 示例: ${CONFIG_DIR}/config_xhttp_reality.json"
  echo "- xhttp 模板: ${CONFIG_DIR}/xhttp_template.conf"

  if ! prompt_yes_no "确认开始生成配置" 1; then
    wizard_warn "已取消"
    return 0
  fi

  mkdir -p "$CONFIG_DIR"
  if [[ -f "$CONFIG_FILE" ]]; then
    cp -f "$CONFIG_FILE" "${CONFIG_FILE}.bak"
  fi

  api_host="$(prompt_non_empty '请输入面板地址(例如 https://panel.example.com): ')"
  api_key="$(prompt_non_empty '请输入面板 API Key: ')"

  if prompt_yes_no "后续节点是否复用同一面板地址和 API Key" 1; then
    fixed_api="1"
  fi

  while [[ "$continue_add" == "1" ]]; do
    node_name="$(prompt_non_empty '请输入节点名称(示例: node-1): ')"

    while true; do
      read -r -p "请输入 NodeID(数字): " node_id
      if [[ "$node_id" =~ ^[0-9]+$ ]]; then
        break
      fi
      wizard_warn "NodeID 必须是数字"
    done

    node_choice="$(prompt_node_type)"
    node_type="$node_choice"
    transport_mode=""
    security_mode="none"
    cert_mode="none"
    cert_domain=""

    if [[ "$node_choice" == "vless_xhttp" ]]; then
      node_type="vless"
      transport_mode="xhttp"
      security_mode="$(prompt_xhttp_security_mode)"
      if [[ "$security_mode" == "tls" ]]; then
        cert_mode="$(prompt_cert_mode)"
        while [[ "$cert_mode" == "none" ]]; do
          wizard_warn "xhttp + tls 需要证书，CertMode 不能为 none"
          cert_mode="$(prompt_cert_mode)"
        done
        cert_domain="$(prompt_non_empty '请输入证书域名(例如 node.example.com): ')"
      else
        cert_mode="none"
      fi
      XHTTP_HINTS+=("${node_name}:${security_mode}")
    elif [[ "$node_choice" == "vless" ]]; then
      security_mode="$(prompt_vless_security_mode)"
      if [[ "$security_mode" == "tls" ]]; then
        cert_mode="$(prompt_cert_mode)"
        while [[ "$cert_mode" == "none" ]]; do
          wizard_warn "vless + tls 需要证书，CertMode 不能为 none"
          cert_mode="$(prompt_cert_mode)"
        done
        cert_domain="$(prompt_non_empty '请输入证书域名(例如 node.example.com): ')"
      elif [[ "$security_mode" == "reality" ]]; then
        cert_mode="none"
      fi
    else
      if prompt_yes_no "该节点是否启用 TLS 证书配置" 0; then
        cert_mode="$(prompt_cert_mode)"
        if [[ "$cert_mode" != "none" ]]; then
          cert_domain="$(prompt_non_empty '请输入证书域名(例如 node.example.com): ')"
        fi
      fi
    fi

    NODE_BLOCKS+=("    {
      \"Name\": \"${node_name}\",
      \"Core\": \"xray\",
      \"ApiHost\": \"${api_host}\",
      \"ApiKey\": \"${api_key}\",
      \"NodeID\": ${node_id},
      \"NodeType\": \"${node_type}\",
      \"Timeout\": 30,
      \"ListenIP\": \"0.0.0.0\",
      \"SendIP\": \"0.0.0.0\",
      \"EnableProxyProtocol\": false,
      \"EnableTFO\": true,
      \"EnableDNS\": true,
      \"DNSType\": \"UseIPv4\",
      \"DisableSniffing\": false,
      \"CertConfig\": {
        \"CertMode\": \"${cert_mode}\",
        \"RejectUnknownSni\": false,
        \"CertDomain\": \"${cert_domain}\",
        \"CertFile\": \"/etc/V2bX/fullchain.cer\",
        \"KeyFile\": \"/etc/V2bX/cert.key\",
        \"Provider\": \"cloudflare\",
        \"Email\": \"admin@example.com\",
        \"DNSEnv\": {
          \"EnvName\": \"env1\"
        }
      }
    }")

    if [[ "$fixed_api" != "1" ]]; then
      api_host="$(prompt_non_empty '请输入下一个节点面板地址: ')"
      api_key="$(prompt_non_empty '请输入下一个节点 API Key: ')"
    fi

    if prompt_yes_no "是否继续添加节点" 0; then
      continue_add="1"
    else
      continue_add="0"
    fi
  done

  if [[ ${#NODE_BLOCKS[@]} -eq 0 ]]; then
    wizard_error "未添加任何节点，取消写入"
    return 1
  fi

  {
    cat <<'EOF'
{
  "Log": {
    "Level": "info",
    "Output": ""
  },
  "Cores": [
    {
      "Type": "xray",
      "Log": {
        "Level": "warn"
      },
      "AssetPath": "/etc/V2bX/",
      "DnsConfigPath": "/etc/V2bX/dns.json",
      "RouteConfigPath": "/etc/V2bX/route.json"
    }
  ],
  "Nodes": [
EOF

    total_nodes=${#NODE_BLOCKS[@]}
    for i in "${!NODE_BLOCKS[@]}"; do
      printf "%b" "${NODE_BLOCKS[$i]}"
      if [[ $i -lt $((total_nodes - 1)) ]]; then
        printf ",\n"
      else
        printf "\n"
      fi
    done

    cat <<'EOF'
  ]
}
EOF
  } > "$CONFIG_FILE"

  ensure_sidecar_files

  wizard_info "配置生成完成: $CONFIG_FILE"
  wizard_info "如果你要使用 xhttp，请在面板把 VLESS 节点 network 设置为 xhttp"
  wizard_info "可参考: ${CONFIG_DIR}/xhttp_template.conf"
  if [[ ${#XHTTP_HINTS[@]} -gt 0 ]]; then
    wizard_info "本次向导已选择 xhttp 的节点:"
    for i in "${!XHTTP_HINTS[@]}"; do
      wizard_info "- ${XHTTP_HINTS[$i]}"
    done
    wizard_info "面板侧请同步设置: network=xhttp；security=对应 reality/tls"
  fi

  restart_service_if_needed
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  generate_config_file
fi
