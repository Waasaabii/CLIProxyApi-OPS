#!/bin/sh

set -eu

REPO_OWNER=${REPO_OWNER:-Waasaabii}
REPO_NAME=${REPO_NAME:-CLIProxyApi-OPS}
GITHUB_API_BASE=${CPA_OPS_GITHUB_API_BASE:-https://api.github.com/repos/$REPO_OWNER/$REPO_NAME}
RELEASE_BASE_URL=${CPA_OPS_RELEASE_BASE_URL:-https://github.com/$REPO_OWNER/$REPO_NAME/releases}
WORKSPACE_ROOT=$(pwd)
INSTALL_ROOT=${CPA_OPS_INSTALL_ROOT:-"$WORKSPACE_ROOT"}

trim_spaces() {
  printf '%s' "$1" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//'
}

fail() {
  printf '错误: %s\n' "$*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "缺少命令: $1"
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
      fail "路径超出当前工作区: $normalized_path (workspace: $WORKSPACE_ROOT)"
      ;;
  esac
}

detect_platform() {
  os_name=$(uname -s)
  arch_name=$(uname -m)

  case "$os_name" in
    Linux)
      platform_os=linux
      ;;
    Darwin)
      platform_os=darwin
      ;;
    *)
      fail "暂不支持的系统: $os_name"
      ;;
  esac

  case "$arch_name" in
    x86_64|amd64)
      platform_arch=amd64
      ;;
    arm64|aarch64)
      platform_arch=arm64
      ;;
    *)
      fail "暂不支持的架构: $arch_name"
      ;;
  esac
}

resolve_latest_version() {
  latest_json=$(curl -fsSL "$GITHUB_API_BASE/releases/latest") || fail "读取最新 release 失败"
  latest_version=$(printf '%s' "$latest_json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
  [ -n "$latest_version" ] || fail "无法解析最新 release 版本"
  printf '%s' "$latest_version"
}

build_asset_names() {
  binary_name="cpa-ops"
  asset_name="cpa-ops-$platform_os-$platform_arch"
  case "$platform_os" in
    windows)
      binary_name="cpa-ops.exe"
      asset_name="$asset_name.exe"
      ;;
  esac
}

print_usage() {
  cat <<EOF
用法:
  sh install-release.sh [--version vX.Y.Z] [--install-root .] [--no-run] [-- <cpa-ops 参数>]

说明:
  - 默认下载最新 release
  - 默认下载到当前工作区的 ./cpa-ops
  - 如果当前是交互终端且没有额外参数，下载完成后直接进入 cpa-ops 交互菜单
EOF
}

version=""
no_run="false"
pass_through_args=""

while [ $# -gt 0 ]; do
  case "$1" in
    --version)
      [ $# -ge 2 ] || fail "--version 缺少参数"
      version=$2
      shift 2
      ;;
    --install-root)
      [ $# -ge 2 ] || fail "--install-root 缺少参数"
      INSTALL_ROOT=$2
      shift 2
      ;;
    --no-run)
      no_run="true"
      shift
      ;;
    --help|-h)
      print_usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    *)
      break
      ;;
  esac
done

need_cmd curl

detect_platform
build_asset_names

if [ -z "$version" ]; then
  version=$(resolve_latest_version)
fi

INSTALL_ROOT=$(normalize_workspace_path "$INSTALL_ROOT")
binary_path="$INSTALL_ROOT/$binary_name"
temp_binary_path="$INSTALL_ROOT/.${binary_name}.download.$$"

mkdir -p "$INSTALL_ROOT"

if [ "$version" = "latest" ]; then
  download_url="$RELEASE_BASE_URL/latest/download/$asset_name"
else
  download_url="$RELEASE_BASE_URL/download/$version/$asset_name"
fi

rm -f "$temp_binary_path"
printf '下载 %s\n' "$download_url"
curl -fL "$download_url" -o "$temp_binary_path" || fail "下载 release 二进制失败"
mv "$temp_binary_path" "$binary_path"

[ -f "$binary_path" ] || fail "未找到二进制: $binary_path"
chmod +x "$binary_path" 2>/dev/null || true

printf '已安装到: %s\n' "$binary_path"

if [ "$no_run" = "true" ]; then
  exit 0
fi

if [ $# -gt 0 ]; then
  exec "$binary_path" "$@"
fi

if [ -t 0 ] && [ -t 1 ]; then
  exec "$binary_path"
fi

printf '当前不是交互终端，未自动进入菜单。你可以手动运行:\n%s\n' "$binary_path"
