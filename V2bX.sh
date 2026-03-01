#!/usr/bin/env bash
set -u

REPO_OWNER="yamatu"
REPO_NAME="yav2bx"

INSTALL_DIR="/usr/local/V2bX"
CONFIG_DIR="/etc/V2bX"
CONFIG_FILE="${CONFIG_DIR}/config.json"
SERVICE_NAME="V2bX"
BIN_PATH="${INSTALL_DIR}/V2bX"
INIT_CONFIG_SCRIPT="${INSTALL_DIR}/initconfig.sh"

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
  printf "%b[ERROR]%b %s\n" "$RED" "$PLAIN" "$1"
}

require_root() {
  if [[ ${EUID:-0} -ne 0 ]]; then
    error "必须使用 root 用户运行"
    exit 1
  fi
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

is_installed() {
  [[ -x "$BIN_PATH" ]]
}

is_running() {
  if ! has_cmd systemctl; then
    return 1
  fi
  systemctl is-active --quiet "$SERVICE_NAME"
}

is_enabled() {
  if ! has_cmd systemctl; then
    return 1
  fi
  systemctl is-enabled --quiet "$SERVICE_NAME"
}

check_status() {
  if ! is_installed; then
    return 2
  fi
  if is_running; then
    return 0
  fi
  return 1
}

show_status_line() {
  check_status
  case $? in
    0)
      echo -e "V2bX 状态: ${GREEN}运行中${PLAIN}"
      ;;
    1)
      echo -e "V2bX 状态: ${YELLOW}未运行${PLAIN}"
      ;;
    2)
      echo -e "V2bX 状态: ${RED}未安装${PLAIN}"
      return
      ;;
  esac

  if is_enabled; then
    echo -e "开机自启: ${GREEN}已开启${PLAIN}"
  else
    echo -e "开机自启: ${YELLOW}未开启${PLAIN}"
  fi
}

pause_back() {
  read -r -p "按回车返回主菜单: " _
}

download_install_script() {
  local url="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/install.sh"
  local out="/tmp/v2bx_install.sh"

  if has_cmd wget; then
    wget -N -O "$out" "$url"
    return $?
  fi
  if has_cmd curl; then
    curl -fsSL "$url" -o "$out"
    return $?
  fi

  error "当前系统缺少 wget/curl，无法下载安装脚本"
  return 1
}

run_install_script() {
  local version="${1:-}"
  if ! download_install_script; then
    return 1
  fi
  chmod +x /tmp/v2bx_install.sh
  if [[ -n "$version" ]]; then
    bash /tmp/v2bx_install.sh "$version"
  else
    bash /tmp/v2bx_install.sh
  fi
}

ensure_installed() {
  if ! is_installed; then
    error "未检测到 V2bX，请先安装"
    return 1
  fi
  return 0
}

start_service() {
  if ! ensure_installed; then
    return 1
  fi
  if is_running; then
    warn "V2bX 已在运行"
    return 0
  fi
  if ! systemctl start "$SERVICE_NAME"; then
    error "启动失败，请检查日志"
    return 1
  fi
  sleep 1
  if is_running; then
    info "V2bX 启动成功"
  else
    error "V2bX 可能启动失败，请执行 v2bx log 查看日志"
    return 1
  fi
}

stop_service() {
  if ! ensure_installed; then
    return 1
  fi
  if systemctl stop "$SERVICE_NAME"; then
    info "V2bX 停止成功"
  else
    error "停止失败"
    return 1
  fi
}

restart_service() {
  if ! ensure_installed; then
    return 1
  fi
  if ! systemctl restart "$SERVICE_NAME"; then
    error "重启失败"
    return 1
  fi
  sleep 1
  if is_running; then
    info "V2bX 重启成功"
  else
    error "重启后服务未运行，请执行 v2bx log 查看日志"
    return 1
  fi
}

status_service() {
  if ! ensure_installed; then
    return 1
  fi
  systemctl status "$SERVICE_NAME" --no-pager -l
}

log_service() {
  if ! ensure_installed; then
    return 1
  fi
  journalctl -u "${SERVICE_NAME}.service" -e --no-pager -f
}

enable_service() {
  if ! ensure_installed; then
    return 1
  fi
  if systemctl enable "$SERVICE_NAME" >/dev/null 2>&1; then
    info "已设置开机自启"
  else
    error "设置开机自启失败"
    return 1
  fi
}

disable_service() {
  if ! ensure_installed; then
    return 1
  fi
  if systemctl disable "$SERVICE_NAME" >/dev/null 2>&1; then
    info "已取消开机自启"
  else
    error "取消开机自启失败"
    return 1
  fi
}

pick_editor() {
  if [[ -n "${EDITOR:-}" ]] && has_cmd "$EDITOR"; then
    echo "$EDITOR"
    return
  fi
  for e in vim nvim vi nano; do
    if has_cmd "$e"; then
      echo "$e"
      return
    fi
  done
  echo ""
}

edit_config() {
  if ! ensure_installed; then
    return 1
  fi
  mkdir -p "$CONFIG_DIR"
  if [[ ! -f "$CONFIG_FILE" ]]; then
    if [[ -f "$INSTALL_DIR/config.json" ]]; then
      cp -f "$INSTALL_DIR/config.json" "$CONFIG_FILE"
    else
      error "未找到配置文件模板，请先执行安装"
      return 1
    fi
  fi

  local editor
  editor="$(pick_editor)"
  if [[ -z "$editor" ]]; then
    error "未找到可用编辑器，请安装 nano/vim，或设置 EDITOR 环境变量"
    return 1
  fi

  "$editor" "$CONFIG_FILE"
  read -r -p "配置已保存，是否立即重启 V2bX？[Y/n]: " ans
  if [[ -z "$ans" || "$ans" =~ ^[Yy]$ ]]; then
    restart_service
  fi
}

generate_config() {
  if [[ ! -f "$INIT_CONFIG_SCRIPT" ]]; then
    error "未找到配置向导脚本: $INIT_CONFIG_SCRIPT"
    return 1
  fi
  # shellcheck source=/usr/local/V2bX/initconfig.sh
  source "$INIT_CONFIG_SCRIPT"
  if declare -F generate_config_file >/dev/null 2>&1; then
    generate_config_file
  else
    error "配置向导脚本加载失败"
    return 1
  fi
}

show_x25519() {
  if ! ensure_installed; then
    return 1
  fi
  "$BIN_PATH" x25519
}

show_version() {
  if ! ensure_installed; then
    return 1
  fi
  "$BIN_PATH" version
}

show_xhttp_help() {
  cat <<'EOF'
XHTTP 使用说明
1) 面板节点协议请使用 vless，network 设为 xhttp
2) 示例配置: /etc/V2bX/config_xhttp_reality.json
3) xhttp 参数模板: /etc/V2bX/xhttp_template.conf
4) 主配置文件: /etc/V2bX/config.json
提示: 修改完配置后执行 v2bx restart
EOF
}

uninstall_v2bx() {
  read -r -p "确定要卸载 V2bX 吗？[y/N]: " ans
  if [[ ! "$ans" =~ ^[Yy]$ ]]; then
    warn "已取消卸载"
    return 0
  fi

  systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
  systemctl disable "$SERVICE_NAME" >/dev/null 2>&1 || true
  rm -f /etc/systemd/system/V2bX.service
  rm -rf "$CONFIG_DIR"
  rm -rf "$INSTALL_DIR"
  rm -f /usr/bin/v2bx /usr/bin/V2bX /usr/bin/v2bx-bin
  systemctl daemon-reload >/dev/null 2>&1 || true
  systemctl reset-failed >/dev/null 2>&1 || true

  info "卸载完成"
}

show_usage() {
  cat <<'EOF'
v2bx 命令用法:
  v2bx                 打开管理菜单
  v2bx install [ver]   安装/重装
  v2bx update [ver]    更新
  v2bx uninstall       卸载
  v2bx start|stop|restart|status|log
  v2bx enable|disable
  v2bx config          编辑 /etc/V2bX/config.json
  v2bx generate        配置向导生成 config.json
  v2bx x25519          生成 X25519 密钥
  v2bx version         查看版本
  v2bx xhttp           显示 xhttp 使用说明
EOF
}

show_menu() {
  if [[ -t 1 ]] && has_cmd clear; then
    clear
  fi
  cat <<'EOF'
V2bX 管理菜单
----------------------------------------
0. 修改配置文件
1. 安装/重装 V2bX
2. 更新 V2bX
3. 卸载 V2bX
----------------------------------------
4. 启动 V2bX
5. 停止 V2bX
6. 重启 V2bX
7. 查看 V2bX 状态
8. 查看 V2bX 日志
----------------------------------------
9. 设置开机自启
10. 取消开机自启
11. 生成 X25519 密钥
12. 查看 V2bX 版本
13. 配置向导(新建/重建 config.json, 含 xhttp 预设)
14. XHTTP 使用说明
15. 退出
----------------------------------------
EOF
  show_status_line
}

menu_loop() {
  while true; do
    show_menu
    read -r -p "请输入选择 [0-15]: " num
    case "$num" in
      0) edit_config; pause_back ;;
      1) run_install_script; pause_back ;;
      2)
        read -r -p "输入版本号(留空为最新版): " version
        run_install_script "$version"
        pause_back
        ;;
      3) uninstall_v2bx; pause_back ;;
      4) start_service; pause_back ;;
      5) stop_service; pause_back ;;
      6) restart_service; pause_back ;;
      7) status_service; pause_back ;;
      8) log_service; pause_back ;;
      9) enable_service; pause_back ;;
      10) disable_service; pause_back ;;
      11) show_x25519; pause_back ;;
      12) show_version; pause_back ;;
      13) generate_config; pause_back ;;
      14) show_xhttp_help; pause_back ;;
      15) exit 0 ;;
      *) warn "请输入 0-15 的数字"; pause_back ;;
    esac
  done
}

main() {
  require_root

  if [[ $# -gt 0 ]]; then
    case "$1" in
      start) start_service ;;
      stop) stop_service ;;
      restart) restart_service ;;
      status) status_service ;;
      log) log_service ;;
      enable) enable_service ;;
      disable) disable_service ;;
      config) edit_config ;;
      generate) generate_config ;;
      x25519) show_x25519 ;;
      version) show_version ;;
      xhttp) show_xhttp_help ;;
      install) run_install_script "${2:-}" ;;
      update) run_install_script "${2:-}" ;;
      uninstall) uninstall_v2bx ;;
      *) show_usage ;;
    esac
    exit 0
  fi

  menu_loop
}

main "$@"
