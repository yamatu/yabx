#!/usr/bin/env bash
set -u

REPO_OWNER="yamatu"
REPO_NAME="yabx"

INSTALL_DIR="/usr/local/V2bX"
CONFIG_DIR="/etc/V2bX"
CONFIG_FILE="${CONFIG_DIR}/config.json"
SERVICE_NAME="V2bX"
BIN_PATH="${INSTALL_DIR}/V2bX"
INIT_CONFIG_SCRIPT="${INSTALL_DIR}/initconfig.sh"
ACME_CF_SCRIPT="${INSTALL_DIR}/acme_cf.sh"
# Multi process mode: one process per node, one generated config per node.
NODES_DIR="${CONFIG_DIR}/nodes"
INSTANCE_PREFIX="v2bx@"
INSTANCE_TEMPLATE_FILE="/etc/systemd/system/v2bx@.service"

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

resolve_abs_path() {
  local p="$1"
  if has_cmd readlink; then
    readlink -f "$p" 2>/dev/null || echo "$p"
    return
  fi
  echo "$p"
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

# The installed kernels and their versions, read from the binary itself so
# the menu cannot drift from what the build actually links.
core_version_lines() {
  if ! is_installed; then
    return 0
  fi
  run_core_binary version 2>/dev/null | grep -E '^[a-z][a-z0-9]* v[0-9]' || true
}

show_status_line() {
  if ! is_installed; then
    echo -e "V2bX 状态: ${RED}未安装${PLAIN}"
    return
  fi

  if multi_mode; then
    echo -e "运行模式: ${GREEN}多进程${PLAIN} ($(instance_summary))"
    if is_running || is_enabled; then
      echo -e "提示: ${YELLOW}单进程服务 V2bX 仍处于运行/自启状态，两个模式不要同时开启${PLAIN}"
      echo -e "      执行 v2bx multi rollback 或 systemctl disable --now V2bX"
    fi
  else
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

    if multi_configs_exist; then
      echo -e "提示: 检测到每节点配置文件，可执行 v2bx multi migrate 切换为多进程模式"
    fi
  fi

  local versions="" line
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    versions+="${versions:+ / }$line"
  done < <(core_version_lines)
  if [[ -n "$versions" ]]; then
    echo -e "内核版本: ${versions}"
  fi
}

# ---------------------------------------------------------------- multi process
# One process per node. The reason to prefer it over one process with several
# nodes: updateDNSConfig rewrites the DNS file of a node on every reload and the
# config watcher restarts the process when that file changes, so in one process a
# single node restarts every other node. Separate processes also isolate a crash
# and the CPU and memory of one node from the others.

# The instance names of the generated per node configs in NODES_DIR.
instance_names() {
  local dir="$NODES_DIR" file name
  [[ -d "$dir" ]] || return 0
  for file in "$dir"/*.json; do
    [[ -e "$file" ]] || continue
    name="${file##*/}"
    printf '%s\n' "${name%.json}"
  done
}

# Generated per node configs exist, no matter whether they are running.
multi_configs_exist() {
  local name
  name="$(instance_names | head -n 1)"
  [[ -n "$name" ]]
}

instance_active() {
  has_cmd systemctl && systemctl is-active --quiet "${INSTANCE_PREFIX}$1"
}

instance_enabled() {
  has_cmd systemctl && systemctl is-enabled --quiet "${INSTANCE_PREFIX}$1"
}

# The mode the host is actually in: generated configs are not enough, at least
# one instance has to be running or enabled.
multi_mode() {
  if ! has_cmd systemctl; then
    return 1
  fi
  local name
  while IFS= read -r name; do
    [[ -z "$name" ]] && continue
    if instance_active "$name" || instance_enabled "$name"; then
      return 0
    fi
  done < <(instance_names)
  return 1
}

instance_summary() {
  local total=0 running=0 name
  while IFS= read -r name; do
    [[ -z "$name" ]] && continue
    total=$((total + 1))
    if instance_active "$name"; then
      running=$((running + 1))
    fi
  done < <(instance_names)
  printf '%s 个实例, %s 运行中' "$total" "$running"
}

show_instances_status() {
  local name state boot
  if ! multi_configs_exist; then
    warn "未找到每节点配置文件 ($NODES_DIR)"
    return 1
  fi
  echo "实例状态:"
  while IFS= read -r name; do
    [[ -z "$name" ]] && continue
    if instance_active "$name"; then
      state="${GREEN}运行中${PLAIN}"
    else
      state="${YELLOW}已停止${PLAIN}"
    fi
    if instance_enabled "$name"; then
      boot="开机自启"
    else
      boot="未自启"
    fi
    printf "  %-28s %b  (%s)\n" "$name" "$state" "$boot"
  done < <(instance_names)
  echo
  echo "配置目录: $NODES_DIR"
  echo "查看日志: journalctl -u ${INSTANCE_PREFIX}<节点名> -e --no-pager"
}

# Instances that are enabled but whose config file is gone: they can only fail.
warn_stale_instances() {
  local names="" name listed
  if ! has_cmd systemctl; then
    return 0
  fi
  while IFS= read -r name; do
    [[ -z "$name" ]] && continue
    listed="${name#${INSTANCE_PREFIX}}"
    listed="${listed%.service}"
    if [[ ! -f "$NODES_DIR/${listed}.json" ]]; then
      names+="${names:+ }${listed}"
    fi
  done < <(systemctl list-unit-files "${INSTANCE_PREFIX}*.service" --no-legend 2>/dev/null | awk '{print $1}')
  if [[ -n "$names" ]]; then
    warn "以下实例已启用但配置已不存在: $names"
    warn "可执行: systemctl disable --now ${INSTANCE_PREFIX}<节点名>"
  fi
}

# Runs one action on every instance.
foreach_instance() {
  local action="$1" name failed="0"
  while IFS= read -r name; do
    [[ -z "$name" ]] && continue
    case "$action" in
      start) systemctl start "${INSTANCE_PREFIX}${name}" ;;
      stop) systemctl stop "${INSTANCE_PREFIX}${name}" ;;
      restart) systemctl restart "${INSTANCE_PREFIX}${name}" ;;
      enable) systemctl enable "${INSTANCE_PREFIX}${name}" >/dev/null 2>&1 ;;
      disable) systemctl disable "${INSTANCE_PREFIX}${name}" >/dev/null 2>&1 ;;
    esac || {
      error "${INSTANCE_PREFIX}${name} ${action} 失败"
      failed="1"
    }
  done < <(instance_names)
  if [[ "$failed" == "1" ]]; then
    warn "有实例执行 ${action} 失败，请检查上面的日志"
    return 1
  fi
  case "$action" in
    start) info "已启动全部实例" ;;
    stop) info "已停止全部实例" ;;
    restart) info "已重启全部实例" ;;
    enable) info "已设置全部实例开机自启" ;;
    disable) info "已取消全部实例开机自启" ;;
  esac
}

# In multi process mode the single process unit is the wrong target for
# start/stop/restart/status/log/enable/disable, so those actions are applied to
# every instance instead. It must only be called after multi_mode returned 0.
redirect_multi() {
  case "$1" in
    start | stop | restart | enable | disable)
      foreach_instance "$1"
      ;;
    status)
      show_instances_status
      ;;
    log)
      local name
      name="$(pick_instance)" || return 1
      [[ -n "$name" ]] || return 1
      instance_control log "$name"
      ;;
    *)
      error "未知操作: $1"
      return 1
      ;;
  esac
}

# Writes one config per node. The multi node config is never modified, so
# switching back is one systemctl call.
split_node_configs() {
  local force="${1:-}"
  if [[ ! -f "$CONFIG_FILE" ]]; then
    error "未找到配置文件: $CONFIG_FILE"
    return 1
  fi
  info "正在生成每节点配置文件 (每个节点一份 DNS 配置，互不影响)..."
  if ! run_core_binary split -c "$CONFIG_FILE" -o "$NODES_DIR" $force; then
    error "生成失败，配置未做任何改动"
    return 1
  fi
}

start_all_instances() {
  local failed="0" name
  while IFS= read -r name; do
    [[ -z "$name" ]] && continue
    if systemctl enable --now "${INSTANCE_PREFIX}${name}" >/dev/null 2>&1; then
      info "已启动 ${INSTANCE_PREFIX}${name}"
    else
      error "${INSTANCE_PREFIX}${name} 启动失败，查看: journalctl -u ${INSTANCE_PREFIX}${name} -e --no-pager"
      failed="1"
    fi
  done < <(instance_names)
  sleep 2
  show_instances_status
  if [[ "$failed" == "1" ]]; then
    warn "有实例启动失败，可随时回退: v2bx multi rollback"
  fi
}

migrate_to_multi() {
  if ! ensure_installed; then
    return 1
  fi
  if ! has_cmd systemctl; then
    error "当前系统没有 systemd，无法使用多进程模式"
    return 1
  fi
  if multi_mode; then
    warn "已经处于多进程模式"
    warn "修改了 $CONFIG_FILE 后重新生成请用: v2bx multi reload"
    return 1
  fi
  if [[ ! -f "$INSTANCE_TEMPLATE_FILE" ]]; then
    error "未找到 systemd 模板单元: $INSTANCE_TEMPLATE_FILE"
    error "请先执行 v2bx update 更新安装脚本(会写入 v2bx@.service)"
    return 1
  fi

  local ans force=""
  read -r -p "将 $CONFIG_FILE 拆分为每节点一个进程，是否继续？[y/N]: " ans
  if [[ ! "$ans" =~ ^[Yy]$ ]]; then
    warn "已取消"
    return 0
  fi
  if multi_configs_exist; then
    read -r -p "$NODES_DIR 已存在配置文件，是否覆盖？[y/N]: " ans
    if [[ ! "$ans" =~ ^[Yy]$ ]]; then
      warn "已取消"
      return 0
    fi
    force="--force"
  fi

  if ! split_node_configs "$force"; then
    return 1
  fi
  if ! multi_configs_exist; then
    error "没有生成任何配置文件，已中止"
    return 1
  fi

  info "停止单进程服务 V2bX (配置文件不会被修改)..."
  systemctl stop V2bX >/dev/null 2>&1 || true
  systemctl disable V2bX >/dev/null 2>&1 || true

  start_all_instances
  warn_stale_instances
  info "回退命令: v2bx multi rollback"
}

reapply_split() {
  if ! ensure_installed; then
    return 1
  fi
  if ! split_node_configs "--force"; then
    return 1
  fi
  local name
  while IFS= read -r name; do
    [[ -z "$name" ]] && continue
    if instance_active "$name" || instance_enabled "$name"; then
      systemctl restart "${INSTANCE_PREFIX}${name}" >/dev/null 2>&1 || \
        error "${INSTANCE_PREFIX}${name} 重启失败，查看: journalctl -u ${INSTANCE_PREFIX}${name} -e --no-pager"
    else
      systemctl enable --now "${INSTANCE_PREFIX}${name}" >/dev/null 2>&1 || true
    fi
  done < <(instance_names)
  warn_stale_instances
  show_instances_status
}

rollback_to_single() {
  if ! ensure_installed; then
    return 1
  fi
  if ! multi_configs_exist; then
    warn "当前不是多进程模式"
    return 1
  fi
  local ans
  read -r -p "停止所有 v2bx@ 实例并恢复单进程 V2bX？[Y/n]: " ans
  if [[ -n "$ans" && ! "$ans" =~ ^[Yy]$ ]]; then
    warn "已取消"
    return 0
  fi

  local name
  while IFS= read -r name; do
    [[ -z "$name" ]] && continue
    systemctl disable --now "${INSTANCE_PREFIX}${name}" >/dev/null 2>&1 || \
      systemctl stop "${INSTANCE_PREFIX}${name}" >/dev/null 2>&1 || true
  done < <(instance_names)

  if systemctl enable --now V2bX >/dev/null 2>&1; then
    sleep 1
    if is_running; then
      info "已恢复单进程模式"
    else
      warn "V2bX 未启动，查看: v2bx log"
    fi
  else
    error "恢复单进程模式失败，请执行: systemctl status V2bX"
  fi
  info "每节点配置保留在 $NODES_DIR (不会被删除)"
}

pick_instance() {
  local names=() name i=1 choice
  while IFS= read -r line; do
    [[ -n "$line" ]] && names+=("$line")
  done < <(instance_names)
  if [[ ${#names[@]} -eq 0 ]]; then
    error "未找到实例，请先执行 v2bx multi migrate" >&2
    return 1
  fi
  if [[ ${#names[@]} -eq 1 ]]; then
    echo "${names[0]}"
    return 0
  fi
  for name in "${names[@]}"; do
    echo "  $i) $name"
    i=$((i + 1))
  done
  read -r -p "请选择实例编号: " choice
  if [[ ! "$choice" =~ ^[0-9]+$ ]] || ((choice < 1 || choice > ${#names[@]})); then
    error "无效的选择" >&2
    return 1
  fi
  echo "${names[$((choice - 1))]}"
}

instance_control() {
  local action="$1" name="${2:-}"
  if [[ -z "$name" ]]; then
    name="$(pick_instance)" || return 1
  fi
  if [[ ! -f "$NODES_DIR/${name}.json" ]]; then
    error "实例配置不存在: $NODES_DIR/${name}.json"
    return 1
  fi
  case "$action" in
    start)
      systemctl start "${INSTANCE_PREFIX}${name}" || { error "启动失败"; return 1; }
      info "${INSTANCE_PREFIX}${name} 已启动"
      ;;
    stop)
      systemctl stop "${INSTANCE_PREFIX}${name}" || { error "停止失败"; return 1; }
      info "${INSTANCE_PREFIX}${name} 已停止"
      ;;
    restart)
      systemctl restart "${INSTANCE_PREFIX}${name}" || { error "重启失败"; return 1; }
      sleep 1
      if instance_active "$name"; then
        info "${INSTANCE_PREFIX}${name} 已重启"
      else
        error "重启后实例未运行，查看: journalctl -u ${INSTANCE_PREFIX}${name} -e --no-pager"
        return 1
      fi
      ;;
    status)
      systemctl status "${INSTANCE_PREFIX}${name}" --no-pager -l
      ;;
    log)
      journalctl -u "${INSTANCE_PREFIX}${name}.service" -e --no-pager -f
      ;;
    enable)
      systemctl enable "${INSTANCE_PREFIX}${name}" >/dev/null 2>&1 || { error "设置开机自启失败"; return 1; }
      info "${INSTANCE_PREFIX}${name} 已设置开机自启"
      ;;
    disable)
      systemctl disable "${INSTANCE_PREFIX}${name}" >/dev/null 2>&1 || { error "取消开机自启失败"; return 1; }
      info "${INSTANCE_PREFIX}${name} 已取消开机自启"
      ;;
    *)
      error "未知操作: $action"
      return 1
      ;;
  esac
}

instance_menu() {
  local name num
  name="$(pick_instance)" || return 1
  [[ -n "$name" ]] || return 1
  echo "实例: $name"
  echo "1. 启动"
  echo "2. 停止"
  echo "3. 重启"
  echo "4. 状态"
  echo "5. 查看日志 (Ctrl+C 退出)"
  echo "6. 设置/取消开机自启"
  echo "0. 返回"
  read -r -p "请输入选择 [0-6]: " num
  case "$num" in
    1) instance_control start "$name" ;;
    2) instance_control stop "$name" ;;
    3) instance_control restart "$name" ;;
    4) instance_control status "$name" ;;
    5) instance_control log "$name" ;;
    6)
      if instance_enabled "$name"; then
        instance_control disable "$name"
      else
        instance_control enable "$name"
      fi
      ;;
    0) return 0 ;;
    *) warn "请输入 0-6 的数字" ;;
  esac
}

show_multi_menu() {
  local num
  while true; do
    if [[ -t 1 ]] && has_cmd clear; then
      clear
    fi
    cat <<'MENUEOF'
V2bX 多进程模式 (每个节点一个进程)
----------------------------------------
1. 查看各实例状态
2. 切换为多进程模式(拆分配置文件并启动实例)
3. 重新生成配置并重启所有实例(修改 config.json 后使用)
4. 回退到单进程模式
5. 单个实例操作(启动/停止/重启/日志)
6. 查看多进程模式说明
0. 返回主菜单
----------------------------------------
MENUEOF
    if multi_mode; then
      echo -e "当前模式: ${GREEN}多进程${PLAIN} ($(instance_summary))"
    else
      echo -e "当前模式: ${YELLOW}单进程${PLAIN}"
    fi
    read -r -p "请输入选择 [0-6]: " num
    case "$num" in
      1) show_instances_status; pause_back ;;
      2) migrate_to_multi; pause_back ;;
      3) reapply_split; pause_back ;;
      4) rollback_to_single; pause_back ;;
      5) instance_menu; pause_back ;;
      6) show_multi_help; pause_back ;;
      0) return 0 ;;
      *) warn "请输入 0-6 的数字"; pause_back ;;
    esac
  done
}

show_multi_help() {
  cat <<'HELPEOF'
多进程模式说明
1) 每个节点一个进程: v2bx@<节点名>.service
2) 每个进程使用 /etc/V2bX/nodes/<节点名>.json，并拥有自己的
   DNS 配置 (nodes/dns/) 与日志 (nodes/log/)
3) /etc/V2bX/config.json 不会被修改，回退只需恢复 V2bX 服务
4) 切换: v2bx multi migrate / v2bx multi rollback
5) 修改 config.json 后: v2bx multi reload
6) 单个实例日志: journalctl -u v2bx@<节点名> -e --no-pager
7) 节点数远多于机器核心数时不建议使用多进程模式

为什么不建议两个模式同时开启:
两个模式都会把节点监听在相同的端口上，同时运行会导致端口冲突,
所以 migrate 会自动停止并禁用单进程的 V2bX 服务。
HELPEOF
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

run_core_binary() {
  local target="$BIN_PATH"
  local self_path
  local target_path

  self_path="$(resolve_abs_path "$0")"
  target_path="$(resolve_abs_path "$target")"

  if [[ "$target_path" == "$self_path" ]]; then
    target="/usr/bin/v2bx-bin"
    target_path="$(resolve_abs_path "$target")"
  fi

  if [[ ! -x "$target" ]]; then
    error "核心二进制不存在: $target"
    return 1
  fi
  if [[ "$target_path" == "$self_path" ]]; then
    error "检测到命令路径冲突，请重新执行安装脚本修复"
    return 1
  fi

  "$target" "$@"
}

start_service() {
  if multi_mode; then
    redirect_multi start
    return $?
  fi
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
  if multi_mode; then
    redirect_multi stop
    return $?
  fi
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
  if multi_mode; then
    redirect_multi restart
    return $?
  fi
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
  if multi_mode; then
    redirect_multi status
    return $?
  fi
  if ! ensure_installed; then
    return 1
  fi
  systemctl status "$SERVICE_NAME" --no-pager -l
}

log_service() {
  if multi_mode; then
    redirect_multi log
    return $?
  fi
  if ! ensure_installed; then
    return 1
  fi
  journalctl -u "${SERVICE_NAME}.service" -e --no-pager -f
}

enable_service() {
  if multi_mode; then
    redirect_multi enable
    return $?
  fi
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
  if multi_mode; then
    redirect_multi disable
    return $?
  fi
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
  if multi_mode; then
    warn "当前是多进程模式，实例读取的是 $NODES_DIR/<节点名>.json"
    read -r -p "是否重新生成每节点配置并重启所有实例？[Y/n]: " ans
    if [[ -z "$ans" || "$ans" =~ ^[Yy]$ ]]; then
      reapply_split
    fi
    return 0
  fi
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

run_acme_manager() {
  local action="${1:-setup}"
  if [[ ! -f "$ACME_CF_SCRIPT" ]]; then
    error "ACME Cloudflare helper not found: $ACME_CF_SCRIPT"
    error "Please run: v2bx update"
    return 1
  fi
  chmod +x "$ACME_CF_SCRIPT" >/dev/null 2>&1 || true
  bash "$ACME_CF_SCRIPT" "$action"
}

show_x25519() {
  if ! ensure_installed; then
    return 1
  fi
  run_core_binary x25519
}

show_version() {
  if ! ensure_installed; then
    return 1
  fi
  run_core_binary version
}

show_xhttp_help() {
  cat <<'EOF'
协议示例说明
XHTTP:
1) 面板节点协议请使用 vless，network 设为 xhttp
2) 示例配置: /etc/V2bX/config_xhttp_reality.json
3) xhttp 参数模板: /etc/V2bX/xhttp_template.conf

Naive:
1) 面板节点协议请使用 naive
2) 本地节点 Core 请使用 sing
3) 示例配置: /etc/V2bX/config_naive.json
4) naive 需要 TLS 证书，CertMode 不能为 none

通用:
1) 主配置文件: /etc/V2bX/config.json
提示: 修改完配置后执行 v2bx restart
EOF
}

uninstall_v2bx() {
  read -r -p "确定要卸载 V2bX 吗？[y/N]: " ans
  if [[ ! "$ans" =~ ^[Yy]$ ]]; then
    warn "已取消卸载"
    return 0
  fi

  local name
  while IFS= read -r name; do
    [[ -z "$name" ]] && continue
    systemctl disable --now "${INSTANCE_PREFIX}${name}" >/dev/null 2>&1 || true
  done < <(instance_names)
  systemctl stop "$SERVICE_NAME" >/dev/null 2>&1 || true
  systemctl disable "$SERVICE_NAME" >/dev/null 2>&1 || true
  rm -f /etc/systemd/system/V2bX.service "$INSTANCE_TEMPLATE_FILE"
  systemctl reset-failed "${INSTANCE_PREFIX}*" >/dev/null 2>&1 || true
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
  v2bx multi [action]  多进程模式: status|migrate|reload|rollback|menu
                       单个实例: start|stop|restart|log|status [节点名]
  v2bx config          编辑 /etc/V2bX/config.json
  v2bx generate        配置向导生成 config.json
  v2bx acme [action]   Cloudflare DNS cert setup/issue/renew/status/edit
  v2bx x25519          生成 X25519 密钥
  v2bx version         查看版本(含内核)
  v2bx xhttp           显示 xhttp / naive 使用说明
  v2bx naive           显示 naive 使用说明
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
12. 查看 V2bX / 内核版本
13. 配置向导(新建/重建 config.json, 含 xhttp / naive 预设)
14. 协议示例说明(xhttp / naive)
15. Cloudflare DNS ACME certificate
16. 多进程模式(每节点独立进程)
17. 退出
----------------------------------------
EOF
  show_status_line
}

menu_loop() {
  while true; do
    show_menu
    read -r -p "请输入选择 [0-17]: " num
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
      15) run_acme_manager setup; pause_back ;;
      16) show_multi_menu; pause_back ;;
      17) exit 0 ;;
      *) warn "请输入 0-17 的数字"; pause_back ;;
    esac
  done
}

main() {
  require_root

  if [[ $# -gt 0 ]]; then
    local rc=0
    case "$1" in
      start) start_service || rc=$? ;;
      stop) stop_service || rc=$? ;;
      restart) restart_service || rc=$? ;;
      status) status_service || rc=$? ;;
      log) log_service || rc=$? ;;
      enable) enable_service || rc=$? ;;
      disable) disable_service || rc=$? ;;
      config) edit_config || rc=$? ;;
      generate) generate_config || rc=$? ;;
      acme|cert) run_acme_manager "${2:-setup}" || rc=$? ;;
      x25519) show_x25519 || rc=$? ;;
      version) show_version || rc=$? ;;
      xhttp) show_xhttp_help || rc=$? ;;
      naive) show_xhttp_help || rc=$? ;;
      multi|instances)
        case "${2:-status}" in
          status | list) show_instances_status || rc=$? ;;
          migrate | split) migrate_to_multi || rc=$? ;;
          reload | reapply) reapply_split || rc=$? ;;
          rollback | single) rollback_to_single || rc=$? ;;
          start | stop | restart | log | enable | disable)
            instance_control "$2" "${3:-}" || rc=$?
            ;;
          menu) show_multi_menu || rc=$? ;;
          *) show_multi_help ;;
        esac
        ;;
      server) run_core_binary "$@" || rc=$? ;;
      install) run_install_script "${2:-}" || rc=$? ;;
      update) run_install_script "${2:-}" || rc=$? ;;
      uninstall) uninstall_v2bx || rc=$? ;;
      *) show_usage; rc=1 ;;
    esac
    exit "$rc"
  fi

  menu_loop
}

# Only run when executed, so the functions above can be sourced by tests.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
