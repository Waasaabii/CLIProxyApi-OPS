package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	githubLatestReleaseURL = defaultGitHubLatestRelease
	githubReleaseListURL   = defaultGitHubReleaseList
)

type Manager struct {
	options         Options
	releaseProvider ReleaseProvider
	deployExecutor  DeployExecutor
}

func NewManager(options Options) (*Manager, error) {
	return newManagerWithDependencies(options, defaultManagerDependencies())
}

func newManagerWithDependencies(options Options, dependencies ManagerDependencies) (*Manager, error) {
	workspaceRoot, err := normalizeWorkspaceRoot(options.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	baseDir := strings.TrimSpace(options.BaseDir)
	if baseDir == "" {
		baseDir = filepath.Join(workspaceRoot, ".cpa-docker")
	}
	baseDir, err = normalizePathWithinWorkspace(workspaceRoot, baseDir)
	if err != nil {
		return nil, err
	}
	options.BaseDir = baseDir
	options.WorkspaceRoot = workspaceRoot

	if dependencies.ReleaseProviderFactory == nil {
		dependencies.ReleaseProviderFactory = githubReleaseProviderFactory{}
	}
	if dependencies.DeployExecutorFactory == nil {
		dependencies.DeployExecutorFactory = composeDeployExecutorFactory{}
	}

	manager := &Manager{
		options: options,
	}
	manager.releaseProvider = dependencies.ReleaseProviderFactory.Create(options)
	manager.deployExecutor = dependencies.DeployExecutorFactory.Create(manager, options)
	if manager.releaseProvider == nil {
		return nil, errors.New("release provider 初始化失败")
	}
	if manager.deployExecutor == nil {
		return nil, errors.New("deploy executor 初始化失败")
	}
	return manager, nil
}

func (m *Manager) ConsoleLogger() Logger {
	return ConsoleLogger{}
}

func (ConsoleLogger) Printf(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}

func (m *Manager) loadConfig() (DeployConfig, error) {
	cfg := defaultDeployConfig(m.options.BaseDir)

	if state, err := m.loadState(); err == nil {
		cfg = mergeDeployConfig(cfg, state.Config)
	}
	if envCfg, err := loadEnvFile(cfg.EnvFile, cfg); err == nil {
		cfg = envCfg
	}
	if composeCfg, err := loadComposeFile(cfg.ComposeFile, cfg); err == nil {
		cfg = composeCfg
	}
	if fileCfg, err := loadRuntimeConfig(cfg.ConfigFile, cfg); err == nil {
		cfg = fileCfg
	}

	cfg = applyOverrides(cfg, m.options.Overrides)
	cfg = finalizeDeployConfig(cfg, m.options.UpstreamBaseURL)
	baseDir, err := normalizePathWithinWorkspace(m.options.WorkspaceRoot, cfg.BaseDir)
	if err != nil {
		return DeployConfig{}, err
	}
	cfg.BaseDir = baseDir
	cfg = finalizeDeployConfig(cfg, m.options.UpstreamBaseURL)
	return cfg, nil
}

func normalizeWorkspaceRoot(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		raw = cwd
	}
	return normalizeAbsolutePath(raw)
}

func normalizePathWithinWorkspace(workspaceRoot, target string) (string, error) {
	workspaceRoot, err := normalizeWorkspaceRoot(workspaceRoot)
	if err != nil {
		return "", err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("路径不能为空")
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(workspaceRoot, target)
	}
	target, err = normalizeAbsolutePath(target)
	if err != nil {
		return "", err
	}
	relPath, err := filepath.Rel(workspaceRoot, target)
	if err != nil {
		return "", err
	}
	if relPath == "." {
		return target, nil
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("路径超出工作区: %s (workspace: %s)", target, workspaceRoot)
	}
	return target, nil
}

func normalizeAbsolutePath(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("路径不能为空")
	}
	target, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	return filepath.Clean(target), nil
}

func defaultDeployConfig(baseDir string) DeployConfig {
	cfg := DeployConfig{
		BaseDir:                filepath.Clean(baseDir),
		Image:                  defaultImage,
		ContainerName:          defaultContainerName,
		BindHost:               defaultBindHost,
		HostPort:               defaultHostPort,
		ContainerPort:          defaultContainerPort,
		AllowRemoteManagement:  true,
		DisableControlPanel:    false,
		Debug:                  false,
		UsageStatisticsEnabled: false,
		RequestRetry:           defaultRequestRetry,
		AuthDir:                defaultAuthDir,
	}
	return finalizeDeployConfig(cfg, "")
}

func finalizeDeployConfig(cfg DeployConfig, upstreamOverride string) DeployConfig {
	cfg.BaseDir = filepath.Clean(cfg.BaseDir)
	cfg.DataDir = filepath.Join(cfg.BaseDir, "data")
	cfg.ComposeFile = filepath.Join(cfg.BaseDir, "docker-compose.yml")
	cfg.ConfigFile = filepath.Join(cfg.DataDir, "config.yaml")
	cfg.EnvFile = filepath.Join(cfg.BaseDir, "cpa-install.env")
	cfg.StateFile = filepath.Join(cfg.BaseDir, "ops", "ops-state.json")
	cfg.BackupsDir = filepath.Join(cfg.BaseDir, "backups")
	cfg.OperationLogFile = filepath.Join(cfg.BaseDir, "cpa-operation.log")

	if cfg.ContainerPort == 0 {
		cfg.ContainerPort = defaultContainerPort
	}
	if cfg.HostPort == 0 {
		cfg.HostPort = defaultHostPort
	}
	if strings.TrimSpace(cfg.Image) == "" {
		cfg.Image = defaultImage
	}
	if strings.TrimSpace(cfg.ContainerName) == "" {
		cfg.ContainerName = defaultContainerName
	}
	if strings.TrimSpace(cfg.BindHost) == "" {
		cfg.BindHost = defaultBindHost
	}
	if strings.TrimSpace(cfg.AuthDir) == "" {
		cfg.AuthDir = defaultAuthDir
	}
	if cfg.RequestRetry < 0 {
		cfg.RequestRetry = defaultRequestRetry
	}
	if strings.TrimSpace(upstreamOverride) != "" {
		cfg.UpstreamBaseURLOverride = strings.TrimSpace(upstreamOverride)
	}
	return cfg
}

func applyOverrides(cfg DeployConfig, overrides OverrideConfig) DeployConfig {
	if strings.TrimSpace(overrides.Image) != "" {
		cfg.Image = strings.TrimSpace(overrides.Image)
	}
	if strings.TrimSpace(overrides.ContainerName) != "" {
		cfg.ContainerName = strings.TrimSpace(overrides.ContainerName)
	}
	if strings.TrimSpace(overrides.BindHost) != "" {
		cfg.BindHost = strings.TrimSpace(overrides.BindHost)
	}
	if overrides.HostPort > 0 {
		cfg.HostPort = overrides.HostPort
	}
	if strings.TrimSpace(overrides.APIKey) != "" {
		cfg.APIKey = strings.TrimSpace(overrides.APIKey)
	}
	if strings.TrimSpace(overrides.ManagementSecret) != "" {
		cfg.ManagementSecret = overrides.ManagementSecret
		cfg.ManagementSecretHashed = isBcryptHash(cfg.ManagementSecret)
	}
	if overrides.AllowRemoteManagement != nil {
		cfg.AllowRemoteManagement = *overrides.AllowRemoteManagement
	}
	if overrides.DisableControlPanel != nil {
		cfg.DisableControlPanel = *overrides.DisableControlPanel
	}
	if overrides.Debug != nil {
		cfg.Debug = *overrides.Debug
	}
	if overrides.UsageStatisticsEnabled != nil {
		cfg.UsageStatisticsEnabled = *overrides.UsageStatisticsEnabled
	}
	if overrides.RequestRetry >= 0 {
		cfg.RequestRetry = overrides.RequestRetry
	}
	return cfg
}

func mergeDeployConfig(base, incoming DeployConfig) DeployConfig {
	out := base
	if strings.TrimSpace(incoming.BaseDir) != "" {
		out.BaseDir = incoming.BaseDir
	}
	if strings.TrimSpace(incoming.Image) != "" {
		out.Image = incoming.Image
	}
	if strings.TrimSpace(incoming.ContainerName) != "" {
		out.ContainerName = incoming.ContainerName
	}
	if strings.TrimSpace(incoming.BindHost) != "" {
		out.BindHost = incoming.BindHost
	}
	if incoming.HostPort > 0 {
		out.HostPort = incoming.HostPort
	}
	if incoming.ContainerPort > 0 {
		out.ContainerPort = incoming.ContainerPort
	}
	if strings.TrimSpace(incoming.APIKey) != "" {
		out.APIKey = incoming.APIKey
	}
	if strings.TrimSpace(incoming.ManagementSecret) != "" {
		out.ManagementSecret = incoming.ManagementSecret
		out.ManagementSecretHashed = incoming.ManagementSecretHashed
	}
	if incoming.AllowRemoteManagement {
		out.AllowRemoteManagement = true
	}
	if !incoming.AllowRemoteManagement && base.AllowRemoteManagement != incoming.AllowRemoteManagement {
		out.AllowRemoteManagement = false
	}
	out.DisableControlPanel = incoming.DisableControlPanel
	out.Debug = incoming.Debug
	out.UsageStatisticsEnabled = incoming.UsageStatisticsEnabled
	if incoming.RequestRetry >= 0 {
		out.RequestRetry = incoming.RequestRetry
	}
	if strings.TrimSpace(incoming.AuthDir) != "" {
		out.AuthDir = incoming.AuthDir
	}
	if strings.TrimSpace(incoming.UpstreamBaseURLOverride) != "" {
		out.UpstreamBaseURLOverride = incoming.UpstreamBaseURLOverride
	}
	return out
}

func (m *Manager) loadState() (RuntimeState, error) {
	cfg := defaultDeployConfig(m.options.BaseDir)
	data, err := os.ReadFile(cfg.StateFile)
	if err != nil {
		return RuntimeState{}, err
	}
	var state RuntimeState
	if err = json.Unmarshal(data, &state); err != nil {
		return RuntimeState{}, err
	}
	return state, nil
}

func (m *Manager) saveState(cfg DeployConfig, release ReleaseInfo, lastBackup string) error {
	safeCfg := cfg
	safeCfg.ManagementSecret = ""
	state := RuntimeState{
		Config:           safeCfg,
		Release:          release,
		CurrentVersion:   release.CurrentVersion,
		LastBackup:       lastBackup,
		CurrentCommit:    "",
		CurrentBuildDate: "",
		UpdatedAt:        time.Now(),
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(cfg.StateFile), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(cfg.StateFile, data, 0o644)
}

func writeRuntimeState(state RuntimeState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(state.Config.StateFile, data, 0o644)
}

func fallbackReleaseFromState(state RuntimeState, currentVersion string) (ReleaseInfo, bool) {
	release := state.Release
	if strings.TrimSpace(release.CurrentVersion) == "" {
		release.CurrentVersion = strings.TrimSpace(state.CurrentVersion)
	}
	if strings.TrimSpace(release.CurrentVersion) == "" {
		release.CurrentVersion = extractImageTag(state.Config.Image)
	}
	if strings.TrimSpace(currentVersion) != "" {
		release.CurrentVersion = strings.TrimSpace(currentVersion)
	}
	if strings.TrimSpace(release.LatestVersion) == "" {
		release.LatestVersion = strings.TrimSpace(release.CurrentVersion)
	}
	if strings.TrimSpace(release.ReleaseURL) == "" && strings.TrimSpace(release.LatestVersion) != "" {
		release.ReleaseURL = defaultGitHubReleasePageBase + "/tag/" + release.LatestVersion
	}
	if strings.TrimSpace(release.CurrentVersion) == "" && strings.TrimSpace(release.LatestVersion) == "" {
		return ReleaseInfo{}, false
	}

	if strings.TrimSpace(release.CurrentVersion) != "" && strings.TrimSpace(release.LatestVersion) != "" {
		comparison := compareVersion(release.LatestVersion, release.CurrentVersion)
		if comparison < 0 {
			// GitHub 限流时可能只拿到旧缓存，但当前服务已成功升级到更高版本。
			// 这时不能再继续展示“最新版本更旧”的脏状态，直接回落为当前版本。
			release.LatestVersion = release.CurrentVersion
			release.ReleaseTitle = ""
			release.ReleaseNotes = ""
			release.OriginalReleaseNotes = ""
			release.ReleaseNotesLocale = ""
			release.ReleaseNotesModel = ""
			release.UpdateSummary = ""
			release.PublishedAt = ""
			release.ReleaseURL = defaultGitHubReleasePageBase + "/tag/" + release.CurrentVersion
			comparison = 0
		}
		if comparison > 0 {
			release.HasUpdate = true
			if release.BehindCount == 0 && len(release.MissingVersions) > 0 {
				release.BehindCount = len(release.MissingVersions)
			}
			if release.BehindCount == 0 {
				release.BehindCount = 1
			}
			if strings.TrimSpace(release.UpdateRecommendationLevel) == "" {
				release.UpdateRecommendationLevel = "high"
			}
			if strings.TrimSpace(release.UpdateRecommendation) == "" {
				release.UpdateRecommendation = "检测到新版本，但 GitHub 当前不可用，已回退到最近一次成功同步的版本信息。"
			}
		} else {
			release.HasUpdate = false
			release.BehindCount = 0
			release.MissingVersions = nil
			release.UpdateRecommendationLevel = "none"
			release.UpdateRecommendation = "当前已是最新版本，无需额外更新。"
		}
	}

	return release, true
}

func loadEnvFile(path string, current DeployConfig) (DeployConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DeployConfig{}, err
	}
	cfg := current
	reLine := regexp.MustCompile(`^([A-Z0-9_]+)=(.*)$`)
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		match := reLine.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		key := match[1]
		value := strings.TrimSpace(match[2])
		value = trimShellValue(value)
		switch key {
		case "CPA_BASE_DIR":
			cfg.BaseDir = value
		case "CPA_IMAGE":
			cfg.Image = value
		case "CPA_CONTAINER_NAME":
			cfg.ContainerName = value
		case "CPA_BIND_HOST":
			cfg.BindHost = value
		case "CPA_HOST_PORT":
			cfg.HostPort = atoiDefault(value, 0)
		case "CPA_API_KEY":
			cfg.APIKey = value
		case "CPA_MANAGEMENT_SECRET":
			cfg.ManagementSecret = value
			cfg.ManagementSecretHashed = isBcryptHash(value)
		case "CPA_ALLOW_REMOTE_MANAGEMENT":
			cfg.AllowRemoteManagement = parseBool(value)
		case "CPA_DISABLE_CONTROL_PANEL":
			cfg.DisableControlPanel = parseBool(value)
		case "CPA_DEBUG":
			cfg.Debug = parseBool(value)
		case "CPA_USAGE_STATISTICS_ENABLED":
			cfg.UsageStatisticsEnabled = parseBool(value)
		case "CPA_REQUEST_RETRY":
			cfg.RequestRetry = atoiDefault(value, defaultRequestRetry)
		}
	}
	return cfg, nil
}

func loadComposeFile(path string, current DeployConfig) (DeployConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DeployConfig{}, err
	}
	var file struct {
		Services map[string]struct {
			Image         string   `yaml:"image"`
			ContainerName string   `yaml:"container_name"`
			Ports         []string `yaml:"ports"`
			Volumes       []string `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err = yaml.Unmarshal(data, &file); err != nil {
		return DeployConfig{}, err
	}
	for _, service := range file.Services {
		cfg := current
		cfg.Image = strings.TrimSpace(service.Image)
		cfg.ContainerName = strings.TrimSpace(service.ContainerName)
		for _, port := range service.Ports {
			bindHost, hostPort, containerPort := parsePortMapping(strings.TrimSpace(port))
			if bindHost != "" {
				cfg.BindHost = bindHost
			}
			if hostPort > 0 {
				cfg.HostPort = hostPort
			}
			if containerPort > 0 {
				cfg.ContainerPort = containerPort
			}
			break
		}
		return cfg, nil
	}
	return DeployConfig{}, errors.New("compose 中未找到服务定义")
}

func loadRuntimeConfig(path string, current DeployConfig) (DeployConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DeployConfig{}, err
	}
	var file struct {
		Port             int `yaml:"port"`
		RemoteManagement struct {
			AllowRemote        bool   `yaml:"allow-remote"`
			SecretKey          string `yaml:"secret-key"`
			DisableControlPane bool   `yaml:"disable-control-panel"`
		} `yaml:"remote-management"`
		AuthDir                string   `yaml:"auth-dir"`
		Debug                  bool     `yaml:"debug"`
		UsageStatisticsEnabled bool     `yaml:"usage-statistics-enabled"`
		RequestRetry           int      `yaml:"request-retry"`
		APIKeys                []string `yaml:"api-keys"`
	}
	if err = yaml.Unmarshal(data, &file); err != nil {
		return DeployConfig{}, err
	}
	var root yaml.Node
	if err = yaml.Unmarshal(data, &root); err != nil {
		return DeployConfig{}, err
	}
	cfg := current

	rootMap := lookupDocumentMap(&root)
	if rootMap == nil {
		return current, nil
	}
	if node := lookupMapValue(rootMap, "port"); node != nil {
		cfg.ContainerPort = atoiDefault(node.Value, current.ContainerPort)
	}
	if remoteNode := lookupMapValue(rootMap, "remote-management"); remoteNode != nil && remoteNode.Kind == yaml.MappingNode {
		if node := lookupMapValue(remoteNode, "allow-remote"); node != nil {
			cfg.AllowRemoteManagement = parseBool(node.Value)
		}
		if node := lookupMapValue(remoteNode, "disable-control-panel"); node != nil {
			cfg.DisableControlPanel = parseBool(node.Value)
		}
	}
	if node := lookupMapValue(rootMap, "auth-dir"); node != nil {
		cfg.AuthDir = strings.TrimSpace(node.Value)
	}
	if node := lookupMapValue(rootMap, "debug"); node != nil {
		cfg.Debug = parseBool(node.Value)
	}
	if node := lookupMapValue(rootMap, "usage-statistics-enabled"); node != nil {
		cfg.UsageStatisticsEnabled = parseBool(node.Value)
	}
	if node := lookupMapValue(rootMap, "request-retry"); node != nil {
		cfg.RequestRetry = atoiDefault(node.Value, current.RequestRetry)
	}

	if len(file.APIKeys) > 0 && strings.TrimSpace(file.APIKeys[0]) != "" {
		cfg.APIKey = strings.TrimSpace(file.APIKeys[0])
	}
	secret := strings.TrimSpace(file.RemoteManagement.SecretKey)
	if secret != "" {
		if isBcryptHash(secret) && current.ManagementSecret != "" {
			// 已有原始密钥时保留它，避免被哈希值污染。
		} else {
			cfg.ManagementSecret = secret
			cfg.ManagementSecretHashed = isBcryptHash(secret)
		}
	}
	return cfg, nil
}

func (m *Manager) LatestRelease(ctx context.Context) (ReleaseInfo, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return ReleaseInfo{}, err
	}
	releases, err := m.releaseProvider.List(ctx, "")
	if err != nil {
		if state, stateErr := m.loadState(); stateErr == nil {
			if release, ok := fallbackReleaseFromState(state, ""); ok {
				return release, nil
			}
		}
		return ReleaseInfo{}, err
	}
	info := buildReleaseInfoFromGitHub(extractImageTag(cfg.Image), releases)
	if state, stateErr := m.loadState(); stateErr == nil && strings.TrimSpace(state.CurrentVersion) != "" {
		info.CurrentVersion = state.CurrentVersion
	}
	return info, nil
}

func (m *Manager) CheckUpdate(ctx context.Context, authToken string) (ReleaseInfo, error) {
	current, commit, buildDate := m.probeCurrentVersion(ctx, authToken)
	releases, err := m.releaseProvider.List(ctx, current)
	if err != nil {
		if state, stateErr := m.loadState(); stateErr == nil {
			if release, ok := fallbackReleaseFromState(state, current); ok {
				if strings.TrimSpace(current) != "" {
					state.CurrentVersion = current
				}
				state.CurrentCommit = commit
				state.CurrentBuildDate = buildDate
				state.Release = release
				state.UpdatedAt = time.Now()
				_ = writeRuntimeState(state)
				return release, nil
			}
		}
		return ReleaseInfo{}, err
	}
	release := buildReleaseInfoFromGitHub(current, releases)
	if cfg, cfgErr := m.loadConfig(); cfgErr == nil {
		lastBackup := ""
		if state, stateErr := m.loadState(); stateErr == nil {
			lastBackup = state.LastBackup
		}
		_ = m.saveState(cfg, release, lastBackup)
		if state, stateErr := m.loadState(); stateErr == nil {
			if current != "" {
				state.CurrentVersion = current
			}
			state.CurrentCommit = commit
			state.CurrentBuildDate = buildDate
			state.Release = release
			state.UpdatedAt = time.Now()
			_ = writeRuntimeState(state)
		}
	}
	return release, nil
}

func buildReleaseInfoFromGitHub(currentVersion string, releases []githubRelease) ReleaseInfo {
	currentVersion = strings.TrimSpace(currentVersion)
	if len(releases) == 0 {
		return ReleaseInfo{CurrentVersion: currentVersion}
	}

	latest := releases[0]
	missing := collectMissingReleases(currentVersion, releases)
	info := ReleaseInfo{
		CurrentVersion: currentVersion,
		LatestVersion:  latest.Version,
		ReleaseTitle:   strings.TrimSpace(latest.Title),
		ReleaseNotes:   strings.TrimSpace(latest.Notes),
		ReleaseURL:     strings.TrimSpace(latest.URL),
		PublishedAt:    strings.TrimSpace(latest.PublishedAt),
		HasUpdate:      len(missing) > 0,
	}
	if info.ReleaseURL == "" && info.LatestVersion != "" {
		info.ReleaseURL = defaultGitHubReleasePageBase + "/tag/" + info.LatestVersion
	}
	if len(missing) == 0 {
		level, advice := buildUpdateRecommendation(info.CurrentVersion, nil)
		info.UpdateRecommendationLevel = level
		info.UpdateRecommendation = advice
		return info
	}

	info.BehindCount = len(missing)
	info.MissingVersions = make([]string, 0, len(missing))
	for _, item := range missing {
		info.MissingVersions = append(info.MissingVersions, item.Version)
	}
	if len(missing) > 1 {
		info.ReleaseTitle = fmt.Sprintf("%s -> %s（共 %d 个版本）", blankFallback(info.CurrentVersion), info.LatestVersion, len(missing))
		info.ReleaseNotes = mergeReleaseNotes(missing)
	}
	level, advice := buildUpdateRecommendation(info.CurrentVersion, missing)
	info.UpdateRecommendationLevel = level
	info.UpdateRecommendation = advice
	return info
}

func collectMissingReleases(currentVersion string, releases []githubRelease) []githubRelease {
	if len(releases) == 0 {
		return nil
	}
	if strings.TrimSpace(currentVersion) == "" {
		return []githubRelease{releases[0]}
	}

	missingNewestFirst := make([]githubRelease, 0, len(releases))
	for _, release := range releases {
		if compareVersion(release.Version, currentVersion) > 0 {
			missingNewestFirst = append(missingNewestFirst, release)
			continue
		}
		break
	}
	reverseGitHubReleases(missingNewestFirst)
	return missingNewestFirst
}

func reverseGitHubReleases(releases []githubRelease) {
	for left, right := 0, len(releases)-1; left < right; left, right = left+1, right-1 {
		releases[left], releases[right] = releases[right], releases[left]
	}
}

func mergeReleaseNotes(releases []githubRelease) string {
	if len(releases) == 0 {
		return ""
	}
	if len(releases) == 1 {
		return strings.TrimSpace(releases[0].Notes)
	}

	var builder strings.Builder
	for index, release := range releases {
		if index > 0 {
			builder.WriteString("\n\n---\n\n")
		}
		builder.WriteString("## ")
		builder.WriteString(blankFallback(release.Version))
		if release.PublishedAt != "" {
			builder.WriteString("\n发布时间: ")
			builder.WriteString(strings.TrimSpace(release.PublishedAt))
		}
		if release.URL != "" {
			builder.WriteString("\nRelease: ")
			builder.WriteString(strings.TrimSpace(release.URL))
		}
		notes := strings.TrimSpace(release.Notes)
		if notes == "" {
			builder.WriteString("\n\n暂无 Release 说明")
			continue
		}
		builder.WriteString("\n\n")
		builder.WriteString(notes)
	}
	return builder.String()
}

func buildUpdateRecommendation(currentVersion string, missing []githubRelease) (string, string) {
	if len(missing) == 0 {
		return "none", "当前已是最新版本，无需额外更新。"
	}

	latest := missing[len(missing)-1]
	majorGap := versionMajor(latest.Version) - versionMajor(currentVersion)
	combined := strings.ToLower(strings.Join(extractReleaseTexts(missing), "\n"))

	switch {
	case strings.Contains(combined, "security") || strings.Contains(combined, "vulnerability") || strings.Contains(combined, "cve"):
		return "urgent", fmt.Sprintf("落后 %d 个版本，且包含安全相关修复，建议尽快更新。", len(missing))
	case majorGap > 0 || strings.Contains(combined, "breaking") || strings.Contains(combined, "migration") || strings.Contains(combined, "migrate"):
		return "high", fmt.Sprintf("落后 %d 个版本，并检测到破坏性变更或迁移提示，建议在维护窗口完成更新。", len(missing))
	case len(missing) >= 5:
		return "high", fmt.Sprintf("当前落后 %d 个版本，版本跨度较大，建议尽快安排升级并先做好备份。", len(missing))
	case len(missing) >= 2:
		return "medium", fmt.Sprintf("当前落后 %d 个版本，建议在低峰期更新，避免继续积累变更。", len(missing))
	default:
		return "low", fmt.Sprintf("当前落后 1 个版本，可按常规窗口更新到 %s。", latest.Version)
	}
}

func extractReleaseTexts(releases []githubRelease) []string {
	texts := make([]string, 0, len(releases))
	for _, release := range releases {
		texts = append(texts, strings.TrimSpace(release.Title)+"\n"+strings.TrimSpace(release.Notes))
	}
	return texts
}

func versionMajor(raw string) int {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "v"))
	if raw == "" {
		return 0
	}
	parts := strings.Split(raw, ".")
	return atoiDefault(parts[0], 0)
}

func blankFallback(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "未知"
	}
	return value
}

func trimShellValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
		value = value[1 : len(value)-1]
	}
	return strings.ReplaceAll(value, `'\''`, `'`)
}

func parseBool(raw string) bool {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func atoiDefault(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return value
}

func parsePortMapping(raw string) (string, int, int) {
	raw = strings.Trim(raw, `"`)
	parts := strings.Split(raw, ":")
	switch len(parts) {
	case 3:
		return parts[0], atoiDefault(parts[1], 0), atoiDefault(parts[2], 0)
	case 2:
		return "", atoiDefault(parts[0], 0), atoiDefault(parts[1], 0)
	default:
		return "", 0, 0
	}
}

func compareVersion(latest, current string) int {
	latest = strings.TrimSpace(strings.TrimPrefix(latest, "v"))
	current = strings.TrimSpace(strings.TrimPrefix(current, "v"))
	if latest == "" || current == "" {
		return 0
	}

	normalize := func(value string) []int {
		re := regexp.MustCompile(`\d+`)
		matches := re.FindAllString(value, -1)
		out := make([]int, 0, len(matches))
		for _, match := range matches {
			out = append(out, atoiDefault(match, 0))
		}
		return out
	}

	a := normalize(latest)
	b := normalize(current)
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		av := 0
		if i < len(a) {
			av = a[i]
		}
		bv := 0
		if i < len(b) {
			bv = b[i]
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	if latest == current {
		return 0
	}
	if latest > current {
		return 1
	}
	return -1
}

func isBcryptHash(raw string) bool {
	return strings.HasPrefix(raw, "$2a$") || strings.HasPrefix(raw, "$2b$") || strings.HasPrefix(raw, "$2y$")
}

func marshalYAMLNode(node *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		_ = encoder.Close()
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func lookupDocumentMap(root *yaml.Node) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	return root
}

func lookupMapValue(parent *yaml.Node, key string) *yaml.Node {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1]
		}
	}
	return nil
}
