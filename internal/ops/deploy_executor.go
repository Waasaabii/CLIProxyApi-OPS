package ops

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type DeployExecutor interface {
	Pull(ctx context.Context, cfg DeployConfig, logger Logger) error
	Up(ctx context.Context, cfg DeployConfig, logger Logger) error
	Down(ctx context.Context, cfg DeployConfig, logger Logger) error
	RemoveContainer(ctx context.Context, cfg DeployConfig, logger Logger, containerName string) error
	InspectContainer(ctx context.Context, cfg DeployConfig, containerName string) (string, error)
	HasLocalImage(ctx context.Context, cfg DeployConfig, image string) bool
}

type composeDeployExecutorFactory struct{}

type composeDeployExecutor struct {
	manager *Manager
}

func (composeDeployExecutorFactory) Create(manager *Manager, options Options) DeployExecutor {
	return &composeDeployExecutor{manager: manager}
}

func (e *composeDeployExecutor) Pull(ctx context.Context, cfg DeployConfig, logger Logger) error {
	if err := e.runCompose(ctx, cfg, logger, "pull"); err != nil {
		if !e.HasLocalImage(ctx, cfg, strings.TrimSpace(cfg.Image)) {
			return err
		}
		message := fmt.Sprintf("镜像拉取失败，已回退为使用本地已有镜像继续部署: %s", strings.TrimSpace(cfg.Image))
		_ = e.manager.writeOperationLog(cfg, "%s", message)
		if logger != nil {
			logger.Printf("%s", message)
		}
		return nil
	}
	return nil
}

func (e *composeDeployExecutor) Up(ctx context.Context, cfg DeployConfig, logger Logger) error {
	return e.runCompose(ctx, cfg, logger, "up", "-d", "--remove-orphans")
}

func (e *composeDeployExecutor) Down(ctx context.Context, cfg DeployConfig, logger Logger) error {
	return e.runCompose(ctx, cfg, logger, "down", "--remove-orphans")
}

func (e *composeDeployExecutor) RemoveContainer(ctx context.Context, cfg DeployConfig, logger Logger, containerName string) error {
	_, err := e.runDocker(ctx, cfg, logger, "rm", "-f", containerName)
	return err
}

func (e *composeDeployExecutor) InspectContainer(ctx context.Context, cfg DeployConfig, containerName string) (string, error) {
	return e.runDocker(ctx, cfg, nil, "inspect", containerName, "--format", "{{json .}}")
}

func (e *composeDeployExecutor) HasLocalImage(ctx context.Context, cfg DeployConfig, image string) bool {
	image = strings.TrimSpace(image)
	if image == "" {
		return false
	}
	_, err := e.runDocker(ctx, cfg, nil, "image", "inspect", image, "--format", "{{.Id}}")
	return err == nil
}

func (e *composeDeployExecutor) runCompose(ctx context.Context, cfg DeployConfig, logger Logger, args ...string) error {
	composeCmd, err := detectComposeCommand(ctx)
	if err != nil {
		return err
	}
	command := append(append([]string{}, composeCmd...), "-f", cfg.ComposeFile)
	command = append(command, args...)
	_, err = e.runCommand(ctx, cfg, logger, command...)
	return err
}

func (e *composeDeployExecutor) runDocker(ctx context.Context, cfg DeployConfig, logger Logger, args ...string) (string, error) {
	command := append([]string{"docker"}, args...)
	return e.runCommand(ctx, cfg, logger, command...)
}

func (e *composeDeployExecutor) runCommand(ctx context.Context, cfg DeployConfig, logger Logger, command ...string) (string, error) {
	if len(command) == 0 {
		return "", errors.New("empty command")
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = cfg.BaseDir
	output, err := cmd.CombinedOutput()

	line := "$ " + strings.Join(command, " ")
	_ = e.manager.writeOperationLog(cfg, "%s", line)
	if logger != nil {
		logger.Printf(line)
	}
	text := strings.TrimSpace(string(output))
	if text != "" {
		for _, item := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			_ = e.manager.writeOperationLog(cfg, "%s", item)
			if logger != nil {
				logger.Printf(item)
			}
		}
	}
	if err != nil {
		return string(output), fmt.Errorf("命令执行失败: %s: %w", strings.Join(command, " "), err)
	}
	return string(output), nil
}

func detectComposeCommand(ctx context.Context) ([]string, error) {
	if err := exec.CommandContext(ctx, "docker", "compose", "version").Run(); err == nil {
		return []string{"docker", "compose"}, nil
	}
	if err := exec.CommandContext(ctx, "docker-compose", "version").Run(); err == nil {
		return []string{"docker-compose"}, nil
	}
	return nil, errors.New("未找到 docker compose 或 docker-compose")
}
