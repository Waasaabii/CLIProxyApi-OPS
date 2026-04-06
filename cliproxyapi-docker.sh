#!/bin/sh

set -eu

SCRIPT_NAME=$(basename "$0")
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
WORKSPACE_ROOT="$SCRIPT_DIR"
DEFAULT_CPA_BASE_DIR="$SCRIPT_DIR/.cpa-docker"

CPA_BASE_DIR=${CPA_BASE_DIR-}
CPA_DATA_DIR=${CPA_DATA_DIR-}
CPA_COMPOSE_FILE=${CPA_COMPOSE_FILE-}
CPA_CONFIG_FILE=${CPA_CONFIG_FILE-}
CPA_ENV_FILE=${CPA_ENV_FILE-}
CPA_LOG_FILE=${CPA_LOG_FILE-}
CPA_IMAGE=${CPA_IMAGE-}
CPA_CONTAINER_NAME=${CPA_CONTAINER_NAME-}
CPA_BIND_HOST=${CPA_BIND_HOST-}
CPA_HOST_PORT=${CPA_HOST_PORT-}
CPA_CONTAINER_PORT=8317
CPA_API_KEY=${CPA_API_KEY-}
CPA_MANAGEMENT_SECRET=${CPA_MANAGEMENT_SECRET-}
CPA_MANAGEMENT_SECRET_HASHED_ONLY=0
CPA_ALLOW_REMOTE_MANAGEMENT=${CPA_ALLOW_REMOTE_MANAGEMENT-}
CPA_DISABLE_CONTROL_PANEL=${CPA_DISABLE_CONTROL_PANEL-}
CPA_DEBUG=${CPA_DEBUG-}
CPA_USAGE_STATISTICS_ENABLED=${CPA_USAGE_STATISTICS_ENABLED-}
CPA_REQUEST_RETRY=${CPA_REQUEST_RETRY-}
CPA_AUTH_DIR="/data/auths"

log() {
  if [ -n "${CPA_LOG_FILE-}" ]; then
    mkdir -p "$(dirname "$CPA_LOG_FILE")"
    printf '%s INFO %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >>"$CPA_LOG_FILE"
  fi
  printf '%s\n' "$*"
}

section() {
  printf '\n========== %s ==========\n' "$1"
}

warn() {
  if [ -n "${CPA_LOG_FILE-}" ]; then
    mkdir -p "$(dirname "$CPA_LOG_FILE")"
    printf '%s WARN %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >>"$CPA_LOG_FILE"
  fi
  printf '警告: %s\n' "$*" >&2
}

fail() {
  if [ -n "${CPA_LOG_FILE-}" ]; then
    mkdir -p "$(dirname "$CPA_LOG_FILE")"
    printf '%s ERROR %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >>"$CPA_LOG_FILE"
  fi
  printf '错误: %s\n' "$*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "缺少命令: $1"
}

escape_squotes() {
  printf "%s" "$1" | sed "s/'/'\\\\''/g"
}

trim_spaces() {
  printf '%s' "$1" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//'
}

normalize_workspace_path() {
  raw_path=$(trim_spaces "$1")
  [ -n "$raw_path" ] || fail "路径不能为空"

  while [ "${raw_path#./}" != "$raw_path" ]; do
    raw_path=${raw_path#./}
  done
  if [ "$raw_path" = "." ]; then
    raw_path=""
  fi

  case "/$raw_path/" in
    */../*|*/./*)
      fail "路径不允许包含目录跳转: $raw_path"
      ;;
  esac

  case "$raw_path" in
    /*)
      normalized_path=$raw_path
      ;;
    *)
      if [ -n "$raw_path" ]; then
        normalized_path="$WORKSPACE_ROOT/$raw_path"
      else
        normalized_path="$WORKSPACE_ROOT"
      fi
      ;;
  esac

  normalized_path=$(printf '%s' "$normalized_path" | sed 's#//*#/#g; s#/$##')
  case "$normalized_path" in
    "$WORKSPACE_ROOT"|"$WORKSPACE_ROOT"/*)
      printf '%s' "$normalized_path"
      ;;
    *)
      fail "路径超出工作区: $normalized_path (workspace: $WORKSPACE_ROOT)"
      ;;
  esac
}

normalize_scalar() {
  normalized_value=$(trim_spaces "$1")
  case "$normalized_value" in
    \"*\")
      normalized_value=${normalized_value#\"}
      normalized_value=${normalized_value%\"}
      ;;
    *)
      normalized_value=${normalized_value%%[[:space:]]#*}
      normalized_value=$(trim_spaces "$normalized_value")
      ;;
  esac
  printf '%s' "$normalized_value"
}

is_bcrypt_hash() {
  case "$1" in
    '$2a$'*|'$2b$'*|'$2y$'*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

read_yaml_top_level_value() {
  yaml_file=$1
  yaml_key=$2
  awk -v key="$yaml_key" '
    $0 ~ "^" key ":[[:space:]]*" {
      sub("^[^:]*:[[:space:]]*", "", $0)
      print
      exit
    }
  ' "$yaml_file"
}

read_yaml_nested_value() {
  yaml_file=$1
  yaml_parent=$2
  yaml_key=$3
  awk -v parent="$yaml_parent" -v key="$yaml_key" '
    $0 ~ "^" parent ":[[:space:]]*$" {
      in_parent=1
      next
    }
    in_parent && $0 ~ "^[^[:space:]]" {
      in_parent=0
    }
    in_parent && $0 ~ "^[[:space:]]+" key ":[[:space:]]*" {
      sub("^[[:space:]]*" key ":[[:space:]]*", "", $0)
      print
      exit
    }
  ' "$yaml_file"
}

read_yaml_first_list_item() {
  yaml_file=$1
  yaml_key=$2
  awk -v key="$yaml_key" '
    $0 ~ "^" key ":[[:space:]]*$" {
      in_list=1
      next
    }
    in_list && $0 ~ "^[^[:space:]]" {
      in_list=0
    }
    in_list && $0 ~ "^[[:space:]]*-[[:space:]]*" {
      sub("^[[:space:]]*-[[:space:]]*", "", $0)
      print
      exit
    }
  ' "$yaml_file"
}

read_compose_scalar() {
  compose_file=$1
  compose_key=$2
  awk -v key="$compose_key" '
    $0 ~ "^[[:space:]]*" key ":[[:space:]]*" {
      sub("^[[:space:]]*" key ":[[:space:]]*", "", $0)
      print
      exit
    }
  ' "$compose_file"
}

read_compose_port_mapping() {
  compose_file=$1
  awk '
    /^[[:space:]]*ports:[[:space:]]*$/ {
      in_ports=1
      next
    }
    in_ports && /^[[:space:]]*-[[:space:]]*/ {
      line=$0
      sub(/^[[:space:]]*-[[:space:]]*/, "", line)
      print line
      exit
    }
    in_ports && /^[^[:space:]]/ {
      in_ports=0
    }
  ' "$compose_file"
}

sync_runtime_state_from_files() {
  if [ -f "$CPA_COMPOSE_FILE" ]; then
    current_image=$(normalize_scalar "$(read_compose_scalar "$CPA_COMPOSE_FILE" "image")")
    current_container_name=$(normalize_scalar "$(read_compose_scalar "$CPA_COMPOSE_FILE" "container_name")")
    current_port_mapping=$(normalize_scalar "$(read_compose_port_mapping "$CPA_COMPOSE_FILE")")

    if [ -n "$current_image" ]; then
      CPA_IMAGE=$current_image
    fi
    if [ -n "$current_container_name" ]; then
      CPA_CONTAINER_NAME=$current_container_name
    fi
    if [ -n "$current_port_mapping" ]; then
      case "$current_port_mapping" in
        *:*:*)
          current_container_port=${current_port_mapping##*:}
          current_host_mapping=${current_port_mapping%:*}
          current_host_port=${current_host_mapping##*:}
          current_bind_host=${current_host_mapping%:*}
          if validate_port "$current_host_port"; then
            CPA_HOST_PORT=$current_host_port
          fi
          if [ -n "$current_bind_host" ]; then
            CPA_BIND_HOST=$current_bind_host
          fi
          if validate_port "$current_container_port"; then
            CPA_CONTAINER_PORT=$current_container_port
          fi
          ;;
        *:*)
          current_container_port=${current_port_mapping##*:}
          current_host_port=${current_port_mapping%:*}
          if validate_port "$current_host_port"; then
            CPA_HOST_PORT=$current_host_port
          fi
          if validate_port "$current_container_port"; then
            CPA_CONTAINER_PORT=$current_container_port
          fi
          ;;
      esac
    fi
  fi

  if [ -f "$CPA_CONFIG_FILE" ]; then
    current_api_key=$(normalize_scalar "$(read_yaml_first_list_item "$CPA_CONFIG_FILE" "api-keys")")
    current_management_secret=$(normalize_scalar "$(read_yaml_nested_value "$CPA_CONFIG_FILE" "remote-management" "secret-key")")
    current_allow_remote_management=$(normalize_scalar "$(read_yaml_nested_value "$CPA_CONFIG_FILE" "remote-management" "allow-remote")")
    current_disable_control_panel=$(normalize_scalar "$(read_yaml_nested_value "$CPA_CONFIG_FILE" "remote-management" "disable-control-panel")")
    current_debug=$(normalize_scalar "$(read_yaml_top_level_value "$CPA_CONFIG_FILE" "debug")")
    current_usage_statistics_enabled=$(normalize_scalar "$(read_yaml_top_level_value "$CPA_CONFIG_FILE" "usage-statistics-enabled")")
    current_request_retry=$(normalize_scalar "$(read_yaml_top_level_value "$CPA_CONFIG_FILE" "request-retry")")

    if [ -n "$current_api_key" ]; then
      CPA_API_KEY=$current_api_key
    fi
    if [ -n "$current_management_secret" ]; then
      if is_bcrypt_hash "$current_management_secret"; then
        if [ -z "$CPA_MANAGEMENT_SECRET" ]; then
          CPA_MANAGEMENT_SECRET_HASHED_ONLY=1
        fi
      else
        CPA_MANAGEMENT_SECRET=$current_management_secret
        CPA_MANAGEMENT_SECRET_HASHED_ONLY=0
      fi
    fi
    case "$current_allow_remote_management" in
      true|false)
        CPA_ALLOW_REMOTE_MANAGEMENT=$current_allow_remote_management
        ;;
    esac
    case "$current_disable_control_panel" in
      true|false)
        CPA_DISABLE_CONTROL_PANEL=$current_disable_control_panel
        ;;
    esac
    case "$current_debug" in
      true|false)
        CPA_DEBUG=$current_debug
        ;;
    esac
    case "$current_usage_statistics_enabled" in
      true|false)
        CPA_USAGE_STATISTICS_ENABLED=$current_usage_statistics_enabled
        ;;
    esac
    if validate_non_negative_int "$current_request_retry"; then
      CPA_REQUEST_RETRY=$current_request_retry
    fi
  fi
}

import_existing_installation() {
  if [ ! -f "$CPA_COMPOSE_FILE" ] && [ ! -f "$CPA_CONFIG_FILE" ]; then
    return 1
  fi

  sync_runtime_state_from_files
  save_cpa_env
  log "检测到已有部署，已从现有文件导入脚本配置: $CPA_ENV_FILE"
  return 0
}

save_cpa_env() {
  mkdir -p "$CPA_BASE_DIR"
  cat >"$CPA_ENV_FILE" <<EOF
CPA_BASE_DIR='$(escape_squotes "$CPA_BASE_DIR")'
CPA_IMAGE='$(escape_squotes "$CPA_IMAGE")'
CPA_CONTAINER_NAME='$(escape_squotes "$CPA_CONTAINER_NAME")'
CPA_BIND_HOST='$(escape_squotes "$CPA_BIND_HOST")'
CPA_HOST_PORT='$(escape_squotes "$CPA_HOST_PORT")'
CPA_API_KEY='$(escape_squotes "$CPA_API_KEY")'
CPA_MANAGEMENT_SECRET='$(escape_squotes "$CPA_MANAGEMENT_SECRET")'
CPA_ALLOW_REMOTE_MANAGEMENT='$(escape_squotes "$CPA_ALLOW_REMOTE_MANAGEMENT")'
CPA_DISABLE_CONTROL_PANEL='$(escape_squotes "$CPA_DISABLE_CONTROL_PANEL")'
CPA_DEBUG='$(escape_squotes "$CPA_DEBUG")'
CPA_USAGE_STATISTICS_ENABLED='$(escape_squotes "$CPA_USAGE_STATISTICS_ENABLED")'
CPA_REQUEST_RETRY='$(escape_squotes "$CPA_REQUEST_RETRY")'
EOF
}

set_cpa_defaults() {
  CPA_BASE_DIR=${CPA_BASE_DIR:-"$DEFAULT_CPA_BASE_DIR"}
  CPA_BASE_DIR=$(normalize_workspace_path "$CPA_BASE_DIR")
  CPA_IMAGE=${CPA_IMAGE:-"eceasy/cli-proxy-api:latest"}
  CPA_CONTAINER_NAME=${CPA_CONTAINER_NAME:-"cpa"}
  CPA_BIND_HOST=${CPA_BIND_HOST:-"127.0.0.1"}
  CPA_HOST_PORT=${CPA_HOST_PORT:-"8317"}
  CPA_API_KEY=${CPA_API_KEY:-"$(generate_api_key)"}
  CPA_MANAGEMENT_SECRET=${CPA_MANAGEMENT_SECRET:-""}
  CPA_ALLOW_REMOTE_MANAGEMENT=${CPA_ALLOW_REMOTE_MANAGEMENT:-"true"}
  CPA_DISABLE_CONTROL_PANEL=${CPA_DISABLE_CONTROL_PANEL:-"false"}
  CPA_DEBUG=${CPA_DEBUG:-"false"}
  CPA_USAGE_STATISTICS_ENABLED=${CPA_USAGE_STATISTICS_ENABLED:-"false"}
  CPA_REQUEST_RETRY=${CPA_REQUEST_RETRY:-"3"}
  normalize_management_secret_runtime
}

update_cpa_paths() {
  CPA_DATA_DIR="$CPA_BASE_DIR/data"
  CPA_COMPOSE_FILE="$CPA_BASE_DIR/docker-compose.yml"
  CPA_CONFIG_FILE="$CPA_DATA_DIR/config.yaml"
  CPA_ENV_FILE="$CPA_BASE_DIR/cpa-install.env"
  CPA_LOG_FILE=${CPA_LOG_FILE:-"$CPA_BASE_DIR/cpa-operation.log"}
  CPA_LOG_FILE=$(normalize_workspace_path "$CPA_LOG_FILE")
}

load_cpa_env() {
  set_cpa_defaults
  update_cpa_paths

  if [ -f "$CPA_ENV_FILE" ]; then
    # shellcheck disable=SC1090
    . "$CPA_ENV_FILE"
  fi

  set_cpa_defaults
  update_cpa_paths
  sync_runtime_state_from_files
}

usage() {
  cat <<EOF
用法:
  sh $SCRIPT_NAME                 打开交互式菜单
  sh $SCRIPT_NAME install         首次安装并启动
  sh $SCRIPT_NAME update          拉取镜像、同步配置并重建已有部署
  sh $SCRIPT_NAME repair          接管或修复已有部署，不重启服务
  sh $SCRIPT_NAME start           启动容器
  sh $SCRIPT_NAME stop            停止容器
  sh $SCRIPT_NAME restart         重启容器
  sh $SCRIPT_NAME status          查看状态
  sh $SCRIPT_NAME info            查看访问与密钥信息
  sh $SCRIPT_NAME logs            查看日志
  sh $SCRIPT_NAME configure       打开配置向导
  sh $SCRIPT_NAME doctor          执行环境检查
  sh $SCRIPT_NAME login           打开认证登录菜单
  sh $SCRIPT_NAME uninstall       卸载容器与网络，保留数据
  sh $SCRIPT_NAME purge           彻底删除容器、配置与数据

脚本外部环境变量统一使用 CPA_ 前缀，例如:
  CPA_BASE_DIR
  CPA_IMAGE
  CPA_CONTAINER_NAME
  CPA_BIND_HOST
  CPA_HOST_PORT
  CPA_API_KEY
  CPA_MANAGEMENT_SECRET

持久化配置文件:
  $DEFAULT_CPA_BASE_DIR/cpa-install.env

运行日志文件:
  $DEFAULT_CPA_BASE_DIR/cpa-operation.log
EOF
}

compose() {
  if docker compose version >/dev/null 2>&1; then
    docker compose -f "$CPA_COMPOSE_FILE" "$@"
    return
  fi

  if command -v docker-compose >/dev/null 2>&1; then
    docker-compose -f "$CPA_COMPOSE_FILE" "$@"
    return
  fi

  fail "未检测到 docker compose 或 docker-compose"
}

random_token() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 16
    return
  fi

  tr -dc 'A-Za-z0-9' </dev/urandom | head -c 32
}

resolve_cpa_ops_command() {
  if [ -x "$SCRIPT_DIR/cpa-ops" ]; then
    printf '%s\n' "$SCRIPT_DIR/cpa-ops"
    return 0
  fi
  if command -v cpa-ops >/dev/null 2>&1; then
    command -v cpa-ops
    return 0
  fi
  return 1
}

generate_project_secret() {
  secret_kind=$1
  if cpa_ops_command=$(resolve_cpa_ops_command 2>/dev/null); then
    generated_secret=$("$cpa_ops_command" generate-secret --kind "$secret_kind" 2>/dev/null || true)
    generated_secret=$(trim_spaces "$generated_secret")
    if [ -n "$generated_secret" ]; then
      printf '%s' "$generated_secret"
      return 0
    fi
  fi
  if command -v go >/dev/null 2>&1 && [ -f "$SCRIPT_DIR/go.mod" ]; then
    generated_secret=$(cd "$SCRIPT_DIR" && go run ./cmd/cpa-ops generate-secret --kind "$secret_kind" 2>/dev/null || true)
    generated_secret=$(trim_spaces "$generated_secret")
    if [ -n "$generated_secret" ]; then
      printf '%s' "$generated_secret"
      return 0
    fi
  fi
  return 1
}

generate_api_key() {
  if generated_secret=$(generate_project_secret api); then
    printf '%s' "$generated_secret"
    return 0
  fi
  printf 'sk-%s' "$(random_token)"
}

generate_management_secret() {
  if generated_secret=$(generate_project_secret management); then
    printf '%s' "$generated_secret"
    return 0
  fi
  printf 'MGT-%s' "$(random_token)"
}

normalize_management_secret_runtime() {
  CPA_MANAGEMENT_SECRET=$(trim_spaces "$CPA_MANAGEMENT_SECRET")
  if [ -n "$CPA_MANAGEMENT_SECRET" ] && is_bcrypt_hash "$CPA_MANAGEMENT_SECRET"; then
    CPA_MANAGEMENT_SECRET=""
    CPA_MANAGEMENT_SECRET_HASHED_ONLY=1
    return
  fi
  if [ -n "$CPA_MANAGEMENT_SECRET" ]; then
    CPA_MANAGEMENT_SECRET_HASHED_ONLY=0
  fi
}

ensure_plain_management_secret_for_sync() {
  if [ -n "$CPA_MANAGEMENT_SECRET" ]; then
    return
  fi
  if [ "$CPA_MANAGEMENT_SECRET_HASHED_ONLY" -eq 1 ]; then
    fail "当前部署仅检测到 bcrypt 哈希，无法回读原始管理密钥。请先执行 'sh $SCRIPT_NAME configure' 重新设置，或在命令前显式传入 CPA_MANAGEMENT_SECRET='新的明文密钥'。"
  fi
  fail "未设置 CPA_MANAGEMENT_SECRET，请先执行 'sh $SCRIPT_NAME configure' 或在命令前传入 CPA_MANAGEMENT_SECRET。"
}

ensure_prereqs() {
  need_cmd docker
  compose version >/dev/null 2>&1 || fail "Docker Compose 不可用"
}

ensure_dirs() {
  mkdir -p "$CPA_DATA_DIR"
}

prompt_default() {
  prompt_label=$1
  prompt_default_value=$2
  printf '%s [%s]: ' "$prompt_label" "$prompt_default_value" >&2
  read -r prompt_value
  if [ -z "$prompt_value" ]; then
    printf '%s' "$prompt_default_value"
  else
    printf '%s' "$prompt_value"
  fi
}

prompt_yes_no() {
  prompt_label=$1
  prompt_default_value=$2
  printf '%s [%s] (true/false): ' "$prompt_label" "$prompt_default_value" >&2
  read -r prompt_value
  if [ -z "$prompt_value" ]; then
    prompt_value=$prompt_default_value
  fi

  case "$prompt_value" in
    true|false)
      printf '%s' "$prompt_value"
      ;;
    *)
      fail "请输入 true 或 false"
      ;;
  esac
}

validate_port() {
  case "$1" in
    ''|*[!0-9]*)
      return 1
      ;;
    *)
      if [ "$1" -lt 1 ] || [ "$1" -gt 65535 ]; then
        return 1
      fi
      return 0
      ;;
  esac
}

validate_non_empty() {
  [ -n "$1" ]
}

validate_non_negative_int() {
  case "$1" in
    ''|*[!0-9]*)
      return 1
      ;;
    *)
      return 0
      ;;
  esac
}

configure_settings() {
  log ""
  log "===================================="
  log " CLIProxyAPI Docker 运维配置向导"
  log "===================================="
  log "说明:"
  log "1. 这里填写的是脚本自己的运维参数，全部用 CPA_ 前缀持久化。"
  log "2. 脚本会同步 docker-compose.yml，并把受脚本管理的字段写回挂载的 config.yaml。"
  log "3. 已有 config.yaml 中未由脚本管理的字段会保留，避免更新时被整文件覆盖。"
  log ""

  new_base_dir=$(prompt_default "CPA_BASE_DIR 部署目录" "$CPA_BASE_DIR")
  new_image=$(prompt_default "CPA_IMAGE 镜像" "$CPA_IMAGE")
  new_container_name=$(prompt_default "CPA_CONTAINER_NAME 容器名" "$CPA_CONTAINER_NAME")
  new_bind_host=$(prompt_default "CPA_BIND_HOST 宿主机绑定地址" "$CPA_BIND_HOST")
  new_host_port=$(prompt_default "CPA_HOST_PORT 宿主机端口" "$CPA_HOST_PORT")
  new_api_key=$(prompt_default "CPA_API_KEY 代理访问密钥" "$CPA_API_KEY")
  if [ "$CPA_MANAGEMENT_SECRET_HASHED_ONLY" -eq 1 ]; then
    warn "当前仅检测到 bcrypt 哈希，旧的原始管理密钥无法回读，请重新设置新的管理密钥。"
  fi
  management_secret_default=$CPA_MANAGEMENT_SECRET
  if [ -z "$management_secret_default" ]; then
    management_secret_default=$(generate_management_secret)
  fi
  new_management_secret=$(prompt_default "CPA_MANAGEMENT_SECRET WebUI 管理密钥" "$management_secret_default")
  new_allow_remote_management=$(prompt_yes_no "CPA_ALLOW_REMOTE_MANAGEMENT 是否允许远程管理" "$CPA_ALLOW_REMOTE_MANAGEMENT")
  new_disable_control_panel=$(prompt_yes_no "CPA_DISABLE_CONTROL_PANEL 是否禁用内置控制面板" "$CPA_DISABLE_CONTROL_PANEL")
  new_debug=$(prompt_yes_no "CPA_DEBUG 是否开启调试日志" "$CPA_DEBUG")
  new_usage_statistics_enabled=$(prompt_yes_no "CPA_USAGE_STATISTICS_ENABLED 是否允许匿名统计" "$CPA_USAGE_STATISTICS_ENABLED")
  new_request_retry=$(prompt_default "CPA_REQUEST_RETRY 请求失败重试次数" "$CPA_REQUEST_RETRY")

  validate_non_empty "$new_base_dir" || fail "部署目录不能为空"
  validate_non_empty "$new_image" || fail "镜像不能为空"
  validate_non_empty "$new_container_name" || fail "容器名不能为空"
  validate_non_empty "$new_bind_host" || fail "绑定地址不能为空"
  validate_port "$new_host_port" || fail "宿主机端口不合法: $new_host_port"
  validate_non_empty "$new_api_key" || fail "代理访问密钥不能为空"
  validate_non_empty "$new_management_secret" || fail "WebUI 管理密钥不能为空"
  validate_non_negative_int "$new_request_retry" || fail "重试次数必须是非负整数"

  CPA_BASE_DIR=$new_base_dir
  CPA_IMAGE=$new_image
  CPA_CONTAINER_NAME=$new_container_name
  CPA_BIND_HOST=$new_bind_host
  CPA_HOST_PORT=$new_host_port
  CPA_API_KEY=$new_api_key
  CPA_MANAGEMENT_SECRET=$new_management_secret
  CPA_MANAGEMENT_SECRET_HASHED_ONLY=0
  CPA_ALLOW_REMOTE_MANAGEMENT=$new_allow_remote_management
  CPA_DISABLE_CONTROL_PANEL=$new_disable_control_panel
  CPA_DEBUG=$new_debug
  CPA_USAGE_STATISTICS_ENABLED=$new_usage_statistics_enabled
  CPA_REQUEST_RETRY=$new_request_retry

  update_cpa_paths
  save_cpa_env
  ensure_dirs
  write_compose
  sync_config

  log ""
  log "配置已保存到: $CPA_ENV_FILE"
  log "Compose 与挂载的 config.yaml 已同步更新。"
  log "如果服务正在运行，部分配置需要执行 restart 才会完全生效。"
  show_access_info
}

ensure_initialized() {
  if [ -f "$CPA_ENV_FILE" ]; then
    return
  fi
  if import_existing_installation; then
    return
  fi
  configure_settings
}

has_existing_installation() {
  if [ -f "$CPA_ENV_FILE" ] || [ -f "$CPA_COMPOSE_FILE" ] || [ -f "$CPA_CONFIG_FILE" ]; then
    return 0
  fi
  return 1
}

ensure_fresh_install_target() {
  if has_existing_installation; then
    fail "检测到已有部署或配置文件，请改用 update 或 repair，避免 install 覆盖现有环境"
  fi
}

ensure_updatable_installation() {
  if [ -f "$CPA_ENV_FILE" ]; then
    return
  fi
  if import_existing_installation; then
    return
  fi
  fail "未检测到可更新的部署，请先执行 install"
}

write_compose() {
  cat >"$CPA_COMPOSE_FILE" <<EOF
services:
  cpa:
    image: ${CPA_IMAGE}
    container_name: ${CPA_CONTAINER_NAME}
    restart: unless-stopped
    command: ["/CLIProxyAPI/CLIProxyAPI", "--config", "/data/config.yaml"]
    environment:
      DEPLOY: cloud
    ports:
      - "${CPA_BIND_HOST}:${CPA_HOST_PORT}:${CPA_CONTAINER_PORT}"
    volumes:
      - ${CPA_DATA_DIR}:/data
EOF
}

write_initial_config() {
  cat >"$CPA_CONFIG_FILE" <<EOF
port: ${CPA_CONTAINER_PORT}
remote-management:
  allow-remote: ${CPA_ALLOW_REMOTE_MANAGEMENT}
  secret-key: "${CPA_MANAGEMENT_SECRET}"
  disable-control-panel: ${CPA_DISABLE_CONTROL_PANEL}
auth-dir: "${CPA_AUTH_DIR}"
debug: ${CPA_DEBUG}
logging-to-file: false
usage-statistics-enabled: ${CPA_USAGE_STATISTICS_ENABLED}
request-retry: ${CPA_REQUEST_RETRY}
quota-exceeded:
  switch-project: true
  switch-preview-model: true
api-keys:
  - "${CPA_API_KEY}"
EOF
}

merge_config() {
  backup_file="$CPA_CONFIG_FILE.bak"
  temp_file=$(mktemp "${CPA_CONFIG_FILE}.XXXXXX") || fail "无法创建临时文件"

  cp "$CPA_CONFIG_FILE" "$backup_file"

  awk \
    -v port="$CPA_CONTAINER_PORT" \
    -v allowRemote="$CPA_ALLOW_REMOTE_MANAGEMENT" \
    -v managementSecret="$CPA_MANAGEMENT_SECRET" \
    -v disableControlPanel="$CPA_DISABLE_CONTROL_PANEL" \
    -v authDir="$CPA_AUTH_DIR" \
    -v debug="$CPA_DEBUG" \
    -v usageStatisticsEnabled="$CPA_USAGE_STATISTICS_ENABLED" \
    -v requestRetry="$CPA_REQUEST_RETRY" \
    -v apiKey="$CPA_API_KEY" '
    function flush_remote_management() {
      if (!allow_remote_seen) {
        print "  allow-remote: " allowRemote
      }
      if (!secret_key_seen) {
        print "  secret-key: \"" managementSecret "\""
      }
      if (!disable_control_panel_seen) {
        print "  disable-control-panel: " disableControlPanel
      }
    }

    function flush_api_keys() {
      if (!api_key_written) {
        print "  - \"" apiKey "\""
      }
    }

    {
      line=$0

      if (in_remote_management && line ~ /^[^[:space:]]/) {
        flush_remote_management()
        in_remote_management=0
      }

      if (in_api_keys && line ~ /^[^[:space:]]/) {
        flush_api_keys()
        in_api_keys=0
      }

      if (in_remote_management) {
        if (line ~ /^[[:space:]]+allow-remote:[[:space:]]*/) {
          print "  allow-remote: " allowRemote
          allow_remote_seen=1
          next
        }
        if (line ~ /^[[:space:]]+secret-key:[[:space:]]*/) {
          print "  secret-key: \"" managementSecret "\""
          secret_key_seen=1
          next
        }
        if (line ~ /^[[:space:]]+disable-control-panel:[[:space:]]*/) {
          print "  disable-control-panel: " disableControlPanel
          disable_control_panel_seen=1
          next
        }
        print line
        next
      }

      if (in_api_keys) {
        if (line ~ /^[[:space:]]*-[[:space:]]*/) {
          if (!api_key_written) {
            print "  - \"" apiKey "\""
            api_key_written=1
          } else {
            print line
          }
          next
        }
        print line
        next
      }

      if (line ~ /^port:[[:space:]]*/) {
        print "port: " port
        port_seen=1
        next
      }
      if (line ~ /^remote-management:[[:space:]]*$/) {
        print "remote-management:"
        remote_management_seen=1
        in_remote_management=1
        next
      }
      if (line ~ /^auth-dir:[[:space:]]*/) {
        print "auth-dir: \"" authDir "\""
        auth_dir_seen=1
        next
      }
      if (line ~ /^debug:[[:space:]]*/) {
        print "debug: " debug
        debug_seen=1
        next
      }
      if (line ~ /^usage-statistics-enabled:[[:space:]]*/) {
        print "usage-statistics-enabled: " usageStatisticsEnabled
        usage_statistics_seen=1
        next
      }
      if (line ~ /^request-retry:[[:space:]]*/) {
        print "request-retry: " requestRetry
        request_retry_seen=1
        next
      }
      if (line ~ /^api-keys:[[:space:]]*$/) {
        print "api-keys:"
        api_keys_seen=1
        in_api_keys=1
        next
      }

      print line
    }

    END {
      if (in_remote_management) {
        flush_remote_management()
      }
      if (!remote_management_seen) {
        print "remote-management:"
        print "  allow-remote: " allowRemote
        print "  secret-key: \"" managementSecret "\""
        print "  disable-control-panel: " disableControlPanel
      }
      if (!auth_dir_seen) {
        print "auth-dir: \"" authDir "\""
      }
      if (!port_seen) {
        print "port: " port
      }
      if (!debug_seen) {
        print "debug: " debug
      }
      if (!usage_statistics_seen) {
        print "usage-statistics-enabled: " usageStatisticsEnabled
      }
      if (!request_retry_seen) {
        print "request-retry: " requestRetry
      }
      if (in_api_keys) {
        flush_api_keys()
      }
      if (!api_keys_seen) {
        print "api-keys:"
        print "  - \"" apiKey "\""
      }
    }
  ' "$CPA_CONFIG_FILE" >"$temp_file"

  mv "$temp_file" "$CPA_CONFIG_FILE"
}

sync_config() {
  ensure_plain_management_secret_for_sync
  if [ -f "$CPA_CONFIG_FILE" ]; then
    merge_config
    log "已同步已有 config.yaml，未托管字段保持不变。"
    return
  fi

  write_initial_config
  log "已生成新的 config.yaml。"
}

backup_current_files() {
  backup_stamp=$(date '+%Y%m%d-%H%M%S')
  backup_dir="$CPA_BASE_DIR/backups/$backup_stamp"
  copied_any=0

  mkdir -p "$backup_dir"

  if [ -f "$CPA_ENV_FILE" ]; then
    cp -p "$CPA_ENV_FILE" "$backup_dir/"
    copied_any=1
  fi
  if [ -f "$CPA_COMPOSE_FILE" ]; then
    cp -p "$CPA_COMPOSE_FILE" "$backup_dir/"
    copied_any=1
  fi
  if [ -f "$CPA_CONFIG_FILE" ]; then
    cp -p "$CPA_CONFIG_FILE" "$backup_dir/"
    copied_any=1
  fi

  if [ "$copied_any" -eq 1 ]; then
    log "已备份当前配置到: $backup_dir"
  else
    rmdir "$backup_dir" 2>/dev/null || true
    log "当前没有可备份的配置文件，跳过备份。"
  fi
}

show_generated_files() {
  section "生成的 Compose 文件"
  cat "$CPA_COMPOSE_FILE"
  section "生成的 config.yaml"
  cat "$CPA_CONFIG_FILE"
}

show_access_info() {
  display_management_secret=$CPA_MANAGEMENT_SECRET
  if [ "$CPA_MANAGEMENT_SECRET_HASHED_ONLY" -eq 1 ]; then
    display_management_secret='[当前仅检测到哈希值，请重新设置管理密钥]'
  elif is_bcrypt_hash "$CPA_MANAGEMENT_SECRET"; then
    display_management_secret='[已哈希保存，原始管理密钥不可回显]'
  fi

  log ""
  log "================== 当前配置 =================="
  log "部署目录: $CPA_BASE_DIR"
  log "数据目录: $CPA_DATA_DIR"
  log "Compose 文件: $CPA_COMPOSE_FILE"
  log "配置文件: $CPA_CONFIG_FILE"
  log "日志文件: $CPA_LOG_FILE"
  log "镜像: $CPA_IMAGE"
  log "容器名: $CPA_CONTAINER_NAME"
  log "绑定地址: $CPA_BIND_HOST"
  log "宿主机端口: $CPA_HOST_PORT"
  log "容器端口: $CPA_CONTAINER_PORT"
  log "代理 API 入口: http://<你的服务器IP>:${CPA_HOST_PORT}"
  log "WebUI 入口: http://<你的服务器IP>:${CPA_HOST_PORT}/management.html"
  log "CPA_API_KEY: $CPA_API_KEY"
  log "CPA_MANAGEMENT_SECRET: $display_management_secret"
  log "允许远程管理: $CPA_ALLOW_REMOTE_MANAGEMENT"
  log "禁用控制面板: $CPA_DISABLE_CONTROL_PANEL"
  log "调试日志: $CPA_DEBUG"
  log "匿名统计: $CPA_USAGE_STATISTICS_ENABLED"
  log "请求重试: $CPA_REQUEST_RETRY"
  log "官方文档: https://help.router-for.me/docker/docker-compose"
  log "WebUI 文档: https://help.router-for.me/management/webui"
}

doctor() {
  load_cpa_env
  log ""
  log "================== 环境检查 =================="

  if command -v docker >/dev/null 2>&1; then
    log "Docker: 已安装"
  else
    warn "Docker: 未安装"
  fi

  if docker compose version >/dev/null 2>&1; then
    log "Docker Compose: docker compose 可用"
  elif command -v docker-compose >/dev/null 2>&1; then
    log "Docker Compose: docker-compose 可用"
  else
    warn "Docker Compose: 不可用"
  fi

  if [ -f "$CPA_ENV_FILE" ]; then
    log "运维配置: 已存在 $CPA_ENV_FILE"
  else
    warn "运维配置: 未初始化"
  fi

  if [ -f "$CPA_CONFIG_FILE" ]; then
    log "应用配置: 已存在 $CPA_CONFIG_FILE"
  else
    warn "应用配置: 尚未生成"
  fi

  if [ -f "$CPA_COMPOSE_FILE" ]; then
    log "Compose 文件: 已存在 $CPA_COMPOSE_FILE"
  else
    warn "Compose 文件: 尚未生成"
  fi

  if command -v ss >/dev/null 2>&1; then
    if ss -lnt "( sport = :$CPA_HOST_PORT )" 2>/dev/null | awk 'NR>1 {found=1} END {exit found ? 0 : 1}'; then
      warn "端口检查: $CPA_HOST_PORT 已被占用"
    else
      log "端口检查: $CPA_HOST_PORT 当前空闲"
    fi
  else
    warn "端口检查: 系统缺少 ss，跳过"
  fi

  if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -Fx "$CPA_CONTAINER_NAME" >/dev/null 2>&1; then
    log "容器检查: $CPA_CONTAINER_NAME 已存在"
  else
    warn "容器检查: $CPA_CONTAINER_NAME 不存在"
  fi
}

install_service() {
  section "开始安装"
  ensure_prereqs
  ensure_fresh_install_target
  configure_settings

  section "准备目录"
  log "部署目录: $CPA_BASE_DIR"
  log "数据目录: $CPA_DATA_DIR"
  ensure_dirs

  section "生成配置文件"
  write_compose
  sync_config
  show_generated_files

  section "拉取镜像"
  compose pull

  section "启动容器"
  compose up -d

  section "安装完成"
  compose ps -a

  log ""
  log "CLIProxyAPI 已完成首次安装并启动。"
  show_access_info
  log ""
  log "下一步建议:"
  log "1. 先访问 WebUI: http://<你的服务器IP>:${CPA_HOST_PORT}/management.html"
  log "2. 再用登录菜单完成 OpenAI / Claude / Gemini 等认证"
}

update_service() {
  section "开始更新"
  ensure_prereqs
  ensure_updatable_installation

  section "准备目录"
  log "部署目录: $CPA_BASE_DIR"
  log "数据目录: $CPA_DATA_DIR"
  ensure_dirs

  section "备份当前配置"
  backup_current_files

  section "同步配置文件"
  write_compose
  sync_config
  show_generated_files

  section "拉取镜像"
  compose pull

  section "重建容器"
  compose up -d

  section "更新完成"
  compose ps -a

  log ""
  log "CLIProxyAPI 已完成更新并重建。"
  show_access_info
}

repair_installation() {
  section "开始修复"

  if [ -f "$CPA_ENV_FILE" ]; then
    log "已存在脚本配置，跳过导入。"
  elif import_existing_installation; then
    :
  else
    fail "未检测到可接管的已有部署，请先执行 install"
  fi

  section "同步本地文件"
  ensure_dirs
  backup_current_files
  write_compose
  sync_config
  show_generated_files

  section "修复完成"
  log "已完成已有部署接管与文件修复，本次未重启服务。"
  log "如需让新配置完全生效，请手动执行:"
  log "  sh $SCRIPT_NAME restart"
  log "或执行:"
  log "  sh $SCRIPT_NAME update"
}

start_service() {
  ensure_prereqs
  ensure_initialized
  [ -f "$CPA_COMPOSE_FILE" ] || fail "未找到 $CPA_COMPOSE_FILE，请先执行 install"
  section "启动服务"
  log "使用 Compose 文件: $CPA_COMPOSE_FILE"
  compose up -d
  section "当前容器状态"
  compose ps -a
  log "已启动。"
}

stop_service() {
  ensure_prereqs
  ensure_initialized
  [ -f "$CPA_COMPOSE_FILE" ] || fail "未找到 $CPA_COMPOSE_FILE，请先执行 install"
  section "停止服务"
  log "使用 Compose 文件: $CPA_COMPOSE_FILE"
  compose stop
  section "当前容器状态"
  compose ps -a
  log "已停止。"
}

restart_service() {
  ensure_prereqs
  ensure_initialized
  [ -f "$CPA_COMPOSE_FILE" ] || fail "未找到 $CPA_COMPOSE_FILE，请先执行 install"
  section "重启服务"
  log "使用 Compose 文件: $CPA_COMPOSE_FILE"
  compose restart
  section "当前容器状态"
  compose ps -a
  log "已重启。"
}

show_status() {
  ensure_prereqs
  ensure_initialized
  [ -f "$CPA_COMPOSE_FILE" ] || fail "未找到 $CPA_COMPOSE_FILE，请先执行 install"
  section "容器状态"
  compose ps -a
  show_access_info
}

show_logs() {
  ensure_prereqs
  ensure_initialized
  [ -f "$CPA_COMPOSE_FILE" ] || fail "未找到 $CPA_COMPOSE_FILE，请先执行 install"
  section "查看日志"
  log "使用 Compose 文件: $CPA_COMPOSE_FILE"
  compose logs --tail=200 -f
}

run_login_cmd() {
  login_title=$1
  login_flag=$2
  ensure_prereqs
  ensure_initialized
  [ -f "$CPA_COMPOSE_FILE" ] || fail "未找到 $CPA_COMPOSE_FILE，请先执行 install"
  section "${login_title} 登录"
  log "执行命令: docker exec -it $CPA_CONTAINER_NAME /CLIProxyAPI/CLIProxyAPI -no-browser $login_flag"
  docker exec -it "$CPA_CONTAINER_NAME" /CLIProxyAPI/CLIProxyAPI -no-browser "$login_flag"
}

login_menu() {
  while true; do
    log ""
    log "============== 认证登录菜单 =============="
    log "1. OpenAI / Codex 登录"
    log "2. Claude 登录"
    log "3. Gemini 登录"
    log "4. Qwen 登录"
    log "5. iFlow 登录"
    log "0. 返回上一级"
    printf '请选择操作: '
    read -r login_choice
    case "$login_choice" in
      1)
        run_login_cmd "OpenAI / Codex" "--codex-login"
        ;;
      2)
        run_login_cmd "Claude" "--claude-login"
        ;;
      3)
        run_login_cmd "Gemini" "--login"
        ;;
      4)
        run_login_cmd "Qwen" "--qwen-login"
        ;;
      5)
        run_login_cmd "iFlow" "--iflow-login"
        ;;
      0)
        return
        ;;
      *)
        log "无效选择，请重试。"
        ;;
    esac
  done
}

uninstall_service() {
  ensure_prereqs
  ensure_initialized
  printf '⚠️ 危险操作检测！\n'
  printf '操作类型：[卸载 CPA Docker 容器]\n'
  printf '影响范围：[删除容器和网络，但保留本地配置与认证数据 %s]\n' "$CPA_DATA_DIR"
  printf '风险评估：[服务会停止，后续可通过 start 重新拉起]\n\n'
  printf '请确认是否继续？[需要明确的“确认”]: '
  read -r answer
  [ "$answer" = "确认" ] || fail "已取消。"

  if [ ! -f "$CPA_COMPOSE_FILE" ]; then
    log "未找到 compose 文件，跳过容器卸载。"
    return
  fi

  section "卸载容器"
  log "使用 Compose 文件: $CPA_COMPOSE_FILE"
  compose down --remove-orphans
  section "卸载结果"
  compose ps -a || true
  log "已卸载容器和网络，数据仍保留在: $CPA_DATA_DIR"
}

purge_all() {
  ensure_prereqs
  ensure_initialized
  printf '⚠️ 危险操作检测！\n'
  printf '操作类型：[彻底删除 CPA Docker 部署]\n'
  printf '影响范围：[删除容器、网络、compose 文件、配置文件、认证文件与本地数据目录 %s]\n' "$CPA_BASE_DIR"
  printf '风险评估：[所有本地配置、API Key、OAuth 认证状态将不可恢复]\n\n'
  printf '请确认是否继续？[需要明确的“PURGE”]: '
  read -r answer
  [ "$answer" = "PURGE" ] || fail "已取消。"

  if [ -f "$CPA_COMPOSE_FILE" ]; then
    section "删除容器与网络"
    log "使用 Compose 文件: $CPA_COMPOSE_FILE"
    compose down --remove-orphans
  fi

  section "删除本地数据目录"
  log "执行命令: rm -rf $CPA_BASE_DIR"
  rm -rf "$CPA_BASE_DIR"
  log "已彻底删除: $CPA_BASE_DIR"
}

show_menu() {
  log ""
  log "=================================="
  log " CLIProxyAPI Docker 运维工厂菜单"
  log "=================================="
  log "部署目录: $CPA_BASE_DIR"
  log "容器名: $CPA_CONTAINER_NAME"
  log "镜像: $CPA_IMAGE"
  log "绑定地址: $CPA_BIND_HOST"
  log "宿主机端口: $CPA_HOST_PORT"
  log ""
  log "1. 首次安装并启动"
  log "2. 更新已有部署"
  log "3. 修复/接管已有部署"
  log "4. 启动服务"
  log "5. 停止服务"
  log "6. 重启服务"
  log "7. 查看状态"
  log "8. 查看访问信息和密钥"
  log "9. 查看日志"
  log "10. 配置向导"
  log "11. 环境检查"
  log "12. 认证登录菜单"
  log "13. 卸载容器（保留数据）"
  log "14. 彻底删除"
  log "0. 退出"
  printf '请选择操作: '
}

run_menu() {
  while true; do
    load_cpa_env
    show_menu
    read -r choice
    case "$choice" in
      1)
        install_service
        ;;
      2)
        update_service
        ;;
      3)
        repair_installation
        ;;
      4)
        start_service
        ;;
      5)
        stop_service
        ;;
      6)
        restart_service
        ;;
      7)
        show_status
        ;;
      8)
        ensure_initialized
        show_access_info
        ;;
      9)
        show_logs
        ;;
      10)
        configure_settings
        ;;
      11)
        doctor
        ;;
      12)
        login_menu
        ;;
      13)
        uninstall_service
        ;;
      14)
        purge_all
        ;;
      0)
        log "退出。"
        exit 0
        ;;
      *)
        log "无效选择，请重试。"
        ;;
    esac
  done
}

load_cpa_env
ACTION=${1:-}

case "$ACTION" in
  install)
    install_service
    ;;
  update)
    update_service
    ;;
  repair)
    repair_installation
    ;;
  start)
    start_service
    ;;
  stop)
    stop_service
    ;;
  restart)
    restart_service
    ;;
  status)
    show_status
    ;;
  info)
    ensure_initialized
    show_access_info
    ;;
  logs)
    show_logs
    ;;
  configure)
    configure_settings
    ;;
  doctor)
    doctor
    ;;
  login)
    login_menu
    ;;
  uninstall)
    uninstall_service
    ;;
  purge)
    purge_all
    ;;
  ""|menu)
    run_menu
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    usage
    fail "不支持的操作: $ACTION"
    ;;
esac
