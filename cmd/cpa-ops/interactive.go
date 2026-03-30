package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Waasaabii/CLIProxyApi-OPS/internal/ops"
)

func runInteractiveMenu(ctx context.Context) error {
	reader := bufio.NewReader(os.Stdin)
	baseDir, err := defaultBaseDir()
	if err != nil {
		return err
	}

	for {
		manager, err := ops.NewManager(ops.Options{BaseDir: baseDir})
		if err != nil {
			return err
		}

		printInteractiveHeader(baseDir, manager)
		fmt.Println("1. 安装最新版本")
		fmt.Println("2. 安装指定版本")
		fmt.Println("3. 更新到最新版本")
		fmt.Println("4. 更新到指定版本")
		fmt.Println("5. 修复/接管部署")
		fmt.Println("6. 检查更新")
		fmt.Println("7. 查看合并 release 说明")
		fmt.Println("8. 查看部署状态")
		fmt.Println("9. 查看部署信息")
		fmt.Println("10. 创建备份")
		fmt.Println("11. 从备份恢复")
		fmt.Println("12. 卸载部署")
		fmt.Println("13. 启动运维代理服务")
		fmt.Println("14. 切换部署目录")
		fmt.Println("0. 退出")

		choice, err := promptInput(reader, "请选择操作", "")
		if err != nil {
			return err
		}

		switch strings.TrimSpace(choice) {
		case "1":
			if err = manager.Install(ctx, manager.ConsoleLogger()); err != nil {
				fmt.Printf("安装失败: %v\n", err)
			}
		case "2":
			version, promptErr := promptVersion(reader)
			if promptErr != nil {
				return promptErr
			}
			if err = runInstall(ctx, []string{"--base-dir", baseDir, "--version", version}); err != nil {
				fmt.Printf("安装失败: %v\n", err)
			}
		case "3":
			if err = manager.Update(ctx, manager.ConsoleLogger(), ""); err != nil {
				fmt.Printf("更新失败: %v\n", err)
			}
		case "4":
			version, promptErr := promptVersion(reader)
			if promptErr != nil {
				return promptErr
			}
			if err = runUpdate(ctx, []string{"--base-dir", baseDir, "--version", version}); err != nil {
				fmt.Printf("更新失败: %v\n", err)
			}
		case "5":
			if err = manager.Repair(ctx, manager.ConsoleLogger()); err != nil {
				fmt.Printf("修复失败: %v\n", err)
			}
		case "6":
			if err = runCheckUpdate(ctx, []string{"--base-dir", baseDir}); err != nil {
				fmt.Printf("检查失败: %v\n", err)
			}
		case "7":
			if err = printMergedReleaseNotes(ctx, manager); err != nil {
				fmt.Printf("读取 release 说明失败: %v\n", err)
			}
		case "8":
			if err = runStatus(ctx, []string{"--base-dir", baseDir}); err != nil {
				fmt.Printf("读取状态失败: %v\n", err)
			}
		case "9":
			if err = runInfo(ctx, []string{"--base-dir", baseDir}); err != nil {
				fmt.Printf("读取信息失败: %v\n", err)
			}
		case "10":
			if err = runBackup(ctx, []string{"--base-dir", baseDir}); err != nil {
				fmt.Printf("备份失败: %v\n", err)
			}
		case "11":
			snapshot, promptErr := promptInput(reader, "请输入备份文件名（留空表示最新）", "")
			if promptErr != nil {
				return promptErr
			}
			args := []string{"--base-dir", baseDir}
			if strings.TrimSpace(snapshot) != "" {
				args = append(args, "--snapshot", strings.TrimSpace(snapshot))
			}
			if err = runRestore(ctx, args); err != nil {
				fmt.Printf("恢复失败: %v\n", err)
			}
		case "12":
			if err = runInteractiveUninstall(ctx, reader, baseDir); err != nil {
				fmt.Printf("卸载失败: %v\n", err)
			}
		case "13":
			listen, promptErr := promptInput(reader, "监听地址", "127.0.0.1:18318")
			if promptErr != nil {
				return promptErr
			}
			return runServe(ctx, []string{"--base-dir", baseDir, "--listen", listen})
		case "14":
			nextBaseDir, promptErr := promptInput(reader, "请输入新的部署目录", baseDir)
			if promptErr != nil {
				return promptErr
			}
			baseDir = filepath.Clean(strings.TrimSpace(nextBaseDir))
		case "0":
			return nil
		default:
			fmt.Println("无效选项，请重新输入。")
		}

		fmt.Println()
	}
}

func printInteractiveHeader(baseDir string, manager *ops.Manager) {
	fmt.Println("========================================")
	fmt.Println(" CLIProxyApi-OPS 交互式运维菜单")
	fmt.Println("========================================")
	fmt.Printf("部署目录: %s\n", baseDir)

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

func runInteractiveUninstall(ctx context.Context, reader *bufio.Reader, baseDir string) error {
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

	args := []string{"--base-dir", baseDir}
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

func promptVersion(reader *bufio.Reader) (string, error) {
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

func promptInput(reader *bufio.Reader, label, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Printf("%s [%s]: ", label, defaultValue)
	} else {
		fmt.Printf("%s: ", label)
	}
	text, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultValue, nil
	}
	return text, nil
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
