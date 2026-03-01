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
  printf "%b[WARN]%b %s\n" "$YELLOW" "$PLAIN" "$1"
}

wizard_error() {
  printf "%b[ERROR]%b %s\n" "$RED" "$PLAIN" "$1"
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
  echo "请选择节点协议类型:"
  echo "1) vless (推荐，可配 xhttp/reality)"
  echo "2) vmess"
  echo "3) trojan"
  echo "4) shadowsocks"
  while true; do
    read -r -p "输入 [1-4]: " n
    case "$n" in
      1) printf 'vless'; return ;;
      2) printf 'vmess'; return ;;
      3) printf 'trojan'; return ;;
      4) printf 'shadowsocks'; return ;;
      *) wizard_warn "请输入 1-4" ;;
    esac
  done
}

prompt_cert_mode() {
  local n
  echo "请选择证书模式:"
  echo "1) none"
  echo "2) http"
  echo "3) dns"
  echo "4) file"
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
  local cert_mode
  local cert_domain
  local node_name
  local node_type
  local node_id
  local total_nodes
  local i

  declare -a NODE_BLOCKS

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

    node_type="$(prompt_node_type)"
    cert_mode="$(prompt_cert_mode)"
    cert_domain=""
    if [[ "$cert_mode" != "none" ]]; then
      cert_domain="$(prompt_non_empty '请输入证书域名(例如 node.example.com): ')"
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

  restart_service_if_needed
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  generate_config_file
fi
