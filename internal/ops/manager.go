package ops

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (m *Manager) CurrentConfig() (DeployConfig, error) {
	return m.loadConfig()
}

func (m *Manager) UpstreamBaseURL() (string, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return "", err
	}
	return m.upstreamBaseURL(cfg), nil
}

func (m *Manager) Install(ctx context.Context, logger Logger) error {
	return m.withOperationLock(func() error {
		cfg, err := m.prepareConfig(true)
		if err != nil {
			return err
		}
		if _, statErr := os.Stat(cfg.ComposeFile); statErr == nil {
			return errors.New("检测到已有部署，请改用 repair 或 update")
		}
		if err = m.persistDeploymentFiles(cfg); err != nil {
			return err
		}
		if err = m.deployExecutor.Pull(ctx, cfg, logger); err != nil {
			return err
		}
		if err = m.deployExecutor.Up(ctx, cfg, logger); err != nil {
			return err
		}
		return m.persistRuntimeState(ctx, cfg, cfg.ManagementSecret, "")
	})
}

func (m *Manager) Update(ctx context.Context, logger Logger, authToken string) error {
	return m.withOperationLock(func() error {
		cfg, err := m.prepareConfig(false)
		if err != nil {
			return err
		}
		cfg.Image = m.resolveUpdateImage(cfg)
		snapshot, err := m.Backup(ctx, logger)
		if err != nil {
			return err
		}
		if err = m.persistDeploymentFiles(cfg); err != nil {
			return err
		}
		if err = m.deployExecutor.Pull(ctx, cfg, logger); err != nil {
			return err
		}
		if err = m.deployExecutor.Up(ctx, cfg, logger); err != nil {
			return err
		}
		return m.persistRuntimeState(ctx, cfg, m.resolveManagementToken(cfg, authToken), snapshot.Name)
	})
}

func (m *Manager) resolveUpdateImage(cfg DeployConfig) string {
	if m.options.Overrides.ImageExplicit {
		return strings.TrimSpace(cfg.Image)
	}
	return defaultImage
}

func (m *Manager) Repair(ctx context.Context, logger Logger) error {
	return m.withOperationLock(func() error {
		cfg, err := m.prepareConfig(false)
		if err != nil {
			return err
		}
		lastBackup := ""
		if state, stateErr := m.loadState(); stateErr == nil {
			lastBackup = strings.TrimSpace(state.LastBackup)
		}
		if err = m.persistDeploymentFiles(cfg); err != nil {
			return err
		}
		if err = m.deployExecutor.Up(ctx, cfg, logger); err != nil {
			return err
		}
		return m.persistRuntimeState(ctx, cfg, cfg.ManagementSecret, lastBackup)
	})
}

func (m *Manager) Backup(ctx context.Context, logger Logger) (Snapshot, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return Snapshot{}, err
	}
	if err = os.MkdirAll(cfg.BackupsDir, 0o755); err != nil {
		return Snapshot{}, err
	}
	now := time.Now()
	name := now.Format("20060102-150405") + ".tar.gz"
	target := filepath.Join(cfg.BackupsDir, name)

	if err = m.writeOperationLog(cfg, "开始创建备份 %s", target); err != nil {
		return Snapshot{}, err
	}
	if logger != nil {
		logger.Printf("开始创建备份: %s", target)
	}

	file, err := os.Create(target)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = file.Close() }()

	gz := gzip.NewWriter(file)
	defer func() { _ = gz.Close() }()

	tw := tar.NewWriter(gz)
	defer func() { _ = tw.Close() }()

	entries := []string{
		cfg.ComposeFile,
		cfg.EnvFile,
		cfg.OperationLogFile,
		cfg.StateFile,
		cfg.DataDir,
	}
	seen := map[string]struct{}{}
	for _, path := range entries {
		if path == "" {
			continue
		}
		cleanPath := filepath.Clean(path)
		if _, ok := seen[cleanPath]; ok {
			continue
		}
		seen[cleanPath] = struct{}{}
		if err = m.addBackupPath(tw, cfg.BaseDir, cleanPath); err != nil {
			return Snapshot{}, err
		}
	}

	snapshot := Snapshot{
		Name:      name,
		Path:      target,
		CreatedAt: now,
	}

	state, stateErr := m.loadState()
	if stateErr == nil {
		state.Config = cfg
		if strings.TrimSpace(state.Release.CurrentVersion) == "" {
			state.Release.CurrentVersion = strings.TrimSpace(state.CurrentVersion)
		}
		if strings.TrimSpace(state.CurrentVersion) == "" {
			state.CurrentVersion = strings.TrimSpace(state.Release.CurrentVersion)
		}
		state.LastBackup = snapshot.Name
		state.UpdatedAt = time.Now()
		_ = writeRuntimeState(state)
	}
	if logger != nil {
		logger.Printf("备份完成: %s", target)
	}
	return snapshot, nil
}

func (m *Manager) Restore(ctx context.Context, logger Logger, snapshotName string) error {
	return m.withOperationLock(func() error {
		cfg, err := m.loadConfig()
		if err != nil {
			return err
		}
		snapshotPath, err := m.resolveSnapshotPath(cfg, snapshotName)
		if err != nil {
			return err
		}

		if _, statErr := os.Stat(cfg.ComposeFile); statErr == nil {
			_ = m.deployExecutor.Down(ctx, cfg, logger)
		}
		if err = m.extractBackup(snapshotPath, cfg.BaseDir); err != nil {
			return err
		}
		cfg, err = m.prepareConfig(false)
		if err != nil {
			return err
		}
		if err = m.persistDeploymentFiles(cfg); err != nil {
			return err
		}
		if err = m.deployExecutor.Up(ctx, cfg, logger); err != nil {
			return err
		}
		return m.persistRuntimeState(ctx, cfg, cfg.ManagementSecret, filepath.Base(snapshotPath))
	})
}

func (m *Manager) Uninstall(ctx context.Context, logger Logger, options UninstallOptions) (UninstallResult, error) {
	var result UninstallResult
	result.DryRun = options.DryRun

	err := m.withOperationLock(func() error {
		cfg, err := m.loadConfig()
		if err != nil {
			return err
		}

		if _, statErr := os.Stat(cfg.ComposeFile); statErr == nil {
			if options.DryRun {
				result.Removed = append(result.Removed, "docker compose down --remove-orphans")
			} else {
				if err = m.deployExecutor.Down(ctx, cfg, logger); err != nil {
					return err
				}
			}
		} else if status, statusErr := m.Status(ctx); statusErr == nil && status.State != "not_found" {
			if options.DryRun {
				result.Removed = append(result.Removed, "docker rm -f "+cfg.ContainerName)
			} else {
				if err = m.deployExecutor.RemoveContainer(ctx, cfg, logger, cfg.ContainerName); err != nil {
					return err
				}
			}
		}

		removePaths := []string{
			cfg.ComposeFile,
			cfg.EnvFile,
			cfg.ConfigFile,
			cfg.OperationLogFile,
			filepath.Join(cfg.BaseDir, "ops"),
		}
		keepPaths := []string{
			cfg.DataDir,
			cfg.BackupsDir,
		}
		if options.PurgeData {
			removePaths = append(removePaths, cfg.DataDir)
			keepPaths = removePathsWithout(keepPaths, cfg.DataDir)
		}
		if options.PurgeBackups {
			removePaths = append(removePaths, cfg.BackupsDir)
			keepPaths = removePathsWithout(keepPaths, cfg.BackupsDir)
		}

		for _, path := range uniqueCleanPaths(removePaths) {
			if path == "" {
				continue
			}
			if _, statErr := os.Stat(path); statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					continue
				}
				return statErr
			}
			result.Removed = append(result.Removed, path)
			if options.DryRun {
				continue
			}
			if err = removeManagedPath(path); err != nil {
				return err
			}
		}

		for _, path := range uniqueCleanPaths(keepPaths) {
			if path == "" {
				continue
			}
			if _, statErr := os.Stat(path); statErr == nil {
				result.Kept = append(result.Kept, path)
			}
		}

		if !options.DryRun {
			_ = removeIfEmpty(cfg.BaseDir)
		}
		return nil
	})
	return result, err
}

func (m *Manager) Status(ctx context.Context) (Status, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return Status{}, err
	}

	output, err := m.deployExecutor.InspectContainer(ctx, cfg, cfg.ContainerName)
	if err != nil {
		return Status{
			ContainerName: cfg.ContainerName,
			State:         "not_found",
			Image:         cfg.Image,
			Ports:         fmt.Sprintf("%s:%d->%d", cfg.BindHost, cfg.HostPort, cfg.ContainerPort),
		}, nil
	}

	var payload struct {
		Config struct {
			Image string `json:"Image"`
		} `json:"Config"`
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
		NetworkSettings struct {
			Ports map[string][]struct {
				HostIP   string `json:"HostIp"`
				HostPort string `json:"HostPort"`
			} `json:"Ports"`
		} `json:"NetworkSettings"`
		Name string `json:"Name"`
	}
	if err = json.Unmarshal([]byte(output), &payload); err != nil {
		return Status{}, err
	}

	ports := make([]string, 0)
	for key, bindings := range payload.NetworkSettings.Ports {
		if len(bindings) == 0 {
			continue
		}
		for _, binding := range bindings {
			ports = append(ports, fmt.Sprintf("%s:%s->%s", binding.HostIP, binding.HostPort, key))
		}
	}
	sort.Strings(ports)

	name := strings.TrimPrefix(payload.Name, "/")
	if name == "" {
		name = cfg.ContainerName
	}

	return Status{
		ContainerName: name,
		State:         strings.TrimSpace(payload.State.Status),
		Image:         blankString(payload.Config.Image, cfg.Image),
		Ports:         strings.Join(ports, ", "),
	}, nil
}

func (m *Manager) Info(ctx context.Context, authToken string) (Info, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return Info{}, err
	}
	status, err := m.Status(ctx)
	if err != nil {
		return Info{}, err
	}
	version, err := m.CheckUpdate(ctx, authToken)
	if err != nil {
		version = ReleaseInfo{
			CurrentVersion: extractImageTag(cfg.Image),
		}
	}
	lastBackup := ""
	if state, stateErr := m.loadState(); stateErr == nil {
		lastBackup = state.LastBackup
	}
	return Info{
		Config:     cfg,
		Status:     status,
		Version:    version,
		LastBackup: lastBackup,
	}, nil
}

func (m *Manager) ReadOperationLog(ctx context.Context, lines int) (string, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(cfg.OperationLogFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if lines <= 0 {
		return string(data), nil
	}
	parts := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) <= lines {
		return strings.Join(parts, "\n"), nil
	}
	return strings.Join(parts[len(parts)-lines:], "\n"), nil
}

func (m *Manager) probeCurrentVersion(ctx context.Context, authToken string) (string, string, string) {
	cfg, err := m.loadConfig()
	if err != nil {
		return "", "", ""
	}
	token := m.resolveManagementToken(cfg, authToken)
	stopOnAuthFailure := token == "" && cfg.ManagementSecretHashed
	endpoint := strings.TrimRight(m.upstreamBaseURL(cfg), "/") + "/v0/management/config"
	client := &http.Client{Timeout: 5 * time.Second}
	attempts := cfg.RequestRetry + 1
	if attempts < 1 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if reqErr == nil {
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			resp, doErr := client.Do(req)
			if doErr == nil {
				func() {
					defer func() { _ = resp.Body.Close() }()
					_, _ = io.Copy(io.Discard, resp.Body)
				}()
				version := strings.TrimSpace(resp.Header.Get("X-CPA-VERSION"))
				commit := strings.TrimSpace(resp.Header.Get("X-CPA-COMMIT"))
				buildDate := strings.TrimSpace(resp.Header.Get("X-CPA-BUILD-DATE"))
				if version != "" {
					return version, commit, buildDate
				}
				if stopOnAuthFailure && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
					break
				}
			}
		}
		if i < attempts-1 {
			select {
			case <-ctx.Done():
				return "", "", ""
			case <-time.After(2 * time.Second):
			}
		}
	}

	if state, stateErr := m.loadState(); stateErr == nil && strings.TrimSpace(state.CurrentVersion) != "" {
		return state.CurrentVersion, state.CurrentCommit, state.CurrentBuildDate
	}
	return extractImageTag(cfg.Image), "", ""
}

func (m *Manager) prepareConfig(generateSecrets bool) (DeployConfig, error) {
	cfg, err := m.loadConfig()
	if err != nil {
		return DeployConfig{}, err
	}
	if generateSecrets {
		if strings.TrimSpace(cfg.APIKey) == "" {
			cfg.APIKey, err = randomSecret(32)
			if err != nil {
				return DeployConfig{}, err
			}
		}
		if strings.TrimSpace(cfg.ManagementSecret) == "" {
			cfg.ManagementSecret, err = randomSecret(32)
			if err != nil {
				return DeployConfig{}, err
			}
			cfg.ManagementSecretHashed = false
		}
	}
	cfg = finalizeDeployConfig(cfg, m.options.UpstreamBaseURL)
	return cfg, nil
}

func (m *Manager) persistDeploymentFiles(cfg DeployConfig) error {
	if err := os.MkdirAll(cfg.BaseDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.StateFile), 0o755); err != nil {
		return err
	}
	if err := writeEnvFileAtomic(cfg); err != nil {
		return err
	}
	if err := writeComposeFileAtomic(cfg); err != nil {
		return err
	}
	return m.syncConfigFile(cfg)
}

func (m *Manager) persistRuntimeState(ctx context.Context, cfg DeployConfig, authToken string, lastBackup string) error {
	release := ReleaseInfo{
		CurrentVersion: extractImageTag(cfg.Image),
	}
	if latest, err := m.LatestRelease(ctx); err == nil {
		release.LatestVersion = latest.LatestVersion
		release.ReleaseTitle = latest.ReleaseTitle
		release.ReleaseNotes = latest.ReleaseNotes
		release.ReleaseURL = latest.ReleaseURL
		release.PublishedAt = latest.PublishedAt
	}
	currentVersion, commit, buildDate := m.probeCurrentVersion(ctx, authToken)
	if currentVersion != "" {
		release.CurrentVersion = currentVersion
	}
	if err := m.saveState(cfg, release, lastBackup); err != nil {
		return err
	}
	state, err := m.loadState()
	if err != nil {
		return nil
	}
	state.CurrentCommit = commit
	state.CurrentBuildDate = buildDate
	state.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil
	}
	return writeFileAtomic(cfg.StateFile, data, 0o600)
}

func (m *Manager) resolveManagementToken(cfg DeployConfig, provided string) string {
	if strings.TrimSpace(provided) != "" {
		return strings.TrimSpace(provided)
	}
	if strings.TrimSpace(cfg.ManagementSecret) != "" && !cfg.ManagementSecretHashed {
		return strings.TrimSpace(cfg.ManagementSecret)
	}
	return ""
}

func (m *Manager) upstreamBaseURL(cfg DeployConfig) string {
	if strings.TrimSpace(cfg.UpstreamBaseURLOverride) != "" {
		return strings.TrimRight(strings.TrimSpace(cfg.UpstreamBaseURLOverride), "/")
	}
	host := strings.TrimSpace(cfg.BindHost)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if parsedIP := net.ParseIP(host); parsedIP != nil && parsedIP.To4() == nil && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("http://%s:%d", host, cfg.HostPort)
}

func (m *Manager) writeOperationLog(cfg DeployConfig, format string, args ...any) error {
	if err := os.MkdirAll(filepath.Dir(cfg.OperationLogFile), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(cfg.OperationLogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	line := fmt.Sprintf(format, args...)
	_, err = fmt.Fprintf(file, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), line)
	return err
}

func (m *Manager) addBackupPath(tw *tar.Writer, baseDir, target string) error {
	if _, err := os.Stat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return filepath.Walk(target, func(path string, currentInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}
		relPath = filepath.ToSlash(relPath)
		if !currentInfo.Mode().IsRegular() {
			header, err := tar.FileInfoHeader(currentInfo, "")
			if err != nil {
				return err
			}
			header.Name = relPath
			if currentInfo.IsDir() {
				header.Name += "/"
			}
			if err = tw.WriteHeader(header); err != nil {
				return err
			}
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()

		fileInfo, err := file.Stat()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(fileInfo, "")
		if err != nil {
			return err
		}
		header.Name = relPath
		if err = tw.WriteHeader(header); err != nil {
			return err
		}

		if _, err = io.CopyN(tw, file, fileInfo.Size()); err != nil {
			return err
		}
		return nil
	})
}

func (m *Manager) resolveSnapshotPath(cfg DeployConfig, snapshotName string) (string, error) {
	if strings.TrimSpace(snapshotName) != "" {
		path := filepath.Join(cfg.BackupsDir, snapshotName)
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return path, nil
	}
	entries, err := os.ReadDir(cfg.BackupsDir)
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".tar.gz") {
			candidates = append(candidates, entry.Name())
		}
	}
	if len(candidates) == 0 {
		return "", errors.New("未找到可恢复的备份")
	}
	sort.Strings(candidates)
	return filepath.Join(cfg.BackupsDir, candidates[len(candidates)-1]), nil
}

func (m *Manager) extractBackup(snapshotPath, baseDir string) error {
	file, err := os.Open(snapshotPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(baseDir, filepath.Clean(header.Name))
		if !strings.HasPrefix(target, filepath.Clean(baseDir)+string(os.PathSeparator)) && filepath.Clean(target) != filepath.Clean(baseDir) {
			return fmt.Errorf("非法备份路径: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			fileMode := os.FileMode(header.Mode)
			if fileMode == 0 {
				fileMode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode)
			if err != nil {
				return err
			}
			if _, err = io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			if err = out.Close(); err != nil {
				return err
			}
		}
	}
}

func writeEnvFileAtomic(cfg DeployConfig) error {
	content := strings.Join([]string{
		"# CLIProxyApi-OPS generated file",
		"CPA_BASE_DIR=" + shellQuote(cfg.BaseDir),
		"CPA_IMAGE=" + shellQuote(cfg.Image),
		"CPA_CONTAINER_NAME=" + shellQuote(cfg.ContainerName),
		"CPA_BIND_HOST=" + shellQuote(cfg.BindHost),
		fmt.Sprintf("CPA_HOST_PORT=%d", cfg.HostPort),
		"CPA_API_KEY=" + shellQuote(cfg.APIKey),
		"CPA_MANAGEMENT_SECRET=" + shellQuote(cfg.ManagementSecret),
		"CPA_ALLOW_REMOTE_MANAGEMENT=" + boolToYAML(cfg.AllowRemoteManagement),
		"CPA_DISABLE_CONTROL_PANEL=" + boolToYAML(cfg.DisableControlPanel),
		"CPA_DEBUG=" + boolToYAML(cfg.Debug),
		"CPA_USAGE_STATISTICS_ENABLED=" + boolToYAML(cfg.UsageStatisticsEnabled),
		fmt.Sprintf("CPA_REQUEST_RETRY=%d", cfg.RequestRetry),
		"",
	}, "\n")
	return writeFileAtomic(cfg.EnvFile, []byte(content), 0o600)
}

func writeComposeFileAtomic(cfg DeployConfig) error {
	volumeSpec := "./data"
	if filepath.Clean(cfg.DataDir) != filepath.Join(cfg.BaseDir, "data") {
		volumeSpec = cfg.DataDir
	}
	content := fmt.Sprintf(`services:
  cpa:
    image: %s
    container_name: %s
    command: ["/CLIProxyAPI/CLIProxyAPI", "--config", "/data/config.yaml"]
    restart: unless-stopped
    environment:
      DEPLOY: cloud
    ports:
      - "%s:%d:%d"
    volumes:
      - "%s:/data"
`, cfg.Image, cfg.ContainerName, cfg.BindHost, cfg.HostPort, cfg.ContainerPort, volumeSpec)
	return writeFileAtomic(cfg.ComposeFile, []byte(content), 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tempName := tempFile.Name()
	defer func() { _ = os.Remove(tempName) }()
	if _, err = tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err = tempFile.Chmod(mode); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err = tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func extractImageTag(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	if index := strings.LastIndex(image, ":"); index >= 0 && index < len(image)-1 {
		return image[index+1:]
	}
	return image
}

func blankString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return strings.TrimSpace(fallback)
	}
	return strings.TrimSpace(value)
}

func uniqueCleanPaths(paths []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		cleanPath := filepath.Clean(strings.TrimSpace(path))
		if cleanPath == "." || cleanPath == "" {
			continue
		}
		if _, ok := seen[cleanPath]; ok {
			continue
		}
		seen[cleanPath] = struct{}{}
		result = append(result, cleanPath)
	}
	sort.Slice(result, func(i, j int) bool {
		return len(result[i]) > len(result[j])
	})
	return result
}

func removePathsWithout(paths []string, target string) []string {
	target = filepath.Clean(strings.TrimSpace(target))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if filepath.Clean(strings.TrimSpace(path)) == target {
			continue
		}
		result = append(result, path)
	}
	return result
}

func removeManagedPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func removeIfEmpty(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(entries) > 0 {
		return nil
	}
	return os.Remove(path)
}

func randomSecret(length int) (string, error) {
	if length <= 0 {
		length = 32
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (m *Manager) withOperationLock(fn func() error) error {
	cfg := defaultDeployConfig(m.options.BaseDir)
	lockPath := filepath.Join(cfg.BaseDir, "ops", "operation.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return err
	}
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("已有运维任务正在执行，请稍后再试")
		}
		return err
	}
	defer func() {
		_ = lockFile.Close()
		_ = os.Remove(lockPath)
	}()
	_, _ = fmt.Fprintf(lockFile, "pid=%d\ntime=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	return fn()
}
