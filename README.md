# CLIProxyApi-OPS

CLI Proxy API 的独立运维工具。

它不内嵌到 CPA 服务里，主要给人直接在终端里做这些事情：

- 安装最新版本或指定版本
- 更新到最新版本或指定版本
- 修复并接管已有部署
- 检查更新、查看聚合后的 release 说明
- 查看部署状态、部署信息、运维日志
- 查看和修改管理密钥
- 备份、恢复、卸载
- 启动运维代理服务，把更新面板外挂到 CPA WebUI

## 适合怎么用

默认推荐直接用交互式终端。

这个工具现在的设计思路是：

- 人工运维时，优先走交互式菜单
- 脚本调用、自动化时，再走命令行参数

也就是说，正常使用时你不需要记一堆命令。

## 前置条件

- Docker 已安装并可用
- Docker Compose 可用：`docker compose` 或 `docker-compose`
- 当前工作目录就是你的运维工作区

## 快速开始

在目标工作区目录内执行：

```sh
curl -fsSL https://raw.githubusercontent.com/Waasaabii/CLIProxyApi-OPS/main/install-release.sh | sh
```

默认行为：

- 自动识别当前平台
- 下载最新 release
- 把二进制放到当前目录的 `./cpa-ops`
- 如果当前是交互终端，直接进入交互式菜单

如果只想下载，不立即启动：

```sh
curl -fsSL https://raw.githubusercontent.com/Waasaabii/CLIProxyApi-OPS/main/install-release.sh | sh -s -- --no-run
```

如果想下载指定版本：

```sh
curl -fsSL https://raw.githubusercontent.com/Waasaabii/CLIProxyApi-OPS/main/install-release.sh | sh -s -- --version v0.2.1
```

如果想下载到指定目录：

```sh
curl -fsSL https://raw.githubusercontent.com/Waasaabii/CLIProxyApi-OPS/main/install-release.sh | sh -s -- --install-root ./bin --no-run
```

如果你已经下载好了二进制，也可以直接运行：

```sh
./cpa-ops
```

## 交互式终端

直接运行：

```sh
./cpa-ops
```

如果当前是交互终端且没有传参，程序会直接进入菜单。

当前菜单大致如下：

```text
1. 安装最新版本
2. 安装指定版本
3. 更新到最新版本
4. 更新到指定版本
5. 修复/接管部署
6. 检查更新
7. 查看合并 release 说明
8. 查看部署状态
9. 查看部署信息
10. 查看/修改管理密钥
11. 查看运维日志
12. 创建备份
13. 从备份恢复
14. 卸载部署
15. 启动运维代理服务
16. 切换部署目录
17. 设置上游地址覆盖
0. 退出
```

交互式终端里已经覆盖了日常人工运维最常用的能力：

- 安装指定版本、更新指定版本时，会先拉取最近的 release 列表供你选择
- 安装、更新、修复前，会先问你是否要调整部署参数
- 可以直接交互设置这些常见参数：
  `image`、`container-name`、`bind-host`、`host-port`、`api-key`、`management-secret`、`allow-remote-management`、`disable-control-panel`、`debug`、`usage-statistics-enabled`、`request-retry`
- 管理密钥可以直接在菜单里查看和修改
- 运维日志可以直接在菜单里查看
- 部署目录和上游地址覆盖是会话级设置，切换后后续菜单项会自动继承

如果只是正常运维，优先直接用菜单就够了。

## 管理密钥说明

管理密钥现在按下面的方式处理：

- CPA 容器使用的 `config.yaml` 里保存的是哈希值
- 本地运维文件里会保留原始管理密钥，方便你在交互式终端里查看和修改
- 本地敏感文件会尽量使用更严格的文件权限

这意味着：

- 你可以在交互式菜单里直接查看或重置管理密钥
- 容器侧不会直接使用原始明文做校验存储
- 如果是非常老的部署，只剩哈希、从没保存过原始密钥，那就只能重新设置，不能反推出旧明文

## 常见使用方式

### 1. 首次安装

进入交互菜单后，直接选：

```text
1. 安装最新版本
```

如果要安装指定版本，就选：

```text
2. 安装指定版本
```

然后从 release 列表里选版本，或者手动输入版本号。

### 2. 更新已有部署

交互菜单里直接选：

```text
3. 更新到最新版本
4. 更新到指定版本
```

如果你需要改镜像、端口、管理密钥之类的部署参数，菜单里会继续问你，不需要切到命令行。

### 3. 接管已有部署

交互菜单里选：

```text
5. 修复/接管部署
```

适合这些场景：

- 当前目录里已经有旧部署
- 你想补齐缺失的运维文件
- 你想把当前部署纳入 `cpa-ops` 管理

### 4. 查看和修改管理密钥

交互菜单里选：

```text
10. 查看/修改管理密钥
```

这个入口会：

- 显示当前管理密钥状态
- 允许你直接输入新的管理密钥
- 二次确认后立即应用

### 5. 查看运维日志

交互菜单里选：

```text
11. 查看运维日志
```

会先让你输入要看的尾部行数，然后直接输出日志内容。

### 6. 切换部署目录或上游地址

如果你当前终端里要切换运维目标，不需要退出工具：

- `16. 切换部署目录`
- `17. 设置上游地址覆盖`

后续菜单项会自动继承当前会话的设置。

## 命令行模式

如果你要写脚本、做自动化，或者已经明确知道自己要执行什么操作，也可以直接用命令行。

常见命令示例：

```sh
./cpa-ops install
./cpa-ops install --version v6.9.3
./cpa-ops update
./cpa-ops update --version v6.9.3
./cpa-ops repair
./cpa-ops management-secret
./cpa-ops check-update
./cpa-ops release-notes
./cpa-ops status
./cpa-ops info
./cpa-ops logs --lines 200
./cpa-ops backup
./cpa-ops restore --snapshot 20260330-120000.tar.gz
./cpa-ops uninstall
./cpa-ops uninstall --purge-data --purge-backups
./cpa-ops serve --listen 127.0.0.1:18318
```

## 直接下载二进制

如果你不想用安装脚本，也可以直接下载 release 二进制。

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

## WebUI 注入

启动运维代理服务：

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

## 工作区约束

这个项目默认禁止把部署文件、日志、临时文件、测试产物写到工作区外。

- 默认部署目录：`./.cpa-docker`
- 默认临时目录：`./.tmp`
- 默认不允许使用 `/tmp`、`/private/tmp`、`/var/tmp`
- 如果传入的路径超出当前工作区，程序会直接拒绝执行

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
