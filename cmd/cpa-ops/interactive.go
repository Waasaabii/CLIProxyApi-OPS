package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Waasaabii/CLIProxyApi-OPS/internal/ops"
)

func runInteractiveMenu(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)
	baseDir, err := defaultBaseDir()
	if err != nil {
		return err
	}
	upstreamBaseURL := ""

	for {
		manager, err := ops.NewManager(ops.Options{BaseDir: baseDir})
		if err != nil {
			return err
		}

		printInteractiveHeader(baseDir, upstreamBaseURL, manager)
		fmt.Println("1. 安装最新版本")
		fmt.Println("2. 安装指定版本")
		fmt.Println("3. 更新到最新版本")
		fmt.Println("4. 更新到指定版本")
		fmt.Println("5. 修复/接管部署")
		fmt.Println("6. 检查更新")
		fmt.Println("7. 查看合并 release 说明")
		fmt.Println("8. 查看部署状态")
		fmt.Println("9. 查看部署信息")
		fmt.Println("10. 查看/修改管理密钥")
		fmt.Println("11. 查看运维日志")
		fmt.Println("12. 创建备份")
		fmt.Println("13. 从备份恢复")
		fmt.Println("14. 卸载部署")
		fmt.Println("15. 启动运维代理服务")
		fmt.Println("16. 切换部署目录")
		fmt.Println("17. 设置上游地址覆盖")
		fmt.Println("0. 退出")

		choice, err := promptInput(reader, "请选择操作", "")
		if err != nil {
			return err
		}

		switch strings.TrimSpace(choice) {
		case "1":
			if err = runInteractiveInstall(ctx, reader, baseDir, upstreamBaseURL, ""); err != nil {
				fmt.Printf("安装失败: %v\n", err)
			}
		case "2":
			version, promptErr := promptVersion(ctx, reader, manager)
			if promptErr != nil {
				return promptErr
			}
			if strings.TrimSpace(version) == "" {
				break
			}
			if err = runInteractiveInstall(ctx, reader, baseDir, upstreamBaseURL, version); err != nil {
				fmt.Printf("安装失败: %v\n", err)
			}
		case "3":
			if err = runInteractiveUpdate(ctx, reader, baseDir, upstreamBaseURL, ""); err != nil {
				fmt.Printf("更新失败: %v\n", err)
			}
		case "4":
			version, promptErr := promptVersion(ctx, reader, manager)
			if promptErr != nil {
				return promptErr
			}
			if strings.TrimSpace(version) == "" {
				break
			}
			if err = runInteractiveUpdate(ctx, reader, baseDir, upstreamBaseURL, version); err != nil {
				fmt.Printf("更新失败: %v\n", err)
			}
		case "5":
			if err = runInteractiveRepair(ctx, reader, baseDir, upstreamBaseURL); err != nil {
				fmt.Printf("修复失败: %v\n", err)
			}
		case "6":
			if err = runCheckUpdate(ctx, interactiveContextArgs(baseDir, upstreamBaseURL)); err != nil {
				fmt.Printf("检查失败: %v\n", err)
			}
		case "7":
			if err = runReleaseNotes(ctx, interactiveContextArgs(baseDir, upstreamBaseURL)); err != nil {
				fmt.Printf("读取 release 说明失败: %v\n", err)
			}
		case "8":
			if err = runStatus(ctx, interactiveContextArgs(baseDir, upstreamBaseURL)); err != nil {
				fmt.Printf("读取状态失败: %v\n", err)
			}
		case "9":
			if err = runInfo(ctx, interactiveContextArgs(baseDir, upstreamBaseURL)); err != nil {
				fmt.Printf("读取信息失败: %v\n", err)
			}
		case "10":
			if err = runInteractiveManagementSecret(ctx, reader, baseDir, upstreamBaseURL); err != nil {
				fmt.Printf("管理密钥失败: %v\n", err)
			}
		case "11":
			if err = runInteractiveLogs(ctx, reader, baseDir, upstreamBaseURL); err != nil {
				fmt.Printf("读取日志失败: %v\n", err)
			}
		case "12":
			if err = runBackup(ctx, interactiveContextArgs(baseDir, upstreamBaseURL)); err != nil {
				fmt.Printf("备份失败: %v\n", err)
			}
		case "13":
			snapshot, promptErr := promptInput(reader, "请输入备份文件名（留空表示最新）", "")
			if promptErr != nil {
				return promptErr
			}
			args := interactiveContextArgs(baseDir, upstreamBaseURL)
			if strings.TrimSpace(snapshot) != "" {
				args = append(args, "--snapshot", strings.TrimSpace(snapshot))
			}
			if err = runRestore(ctx, args); err != nil {
				fmt.Printf("恢复失败: %v\n", err)
			}
		case "14":
			if err = runInteractiveUninstall(ctx, reader, baseDir, upstreamBaseURL); err != nil {
				fmt.Printf("卸载失败: %v\n", err)
			}
		case "15":
			listen, promptErr := promptInput(reader, "监听地址", "127.0.0.1:18318")
			if promptErr != nil {
				return promptErr
			}
			args := interactiveContextArgs(baseDir, upstreamBaseURL)
			args = append(args, "--listen", listen)
			return runServe(ctx, args)
		case "16":
			nextBaseDir, promptErr := promptInput(reader, "请输入新的部署目录", baseDir)
			if promptErr != nil {
				return promptErr
			}
			baseDir = filepath.Clean(strings.TrimSpace(nextBaseDir))
		case "17":
			nextUpstreamBaseURL, promptErr := promptInput(reader, "请输入上游 CPA 基础地址覆盖（留空清空）", upstreamBaseURL)
			if promptErr != nil {
				return promptErr
			}
			upstreamBaseURL = strings.TrimSpace(nextUpstreamBaseURL)
		case "0":
			return nil
		default:
			fmt.Println("无效选项，请重新输入。")
		}

		fmt.Println()
	}
}

func printInteractiveHeader(baseDir, upstreamBaseURL string, manager *ops.Manager) {
	fmt.Println("========================================")
	fmt.Println(" CLIProxyApi-OPS 交互式运维菜单")
	fmt.Println("========================================")
	fmt.Printf("部署目录: %s\n", baseDir)
	if strings.TrimSpace(upstreamBaseURL) != "" {
		fmt.Printf("上游地址覆盖: %s\n", strings.TrimSpace(upstreamBaseURL))
	}

	cfg, err := manager.CurrentConfig()
	if err == nil {
		fmt.Printf("当前镜像: %s\n", cfg.Image)
	}
	status, err := manager.Status(context.Background())
	if err == nil {
		fmt.Printf("容器状态: %s\n", blankFallback(status.State))
	} else {
		fmt.Printf("容器状态: 未知\n")
	}
	fmt.Println()
}

func printMergedReleaseNotes(ctx context.Context, manager *ops.Manager) error {
	info, err := manager.LatestReleaseNotes(ctx, "zh-CN", "")
	if err != nil {
		return err
	}
	fmt.Printf("当前版本: %s\n", blankFallback(info.CurrentVersion))
	fmt.Printf("最新版本: %s\n", blankFallback(info.LatestVersion))
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

func runInteractiveUninstall(ctx context.Context, reader *bufio.Reader, baseDir, upstreamBaseURL string) error {
	dryRunText, err := promptInput(reader, "是否先执行模拟卸载？(Y/n)", "Y")
	if err != nil {
		return err
	}
	dryRun := parseYes(dryRunText, true)
	purgeDataText, err := promptInput(reader, "是否删除 data 目录？(y/N)", "N")
	if err != nil {
		return err
	}
	purgeBackupsText, err := promptInput(reader, "是否删除 backups 目录？(y/N)", "N")
	if err != nil {
		return err
	}

	args := interactiveContextArgs(baseDir, upstreamBaseURL)
	if dryRun {
		args = append(args, "--dry-run")
	}
	if parseYes(purgeDataText, false) {
		args = append(args, "--purge-data")
	}
	if parseYes(purgeBackupsText, false) {
		args = append(args, "--purge-backups")
	}
	return runUninstall(ctx, args)
}

func runInteractiveManagementSecret(ctx context.Context, reader *bufio.Reader, baseDir, upstreamBaseURL string) error {
	manager, err := ops.NewManager(ops.Options{BaseDir: baseDir})
	if err != nil {
		return err
	}
	cfg, err := manager.CurrentConfig()
	if err != nil {
		return err
	}

	fmt.Println("当前管理密钥状态:")
	printManagementSecret(cfg)
	fmt.Println("留空直接回车表示返回，不做修改。")

	defaultValue := ""
	if !cfg.ManagementSecretHashed {
		defaultValue = strings.TrimSpace(cfg.ManagementSecret)
	}
	nextSecret, err := promptInput(reader, "请输入新的管理密钥", defaultValue)
	if err != nil {
		return err
	}
	nextSecret = strings.TrimSpace(nextSecret)
	if nextSecret == "" {
		fmt.Println("已取消修改管理密钥。")
		return nil
	}
	if nextSecret == strings.TrimSpace(cfg.ManagementSecret) && !cfg.ManagementSecretHashed {
		fmt.Println("管理密钥未变化，无需更新。")
		return nil
	}

	confirmText, err := promptInput(reader, "确认立即应用新的管理密钥？(Y/n)", "Y")
	if err != nil {
		return err
	}
	if !parseYes(confirmText, true) {
		fmt.Println("已取消修改管理密钥。")
		return nil
	}

	args := interactiveContextArgs(baseDir, upstreamBaseURL)
	args = append(args, "--management-secret", nextSecret)
	if err = runRepair(ctx, args); err != nil {
		return err
	}
	fmt.Println("管理密钥已更新。")
	return nil
}

func runInteractiveInstall(ctx context.Context, reader *bufio.Reader, baseDir, upstreamBaseURL, version string) error {
	cfg, err := currentInteractiveConfig(baseDir)
	if err != nil {
		return err
	}
	args, err := promptDeployOverrideArgs(reader, baseDir, upstreamBaseURL, cfg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(version) != "" {
		args = append(args, "--version", strings.TrimSpace(version))
	}
	return runInstall(ctx, args)
}

func runInteractiveUpdate(ctx context.Context, reader *bufio.Reader, baseDir, upstreamBaseURL, version string) error {
	cfg, err := currentInteractiveConfig(baseDir)
	if err != nil {
		return err
	}
	args, err := promptDeployOverrideArgs(reader, baseDir, upstreamBaseURL, cfg)
	if err != nil {
		return err
	}
	if strings.TrimSpace(version) != "" {
		args = append(args, "--version", strings.TrimSpace(version))
	}
	return runUpdate(ctx, args)
}

func runInteractiveRepair(ctx context.Context, reader *bufio.Reader, baseDir, upstreamBaseURL string) error {
	cfg, err := currentInteractiveConfig(baseDir)
	if err != nil {
		return err
	}
	args, err := promptDeployOverrideArgs(reader, baseDir, upstreamBaseURL, cfg)
	if err != nil {
		return err
	}
	return runRepair(ctx, args)
}

func runInteractiveLogs(ctx context.Context, reader *bufio.Reader, baseDir, upstreamBaseURL string) error {
	lines, err := promptIntInput(reader, "显示尾部日志行数", 200, false)
	if err != nil {
		return err
	}
	args := interactiveContextArgs(baseDir, upstreamBaseURL)
	args = append(args, "--lines", strconv.Itoa(lines))
	return runLogs(ctx, args)
}

func currentInteractiveConfig(baseDir string) (ops.DeployConfig, error) {
	manager, err := ops.NewManager(ops.Options{BaseDir: baseDir})
	if err != nil {
		return ops.DeployConfig{}, err
	}
	return manager.CurrentConfig()
}

func promptDeployOverrideArgs(reader *bufio.Reader, baseDir, upstreamBaseURL string, cfg ops.DeployConfig) ([]string, error) {
	args := interactiveContextArgs(baseDir, upstreamBaseURL)

	customizeText, err := promptInput(reader, "是否调整部署参数？(y/N)", "N")
	if err != nil {
		return nil, err
	}
	if !parseYes(customizeText, false) {
		return args, nil
	}

	fmt.Println("按回车保留当前值。镜像留空表示继续使用自动选择逻辑。")

	imageLabel := "自定义镜像（留空表示自动选择）"
	if currentImage := strings.TrimSpace(cfg.Image); currentImage != "" {
		imageLabel += "，当前 " + currentImage
	}
	image, err := promptInput(reader, imageLabel, "")
	if err != nil {
		return nil, err
	}
	if image = strings.TrimSpace(image); image != "" {
		args = append(args, "--image", image)
	}

	containerName, err := promptInput(reader, "容器名", strings.TrimSpace(cfg.ContainerName))
	if err != nil {
		return nil, err
	}
	if value := strings.TrimSpace(containerName); value != "" {
		args = append(args, "--container-name", value)
	}

	bindHost, err := promptInput(reader, "绑定地址", strings.TrimSpace(cfg.BindHost))
	if err != nil {
		return nil, err
	}
	if value := strings.TrimSpace(bindHost); value != "" {
		args = append(args, "--bind-host", value)
	}

	hostPort, err := promptIntInput(reader, "宿主机端口", cfg.HostPort, false)
	if err != nil {
		return nil, err
	}
	args = append(args, "--host-port", strconv.Itoa(hostPort))

	apiKey, err := promptInput(reader, "CPA API Key（留空保持当前/自动生成）", strings.TrimSpace(cfg.APIKey))
	if err != nil {
		return nil, err
	}
	if value := strings.TrimSpace(apiKey); value != "" {
		args = append(args, "--api-key", value)
	}

	managementDefault := ""
	if !cfg.ManagementSecretHashed {
		managementDefault = strings.TrimSpace(cfg.ManagementSecret)
	}
	managementSecret, err := promptInput(reader, "管理密钥（留空保持当前/自动生成）", managementDefault)
	if err != nil {
		return nil, err
	}
	if value := strings.TrimSpace(managementSecret); value != "" {
		args = append(args, "--management-secret", value)
	}

	allowRemote, err := promptBoolInput(reader, "允许远程管理", cfg.AllowRemoteManagement)
	if err != nil {
		return nil, err
	}
	args = append(args, "--allow-remote-management", strconv.FormatBool(allowRemote))

	disableControlPanel, err := promptBoolInput(reader, "禁用控制面板", cfg.DisableControlPanel)
	if err != nil {
		return nil, err
	}
	args = append(args, "--disable-control-panel", strconv.FormatBool(disableControlPanel))

	debugEnabled, err := promptBoolInput(reader, "开启调试模式", cfg.Debug)
	if err != nil {
		return nil, err
	}
	args = append(args, "--debug", strconv.FormatBool(debugEnabled))

	usageStatsEnabled, err := promptBoolInput(reader, "开启匿名统计", cfg.UsageStatisticsEnabled)
	if err != nil {
		return nil, err
	}
	args = append(args, "--usage-statistics-enabled", strconv.FormatBool(usageStatsEnabled))

	requestRetry, err := promptIntInput(reader, "请求重试次数", cfg.RequestRetry, true)
	if err != nil {
		return nil, err
	}
	args = append(args, "--request-retry", strconv.Itoa(requestRetry))

	return args, nil
}

func interactiveContextArgs(baseDir, upstreamBaseURL string) []string {
	args := []string{"--base-dir", baseDir}
	if strings.TrimSpace(upstreamBaseURL) != "" {
		args = append(args, "--upstream-base-url", strings.TrimSpace(upstreamBaseURL))
	}
	return args
}

func promptVersion(ctx context.Context, reader *bufio.Reader, manager *ops.Manager) (string, error) {
	releases, err := manager.ListReleases(ctx, 12)
	if err == nil && len(releases) > 0 {
		fmt.Println("可选 release:")
		for index, release := range releases {
			line := fmt.Sprintf("%d. %s", index+1, release.Version)
			if strings.TrimSpace(release.Title) != "" && strings.TrimSpace(release.Title) != release.Version {
				line += "  " + strings.TrimSpace(release.Title)
			}
			if strings.TrimSpace(release.PublishedAt) != "" {
				line += "  " + strings.TrimSpace(release.PublishedAt)
			}
			fmt.Println(line)
		}
		fmt.Println("M. 手动输入版本")
		fmt.Println("0. 返回上一级")

		selection, promptErr := promptInput(reader, "请选择目标版本编号", "1")
		if promptErr != nil {
			return "", promptErr
		}
		version, resolveErr := resolveReleaseVersionSelection(selection, releases)
		if resolveErr == nil {
			return version, nil
		}
		if errors.Is(resolveErr, errVersionSelectionBack) {
			return "", nil
		}
		if !errors.Is(resolveErr, errVersionSelectionManual) {
			return "", resolveErr
		}
	} else if err != nil {
		fmt.Printf("release 列表拉取失败，已回退为手动输入: %v\n", err)
	}

	value, err := promptInput(reader, "请输入目标版本，例如 v6.9.6", "")
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("目标版本不能为空")
	}
	return value, nil
}

var (
	errVersionSelectionManual = errors.New("manual version input")
	errVersionSelectionBack   = errors.New("back to menu")
)

func resolveReleaseVersionSelection(raw string, releases []ops.ReleaseSummary) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("目标版本不能为空")
	}
	switch strings.ToLower(value) {
	case "0", "q", "quit", "back":
		return "", errVersionSelectionBack
	case "m", "manual":
		return "", errVersionSelectionManual
	}
	if index, err := strconv.Atoi(value); err == nil {
		if index < 1 || index > len(releases) {
			return "", fmt.Errorf("无效版本编号: %d", index)
		}
		version := strings.TrimSpace(releases[index-1].Version)
		if version == "" {
			return "", fmt.Errorf("编号 %d 对应的版本为空", index)
		}
		return version, nil
	}
	return value, nil
}

func promptInput(reader *bufio.Reader, label, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Printf("%s [%s]: ", label, defaultValue)
	} else {
		fmt.Printf("%s: ", label)
	}
	text, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			text = strings.TrimSpace(text)
			if text == "" {
				return defaultValue, nil
			}
			return text, nil
		}
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultValue, nil
	}
	return text, nil
}

func promptBoolInput(reader *bufio.Reader, label string, defaultValue bool) (bool, error) {
	defaultText := "N"
	if defaultValue {
		defaultText = "Y"
	}
	raw, err := promptInput(reader, label+" (Y/n)", defaultText)
	if err != nil {
		return false, err
	}
	return parseYes(raw, defaultValue), nil
}

func promptIntInput(reader *bufio.Reader, label string, defaultValue int, allowZero bool) (int, error) {
	for {
		raw, err := promptInput(reader, label, strconv.Itoa(defaultValue))
		if err != nil {
			return 0, err
		}
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			fmt.Printf("%s 无效，必须是整数。\n", label)
			continue
		}
		if allowZero {
			if value < 0 {
				fmt.Printf("%s 无效，不能小于 0。\n", label)
				continue
			}
			return value, nil
		}
		if value <= 0 || value > 65535 {
			fmt.Printf("%s 无效，必须在 1-65535 之间。\n", label)
			continue
		}
		return value, nil
	}
}

func parseYes(raw string, defaultValue bool) bool {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return defaultValue
	}
	switch value {
	case "y", "yes", "1", "true":
		return true
	case "n", "no", "0", "false":
		return false
	default:
		return defaultValue
	}
}

func defaultBaseDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, ".cpa-docker"), nil
}
