package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Waasaabii/CLIProxyApi-OPS/internal/ops"
	"github.com/Waasaabii/CLIProxyApi-OPS/internal/server"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) == 0 {
		if isInteractiveTerminal() {
			return runInteractiveMenu(ctx)
		}
		printUsage()
		return nil
	}

	switch args[0] {
	case "menu":
		return runInteractiveMenu(ctx)
	case "install":
		return runInstall(ctx, args[1:])
	case "update":
		return runUpdate(ctx, args[1:])
	case "repair":
		return runRepair(ctx, args[1:])
	case "backup":
		return runBackup(ctx, args[1:])
	case "restore":
		return runRestore(ctx, args[1:])
	case "uninstall":
		return runUninstall(ctx, args[1:])
	case "status":
		return runStatus(ctx, args[1:])
	case "info":
		return runInfo(ctx, args[1:])
	case "management-secret":
		return runManagementSecret(ctx, args[1:])
	case "logs":
		return runLogs(ctx, args[1:])
	case "check-update":
		return runCheckUpdate(ctx, args[1:])
	case "release-notes":
		return runReleaseNotes(ctx, args[1:])
	case "serve":
		return runServe(ctx, args[1:])
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("不支持的命令: %s", args[0])
	}
}

func printUsage() {
	fmt.Print(`用法:
  cpa-ops install         首次安装并启动 CPA
  cpa-ops update          更新已有部署并重启
  cpa-ops repair          接管或修复已有部署
  cpa-ops backup          备份当前部署文件
  cpa-ops restore         从备份恢复并重启
  cpa-ops uninstall       卸载当前部署
  cpa-ops status          查看容器状态
  cpa-ops info            查看部署信息
  cpa-ops management-secret 查看管理密钥
  cpa-ops logs            查看运维日志
  cpa-ops check-update    检查最新版本
 cpa-ops release-notes   查看最新 release 说明
  cpa-ops serve           启动运维 API 与反向代理
  cpa-ops menu            启动交互式运维菜单

公共参数:
  --base-dir              CPA 部署目录
  --upstream-base-url     覆盖反向代理到 CPA 的基础地址
  --version               指定 CPA 版本标签，例如 v6.9.6
  --image                 指定完整镜像名，例如 eceasy/cli-proxy-api:v6.9.6
`)
}

func runInstall(ctx context.Context, args []string) error {
	manager, _, err := buildManager(args)
	if err != nil {
		return err
	}
	return manager.Install(ctx, manager.ConsoleLogger())
}

func runUpdate(ctx context.Context, args []string) error {
	manager, _, err := buildManager(args)
	if err != nil {
		return err
	}
	return manager.Update(ctx, manager.ConsoleLogger(), "")
}

func runRepair(ctx context.Context, args []string) error {
	manager, _, err := buildManager(args)
	if err != nil {
		return err
	}
	return manager.Repair(ctx, manager.ConsoleLogger())
}

func runBackup(ctx context.Context, args []string) error {
	manager, _, err := buildManager(args)
	if err != nil {
		return err
	}
	snapshot, err := manager.Backup(ctx, manager.ConsoleLogger())
	if err != nil {
		return err
	}
	fmt.Printf("备份完成: %s\n", snapshot.Path)
	return nil
}

func runRestore(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)

	baseDir := flags.String("base-dir", "", "CPA 部署目录")
	upstreamBaseURL := flags.String("upstream-base-url", "", "上游 CPA 基础地址")
	snapshot := flags.String("snapshot", "", "备份目录名，默认恢复最新")

	if err := flags.Parse(args); err != nil {
		return err
	}

	manager, err := ops.NewManager(ops.Options{
		BaseDir:         *baseDir,
		UpstreamBaseURL: *upstreamBaseURL,
	})
	if err != nil {
		return err
	}
	return manager.Restore(ctx, manager.ConsoleLogger(), *snapshot)
}

func runStatus(ctx context.Context, args []string) error {
	manager, _, err := buildManager(args)
	if err != nil {
		return err
	}
	status, err := manager.Status(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("容器名: %s\n", status.ContainerName)
	fmt.Printf("容器状态: %s\n", status.State)
	fmt.Printf("镜像: %s\n", status.Image)
	fmt.Printf("端口: %s\n", status.Ports)
	return nil
}

func runUninstall(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)

	baseDir := flags.String("base-dir", "", "CPA 部署目录")
	upstreamBaseURL := flags.String("upstream-base-url", "", "上游 CPA 基础地址")
	dryRun := flags.Bool("dry-run", false, "仅展示将清理的内容，不实际执行")
	purgeData := flags.Bool("purge-data", false, "同时删除 data 目录")
	purgeBackups := flags.Bool("purge-backups", false, "同时删除 backups 目录")

	if err := flags.Parse(args); err != nil {
		return err
	}

	manager, err := ops.NewManager(ops.Options{
		BaseDir:         *baseDir,
		UpstreamBaseURL: *upstreamBaseURL,
	})
	if err != nil {
		return err
	}

	result, err := manager.Uninstall(ctx, manager.ConsoleLogger(), ops.UninstallOptions{
		DryRun:       *dryRun,
		PurgeData:    *purgeData,
		PurgeBackups: *purgeBackups,
	})
	if err != nil {
		return err
	}

	if result.DryRun {
		fmt.Println("模拟卸载完成，以下内容将被清理:")
	} else {
		fmt.Println("卸载完成，已清理:")
	}
	for _, path := range result.Removed {
		fmt.Printf("  - %s\n", path)
	}
	if len(result.Kept) > 0 {
		fmt.Println("保留内容:")
		for _, path := range result.Kept {
			fmt.Printf("  - %s\n", path)
		}
	}
	return nil
}

func runInfo(ctx context.Context, args []string) error {
	manager, authToken, err := buildManager(args)
	if err != nil {
		return err
	}
	info, err := manager.Info(ctx, authToken)
	if err != nil {
		return err
	}
	fmt.Printf("部署目录: %s\n", info.Config.BaseDir)
	fmt.Printf("数据目录: %s\n", info.Config.DataDir)
	fmt.Printf("Compose 文件: %s\n", info.Config.ComposeFile)
	fmt.Printf("配置文件: %s\n", info.Config.ConfigFile)
	fmt.Printf("状态文件: %s\n", info.Config.StateFile)
	fmt.Printf("镜像: %s\n", info.Config.Image)
	fmt.Printf("容器名: %s\n", info.Config.ContainerName)
	fmt.Printf("当前版本: %s\n", blankFallback(info.Version.CurrentVersion))
	fmt.Printf("最新版本: %s\n", blankFallback(info.Version.LatestVersion))
	fmt.Printf("有新版本: %t\n", info.Version.HasUpdate)
	fmt.Printf("容器状态: %s\n", info.Status.State)
	fmt.Printf("最近备份: %s\n", blankFallback(info.LastBackup))
	printManagementSecret(info.Config)
	return nil
}

func runManagementSecret(ctx context.Context, args []string) error {
	manager, _, err := buildManager(args)
	if err != nil {
		return err
	}
	cfg, err := manager.CurrentConfig()
	if err != nil {
		return err
	}
	printManagementSecret(cfg)
	return nil
}

func runLogs(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("logs", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)

	baseDir := flags.String("base-dir", "", "CPA 部署目录")
	upstreamBaseURL := flags.String("upstream-base-url", "", "上游 CPA 基础地址")
	lines := flags.Int("lines", 200, "显示尾部日志行数")

	if err := flags.Parse(args); err != nil {
		return err
	}

	manager, err := ops.NewManager(ops.Options{
		BaseDir:         *baseDir,
		UpstreamBaseURL: *upstreamBaseURL,
	})
	if err != nil {
		return err
	}
	content, err := manager.ReadOperationLog(ctx, *lines)
	if err != nil {
		return err
	}
	fmt.Print(content)
	return nil
}

func runCheckUpdate(ctx context.Context, args []string) error {
	manager, authToken, err := buildManager(args)
	if err != nil {
		return err
	}
	info, err := manager.CheckUpdate(ctx, authToken)
	if err != nil {
		return err
	}
	fmt.Printf("当前版本: %s\n", blankFallback(info.CurrentVersion))
	fmt.Printf("最新版本: %s\n", blankFallback(info.LatestVersion))
	fmt.Printf("有新版本: %t\n", info.HasUpdate)
	fmt.Printf("落后版本数: %d\n", info.BehindCount)
	fmt.Printf("发布时间: %s\n", blankFallback(info.PublishedAt))
	if len(info.MissingVersions) > 0 {
		fmt.Printf("缺失版本: %s\n", strings.Join(info.MissingVersions, ", "))
	}
	if info.UpdateRecommendation != "" {
		fmt.Printf("更新建议: %s\n", info.UpdateRecommendation)
	}
	return nil
}

func runReleaseNotes(ctx context.Context, args []string) error {
	manager, authToken, err := buildManager(args)
	if err != nil {
		return err
	}
	info, err := manager.LatestReleaseNotes(ctx, "zh-CN", authToken)
	if err != nil {
		return err
	}
	fmt.Printf("当前版本: %s\n", blankFallback(info.CurrentVersion))
	fmt.Printf("版本: %s\n", blankFallback(info.LatestVersion))
	fmt.Printf("标题: %s\n", blankFallback(info.ReleaseTitle))
	fmt.Printf("落后版本数: %d\n", info.BehindCount)
	if len(info.MissingVersions) > 0 {
		fmt.Printf("缺失版本: %s\n", strings.Join(info.MissingVersions, ", "))
	}
	if info.UpdateRecommendation != "" {
		fmt.Printf("更新建议: %s\n", info.UpdateRecommendation)
	}
	if info.UpdateSummary != "" {
		fmt.Printf("更新摘要: %s\n", info.UpdateSummary)
	}
	fmt.Printf("发布时间: %s\n", blankFallback(info.PublishedAt))
	fmt.Printf("链接: %s\n", blankFallback(info.ReleaseURL))
	fmt.Printf("\n%s\n", strings.TrimSpace(info.ReleaseNotes))
	return nil
}

func runServe(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)

	baseDir := flags.String("base-dir", "", "CPA 部署目录")
	upstreamBaseURL := flags.String("upstream-base-url", "", "上游 CPA 基础地址")
	listenAddr := flags.String("listen", ":18318", "监听地址")

	if err := flags.Parse(args); err != nil {
		return err
	}

	manager, err := ops.NewManager(ops.Options{
		BaseDir:         *baseDir,
		UpstreamBaseURL: *upstreamBaseURL,
	})
	if err != nil {
		return err
	}

	srv, err := server.New(manager, *listenAddr)
	if err != nil {
		return err
	}
	fmt.Printf("cpa-ops 已启动: %s\n", *listenAddr)
	return srv.Run(ctx)
}

func buildManager(args []string) (*ops.Manager, string, error) {
	flags := flag.NewFlagSet("common", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)

	baseDir := flags.String("base-dir", "", "CPA 部署目录")
	upstreamBaseURL := flags.String("upstream-base-url", "", "上游 CPA 基础地址")
	authToken := flags.String("management-key", "", "用于版本探测的管理密钥，可选")

	version := flags.String("version", "", "CPA 版本标签，例如 v6.9.6")
	image := flags.String("image", "", "CPA 镜像")
	containerName := flags.String("container-name", "", "CPA 容器名")
	bindHost := flags.String("bind-host", "", "宿主机绑定地址")
	hostPort := flags.Int("host-port", 0, "宿主机端口")
	apiKey := flags.String("api-key", "", "CPA API Key")
	managementSecret := flags.String("management-secret", "", "CPA 管理密钥")
	allowRemote := flags.String("allow-remote-management", "", "是否允许远程管理")
	disableControlPanel := flags.String("disable-control-panel", "", "是否禁用控制面板")
	debug := flags.String("debug", "", "是否开启调试")
	usageStats := flags.String("usage-statistics-enabled", "", "是否开启匿名统计")
	requestRetry := flags.Int("request-retry", -1, "请求重试次数")

	if err := flags.Parse(args); err != nil {
		return nil, "", err
	}
	if *hostPort < 0 || *hostPort > 65535 {
		return nil, "", fmt.Errorf("--host-port 参数无效: 端口必须在 1-65535 之间")
	}
	if *requestRetry < -1 {
		return nil, "", fmt.Errorf("--request-retry 参数无效: 必须是非负整数")
	}
	imageValue, imageExplicit := resolveImageOverride(*image, *version)
	allowRemoteValue, err := parseOptionalBool(*allowRemote)
	if err != nil {
		return nil, "", fmt.Errorf("--allow-remote-management 参数无效: %w", err)
	}
	disableControlPanelValue, err := parseOptionalBool(*disableControlPanel)
	if err != nil {
		return nil, "", fmt.Errorf("--disable-control-panel 参数无效: %w", err)
	}
	debugValue, err := parseOptionalBool(*debug)
	if err != nil {
		return nil, "", fmt.Errorf("--debug 参数无效: %w", err)
	}
	usageStatsValue, err := parseOptionalBool(*usageStats)
	if err != nil {
		return nil, "", fmt.Errorf("--usage-statistics-enabled 参数无效: %w", err)
	}

	manager, err := ops.NewManager(ops.Options{
		BaseDir:         *baseDir,
		UpstreamBaseURL: *upstreamBaseURL,
		Overrides: ops.OverrideConfig{
			Image:                  imageValue,
			ImageExplicit:          imageExplicit,
			ContainerName:          *containerName,
			BindHost:               *bindHost,
			HostPort:               *hostPort,
			APIKey:                 *apiKey,
			ManagementSecret:       *managementSecret,
			AllowRemoteManagement:  allowRemoteValue,
			DisableControlPanel:    disableControlPanelValue,
			Debug:                  debugValue,
			UsageStatisticsEnabled: usageStatsValue,
			RequestRetry:           *requestRetry,
			RequestRetryExplicit:   *requestRetry >= 0,
		},
	})
	if err != nil {
		return nil, "", err
	}
	return manager, *authToken, nil
}

func resolveImageOverride(image, version string) (string, bool) {
	image = strings.TrimSpace(image)
	if image != "" {
		return image, true
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return "", false
	}
	return "eceasy/cli-proxy-api:" + strings.TrimPrefix(version, ":"), true
}

func parseOptionalBool(raw string) (*bool, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "":
		return nil, nil
	case "true", "1", "yes", "y":
		value := true
		return &value, nil
	case "false", "0", "no", "n":
		value := false
		return &value, nil
	default:
		return nil, fmt.Errorf("无效布尔值 %q，允许值: true/false", strings.TrimSpace(raw))
	}
}

func blankFallback(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未知"
	}
	return value
}

func printManagementSecret(cfg ops.DeployConfig) {
	secret := strings.TrimSpace(cfg.ManagementSecret)
	switch {
	case secret == "":
		fmt.Printf("管理密钥: [未设置]\n")
	case cfg.ManagementSecretHashed:
		fmt.Printf("管理密钥: [当前仅检测到哈希值，请通过 --management-secret 重新设置]\n")
	default:
		fmt.Printf("管理密钥: %s\n", secret)
	}
}

func isInteractiveTerminal() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}
