# CLIProxyApi-OPS

CLI Proxy API 的独立运维工具。

它不内嵌到 CPA 服务里，负责安装、更新、回退、修复、备份、恢复、卸载，以及通过反向代理把更新面板外挂到 CPA WebUI。

## 能力范围

- 安装最新版本或指定版本
- 更新到最新版本或指定版本
- 回退到指定版本
- 修复并接管已有部署
- 检查更新、聚合多版本 release 说明、生成更新建议
- 从 CPA 主服务调用翻译能力翻译 release 说明
- 通过反向代理向 CPA 管理页注入“检查更新 / 立即更新并重启”
- 备份、恢复、卸载
- 交互式终端菜单

交互式终端里选择“安装指定版本 / 更新到指定版本”时，会先拉取最近 release 列表供选择；只有 release 拉取失败或你主动选择手动模式时，才需要自己输入版本号。

## 前置条件

- Docker 已安装并可用
- Docker Compose 可用：`docker compose` 或 `docker-compose`
- 当前工作目录就是你的运维工作区

## 工作区约束

这个项目默认禁止把部署文件、日志、临时文件、测试产物写到工作区外。

- 默认部署目录：`./.cpa-docker`
- 默认临时目录：`./.tmp`
- 默认不允许使用 `/tmp`、`/private/tmp`、`/var/tmp`
- 如果传入的路径超出当前工作区，程序会直接拒绝执行

## 直接从 Release 安装

如果你不想自己 `go build`，推荐直接用下面这条命令。

在目标工作区目录内执行：

```sh
curl -fsSL https://raw.githubusercontent.com/Waasaabii/CLIProxyApi-OPS/main/install-release.sh | sh
```

它会自动识别系统架构，下载对应平台的最新单文件二进制，并直接进入交互终端。

默认行为：

- 自动识别当前平台
- 下载最新 release
- 下载到当前工作区的 `./.tmp/releases/<version>/cpa-ops`
- 直接启动 `cpa-ops` 交互终端

安装指定版本：

```sh
curl -fsSL https://raw.githubusercontent.com/Waasaabii/CLIProxyApi-OPS/main/install-release.sh | sh -s -- --version v0.1.0
```

只下载不启动：

```sh
curl -fsSL https://raw.githubusercontent.com/Waasaabii/CLIProxyApi-OPS/main/install-release.sh | sh -s -- --no-run
```

如果你更喜欢像宝塔那样“直接下载一个可执行文件再运行”，可以直接使用 GitHub Release 资产。

Linux amd64：

```sh
curl -fL https://github.com/Waasaabii/CLIProxyApi-OPS/releases/latest/download/cpa-ops-linux-amd64 -o ./cpa-ops
chmod +x ./cpa-ops
./cpa-ops
```

Linux arm64：

```sh
curl -fL https://github.com/Waasaabii/CLIProxyApi-OPS/releases/latest/download/cpa-ops-linux-arm64 -o ./cpa-ops
chmod +x ./cpa-ops
./cpa-ops
```

macOS amd64：

```sh
curl -fL https://github.com/Waasaabii/CLIProxyApi-OPS/releases/latest/download/cpa-ops-darwin-amd64 -o ./cpa-ops
chmod +x ./cpa-ops
./cpa-ops
```

macOS arm64：

```sh
curl -fL https://github.com/Waasaabii/CLIProxyApi-OPS/releases/latest/download/cpa-ops-darwin-arm64 -o ./cpa-ops
chmod +x ./cpa-ops
./cpa-ops
```

Windows PowerShell：

```powershell
Invoke-WebRequest -Uri "https://github.com/Waasaabii/CLIProxyApi-OPS/releases/latest/download/cpa-ops-windows-amd64.exe" -OutFile ".\cpa-ops.exe"
.\cpa-ops.exe
```

## 本地构建

```sh
go build -o ./cpa-ops ./cmd/cpa-ops
./cpa-ops
```

如果当前是交互终端且没有传参，程序会直接进入交互菜单。

## 常用命令

```sh
./cpa-ops
./cpa-ops install
./cpa-ops install --version v6.9.3
./cpa-ops update
./cpa-ops update --version v6.9.3
./cpa-ops repair
./cpa-ops check-update
./cpa-ops release-notes
./cpa-ops backup
./cpa-ops restore --snapshot 20260330-120000.tar.gz
./cpa-ops uninstall
./cpa-ops uninstall --purge-data --purge-backups
./cpa-ops serve --listen 127.0.0.1:18318
```

## WebUI 注入

```sh
./cpa-ops serve --listen 127.0.0.1:18318
```

然后访问：

```text
http://127.0.0.1:18318/management.html#/system
```

效果：

- 保留 CPA 原始管理页
- 在系统页外挂运维更新面板
- 点击原页面“检查更新”后显示聚合版本信息
- 可以直接“立即更新并重启”

## GitHub Release 资产命名

- `cpa-ops-linux-amd64`
- `cpa-ops-linux-arm64`
- `cpa-ops-darwin-amd64`
- `cpa-ops-darwin-arm64`
- `cpa-ops-windows-amd64.exe`

## 开发验证

```sh
go test ./...
```

CI 会在以下场景执行测试和打包：

- push 到 `main` / `master`
- Pull Request
- 手动触发
- 打 tag 时额外发布 GitHub Release 资产
