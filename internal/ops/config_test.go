package ops

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCompareVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		latest   string
		current  string
		expected int
	}{
		{name: "newer", latest: "v1.2.3", current: "1.2.2", expected: 1},
		{name: "same", latest: "v1.2.3", current: "1.2.3", expected: 0},
		{name: "older", latest: "1.2.2", current: "1.2.3", expected: -1},
		{name: "missing", latest: "", current: "1.2.3", expected: 0},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if result := compareVersion(tc.latest, tc.current); result != tc.expected {
				t.Fatalf("compareVersion(%q, %q) = %d, want %d", tc.latest, tc.current, result, tc.expected)
			}
		})
	}
}

func TestMergeConfigFilePreservesUnknownFields(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	manager, err := NewManager(Options{BaseDir: baseDir, WorkspaceRoot: baseDir})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	cfg.APIKey = "api-key-1"
	cfg.ManagementSecret = "secret-1"
	cfg.ManagementSecretHashed = false
	cfg.Debug = true
	cfg.RequestRetry = 7

	if err = os.MkdirAll(filepath.Dir(cfg.ConfigFile), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	original := `port: 8317
custom-setting: keep-me
remote-management:
  allow-remote: false
  secret-key: old
debug: false
api-keys:
  - old-key
`
	if err = os.WriteFile(cfg.ConfigFile, []byte(original), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	if err = manager.syncConfigFile(cfg); err != nil {
		t.Fatalf("syncConfigFile failed: %v", err)
	}

	merged, err := os.ReadFile(cfg.ConfigFile)
	if err != nil {
		t.Fatalf("read merged config failed: %v", err)
	}
	content := string(merged)
	if !strings.Contains(content, "custom-setting: keep-me") {
		t.Fatalf("未知字段被覆盖了:\n%s", content)
	}
	if strings.Contains(content, "secret-key: secret-1") {
		t.Fatalf("管理密钥不应以明文写入 config.yaml:\n%s", content)
	}
	if !strings.Contains(content, "secret-key: $2") {
		t.Fatalf("管理密钥未写入 bcrypt 哈希:\n%s", content)
	}
	if !strings.Contains(content, "- api-key-1") {
		t.Fatalf("API Key 未更新:\n%s", content)
	}
}

func TestManagementSecretStaysPlainInLocalFilesAndHashedInRuntimeConfig(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	manager, err := NewManager(Options{BaseDir: baseDir, WorkspaceRoot: baseDir})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	cfg.APIKey = "api-key-1"
	cfg.ManagementSecret = "secret-1"

	if err = manager.persistDeploymentFiles(cfg); err != nil {
		t.Fatalf("persistDeploymentFiles failed: %v", err)
	}
	if err = manager.saveState(cfg, ReleaseInfo{CurrentVersion: "v1.0.0"}, ""); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	configData, err := os.ReadFile(cfg.ConfigFile)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	configText := string(configData)
	if strings.Contains(configText, "secret-key: secret-1") {
		t.Fatalf("config.yaml 不应保存明文管理密钥:\n%s", configText)
	}
	if !strings.Contains(configText, "secret-key: $2") {
		t.Fatalf("config.yaml 未写入 bcrypt 哈希:\n%s", configText)
	}

	envData, err := os.ReadFile(cfg.EnvFile)
	if err != nil {
		t.Fatalf("read env failed: %v", err)
	}
	if !strings.Contains(string(envData), "CPA_MANAGEMENT_SECRET='secret-1'") {
		t.Fatalf("env 文件未保存原始管理密钥:\n%s", string(envData))
	}
	envInfo, err := os.Stat(cfg.EnvFile)
	if err != nil {
		t.Fatalf("stat env failed: %v", err)
	}
	if envInfo.Mode().Perm() != 0o600 {
		t.Fatalf("env 文件权限 = %o, want 600", envInfo.Mode().Perm())
	}

	state, err := manager.loadState()
	if err != nil {
		t.Fatalf("loadState failed: %v", err)
	}
	if state.Config.ManagementSecret != "secret-1" {
		t.Fatalf("state 管理密钥 = %q, want %q", state.Config.ManagementSecret, "secret-1")
	}
	stateInfo, err := os.Stat(cfg.StateFile)
	if err != nil {
		t.Fatalf("stat state failed: %v", err)
	}
	if stateInfo.Mode().Perm() != 0o600 {
		t.Fatalf("state 文件权限 = %o, want 600", stateInfo.Mode().Perm())
	}

	if err = os.Remove(cfg.EnvFile); err != nil {
		t.Fatalf("remove env failed: %v", err)
	}
	loadedCfg, err := manager.CurrentConfig()
	if err != nil {
		t.Fatalf("CurrentConfig failed: %v", err)
	}
	if loadedCfg.ManagementSecret != "secret-1" {
		t.Fatalf("loaded 管理密钥 = %q, want %q", loadedCfg.ManagementSecret, "secret-1")
	}
	if loadedCfg.ManagementSecretHashed {
		t.Fatal("loaded 管理密钥不应被哈希值污染")
	}
}

func TestApplyOverridesKeepsDefaultRequestRetryWhenNotExplicit(t *testing.T) {
	t.Parallel()

	cfg := defaultDeployConfig(t.TempDir())
	overrides := OverrideConfig{RequestRetry: 0}

	updated := applyOverrides(cfg, overrides)

	if updated.RequestRetry != defaultRequestRetry {
		t.Fatalf("request retry = %d, want %d", updated.RequestRetry, defaultRequestRetry)
	}
}

func TestUninstallDryRunKeepsDataAndBackupsByDefault(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	manager, err := NewManager(Options{BaseDir: baseDir, WorkspaceRoot: baseDir})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	cfg.APIKey = "test-api-key"
	cfg.ManagementSecret = "test-management-secret"
	if err = seedExistingDeployment(cfg); err != nil {
		t.Fatalf("seedExistingDeployment failed: %v", err)
	}

	paths := []string{
		cfg.OperationLogFile,
		filepath.Join(cfg.BaseDir, "ops", "ops-state.json"),
		filepath.Join(cfg.DataDir, "auths", "keep.txt"),
		filepath.Join(cfg.BackupsDir, "backup.tar.gz"),
	}
	for _, path := range paths {
		if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err = os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}

	result, err := manager.Uninstall(t.Context(), nil, UninstallOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Uninstall dry-run failed: %v", err)
	}

	removed := strings.Join(result.Removed, "\n")
	kept := strings.Join(result.Kept, "\n")
	if !strings.Contains(removed, cfg.ComposeFile) || !strings.Contains(removed, cfg.ConfigFile) {
		t.Fatalf("dry-run 未列出托管文件:\n%s", removed)
	}
	if !strings.Contains(kept, cfg.DataDir) || !strings.Contains(kept, cfg.BackupsDir) {
		t.Fatalf("dry-run 未保留 data/backups:\n%s", kept)
	}
	if _, err = os.Stat(cfg.ConfigFile); err != nil {
		t.Fatalf("dry-run 不应删除文件: %v", err)
	}
}

type recordingDeployExecutor struct {
	downCalls atomic.Int32
}

func (e *recordingDeployExecutor) Pull(ctx context.Context, cfg DeployConfig, logger Logger) error {
	return nil
}

func (e *recordingDeployExecutor) Up(ctx context.Context, cfg DeployConfig, logger Logger) error {
	return nil
}

func (e *recordingDeployExecutor) Down(ctx context.Context, cfg DeployConfig, logger Logger) error {
	e.downCalls.Add(1)
	return nil
}

func (e *recordingDeployExecutor) RemoveContainer(ctx context.Context, cfg DeployConfig, logger Logger, containerName string) error {
	return nil
}

func (e *recordingDeployExecutor) InspectContainer(ctx context.Context, cfg DeployConfig, containerName string) (string, error) {
	return "", nil
}

func (e *recordingDeployExecutor) HasLocalImage(ctx context.Context, cfg DeployConfig, image string) bool {
	return false
}

func TestUninstallRemovesManagedFilesAndKeepsUserFiles(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	deployExecutor := &recordingDeployExecutor{}
	manager, err := newManagerWithDependencies(Options{BaseDir: baseDir, WorkspaceRoot: baseDir}, ManagerDependencies{
		ReleaseProviderFactory: stubReleaseProviderFactory{provider: &stubReleaseProvider{}},
		DeployExecutorFactory:  stubDeployExecutorFactory{executor: deployExecutor},
	})
	if err != nil {
		t.Fatalf("newManagerWithDependencies failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	cfg.APIKey = "test-api-key"
	cfg.ManagementSecret = "test-management-secret"
	if err = seedExistingDeployment(cfg); err != nil {
		t.Fatalf("seedExistingDeployment failed: %v", err)
	}

	managedPaths := []string{
		cfg.OperationLogFile,
		filepath.Join(cfg.BaseDir, "ops", "ops-state.json"),
		filepath.Join(cfg.BaseDir, "ops", "tasks", "task.log"),
	}
	keptPaths := []string{
		filepath.Join(cfg.DataDir, "auths", "keep.txt"),
		filepath.Join(cfg.BackupsDir, "backup.tar.gz"),
		filepath.Join(cfg.BaseDir, "custom", "user-note.txt"),
	}

	for _, path := range append(append([]string{}, managedPaths...), keptPaths...) {
		if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err = os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}

	result, err := manager.Uninstall(t.Context(), nil, UninstallOptions{})
	if err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}

	if deployExecutor.downCalls.Load() != 1 {
		t.Fatalf("compose down 调用次数 = %d", deployExecutor.downCalls.Load())
	}

	removed := strings.Join(result.Removed, "\n")
	if !strings.Contains(removed, cfg.ComposeFile) || !strings.Contains(removed, filepath.Join(cfg.BaseDir, "ops")) {
		t.Fatalf("removed 列表不完整:\n%s", removed)
	}

	for _, path := range []string{
		cfg.ComposeFile,
		cfg.EnvFile,
		cfg.ConfigFile,
		cfg.OperationLogFile,
		filepath.Join(cfg.BaseDir, "ops"),
	} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("托管路径未删除: %s err=%v", path, err)
		}
	}

	for _, path := range keptPaths {
		if _, err = os.Stat(path); err != nil {
			t.Fatalf("应保留的路径缺失: %s err=%v", path, err)
		}
	}

	if _, err = os.Stat(cfg.BaseDir); err != nil {
		t.Fatalf("存在用户文件时不应删除 baseDir: %v", err)
	}
}

func TestNewManagerRejectsBaseDirOutsideWorkspace(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	_, err := NewManager(Options{
		BaseDir:       filepath.Join(os.TempDir(), "cpa-outside-workspace"),
		WorkspaceRoot: workspaceRoot,
	})
	if err == nil {
		t.Fatal("expected outside workspace base dir to be rejected")
	}
	if !strings.Contains(err.Error(), "路径超出工作区") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigRejectsEscapedBaseDirFromEnvFile(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	baseDir := filepath.Join(workspaceRoot, ".cpa-docker")
	manager, err := NewManager(Options{
		BaseDir:       baseDir,
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	if err = os.MkdirAll(filepath.Dir(cfg.EnvFile), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	outsideBaseDir := filepath.Join(os.TempDir(), "cpa-escape-from-env")
	if err = os.WriteFile(cfg.EnvFile, []byte("CPA_BASE_DIR='"+outsideBaseDir+"'\n"), 0o644); err != nil {
		t.Fatalf("write env failed: %v", err)
	}

	_, err = manager.loadConfig()
	if err == nil {
		t.Fatal("expected escaped base dir from env file to be rejected")
	}
	if !strings.Contains(err.Error(), "路径超出工作区") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewManagerDiscoversManagedBaseDirWithinWorkspace(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	baseDir := filepath.Join(workspaceRoot, "deployments", ".cpa-docker")

	cfg := defaultDeployConfig(baseDir)
	cfg.Image = "eceasy/cli-proxy-api:v6.9.3"
	cfg.ContainerName = "cpa-legacy"
	cfg.APIKey = "legacy-api-key"
	cfg.ManagementSecret = "legacy-management-secret"
	if err := seedExistingDeployment(cfg); err != nil {
		t.Fatalf("seedExistingDeployment failed: %v", err)
	}

	manager, err := NewManager(Options{WorkspaceRoot: workspaceRoot})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	currentCfg, err := manager.CurrentConfig()
	if err != nil {
		t.Fatalf("CurrentConfig failed: %v", err)
	}
	if currentCfg.BaseDir != baseDir {
		t.Fatalf("base dir = %q, want %q", currentCfg.BaseDir, baseDir)
	}
	if currentCfg.ContainerName != "cpa-legacy" {
		t.Fatalf("container name = %q", currentCfg.ContainerName)
	}
}

func TestNewManagerRejectsMultipleManagedBaseDirs(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(workspaceRoot, "alpha", ".cpa-docker"),
		filepath.Join(workspaceRoot, "beta", ".cpa-docker"),
	} {
		cfg := defaultDeployConfig(dir)
		cfg.Image = "eceasy/cli-proxy-api:v6.9.3"
		cfg.APIKey = "test-api-key"
		cfg.ManagementSecret = "test-management-secret"
		if err := seedExistingDeployment(cfg); err != nil {
			t.Fatalf("seedExistingDeployment failed: %v", err)
		}
	}

	_, err := NewManager(Options{WorkspaceRoot: workspaceRoot})
	if err == nil {
		t.Fatal("expected multiple managed base dirs to be rejected")
	}
	if !strings.Contains(err.Error(), "多个可接管部署") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigRejectsInvalidBoolFromEnvFile(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	manager, err := NewManager(Options{BaseDir: baseDir, WorkspaceRoot: baseDir})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	if err = os.MkdirAll(filepath.Dir(cfg.EnvFile), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err = os.WriteFile(cfg.EnvFile, []byte("CPA_DEBUG=maybe\n"), 0o644); err != nil {
		t.Fatalf("write env failed: %v", err)
	}

	_, err = manager.loadConfig()
	if err == nil {
		t.Fatal("expected invalid bool from env file to be rejected")
	}
	if !strings.Contains(err.Error(), "CPA_DEBUG") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigRejectsInvalidHostPortFromEnvFile(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	manager, err := NewManager(Options{BaseDir: baseDir, WorkspaceRoot: baseDir})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	if err = os.MkdirAll(filepath.Dir(cfg.EnvFile), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err = os.WriteFile(cfg.EnvFile, []byte("CPA_HOST_PORT=70000\n"), 0o644); err != nil {
		t.Fatalf("write env failed: %v", err)
	}

	_, err = manager.loadConfig()
	if err == nil {
		t.Fatal("expected invalid host port from env file to be rejected")
	}
	if !strings.Contains(err.Error(), "CPA_HOST_PORT") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigRejectsInvalidBoolFromRuntimeConfig(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	manager, err := NewManager(Options{BaseDir: baseDir, WorkspaceRoot: baseDir})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	if err = os.MkdirAll(filepath.Dir(cfg.ConfigFile), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := `port: 8317
remote-management:
  allow-remote: enabled
`
	if err = os.WriteFile(cfg.ConfigFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err = manager.loadConfig()
	if err == nil {
		t.Fatal("expected invalid bool from runtime config to be rejected")
	}
	if !strings.Contains(err.Error(), "allow-remote") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigRejectsInvalidRequestRetryFromRuntimeConfig(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	manager, err := NewManager(Options{BaseDir: baseDir, WorkspaceRoot: baseDir})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	if err = os.MkdirAll(filepath.Dir(cfg.ConfigFile), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	content := `port: 8317
request-retry: -2
`
	if err = os.WriteFile(cfg.ConfigFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	_, err = manager.loadConfig()
	if err == nil {
		t.Fatal("expected invalid request retry from runtime config to be rejected")
	}
	if !strings.Contains(err.Error(), "request-retry") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildReleaseInfoFromGitHubMergesMissingReleases(t *testing.T) {
	t.Parallel()

	releases := []githubRelease{
		{
			Version:     "v1.3.0",
			Title:       "v1.3.0",
			Notes:       "feat: newest",
			URL:         "https://example.com/v1.3.0",
			PublishedAt: "2026-03-30T00:00:00Z",
		},
		{
			Version:     "v1.2.0",
			Title:       "v1.2.0",
			Notes:       "breaking: migrate config",
			URL:         "https://example.com/v1.2.0",
			PublishedAt: "2026-03-29T00:00:00Z",
		},
		{
			Version:     "v1.1.0",
			Title:       "v1.1.0",
			Notes:       "fix: keep config",
			URL:         "https://example.com/v1.1.0",
			PublishedAt: "2026-03-28T00:00:00Z",
		},
	}

	info := buildReleaseInfoFromGitHub("v1.0.0", releases)
	if !info.HasUpdate {
		t.Fatal("expected update to be detected")
	}
	if info.BehindCount != 3 {
		t.Fatalf("behind count = %d", info.BehindCount)
	}
	if strings.Join(info.MissingVersions, ",") != "v1.1.0,v1.2.0,v1.3.0" {
		t.Fatalf("missing versions = %#v", info.MissingVersions)
	}
	if !strings.Contains(info.ReleaseNotes, "## v1.1.0") || !strings.Contains(info.ReleaseNotes, "## v1.3.0") {
		t.Fatalf("merged release notes mismatch:\n%s", info.ReleaseNotes)
	}
	if info.UpdateRecommendationLevel != "high" {
		t.Fatalf("recommendation level = %q", info.UpdateRecommendationLevel)
	}
	if !strings.Contains(info.UpdateRecommendation, "迁移提示") {
		t.Fatalf("recommendation = %q", info.UpdateRecommendation)
	}
}

func TestCheckUpdateReturnsMergedReleaseRange(t *testing.T) {
	latestBackup := githubLatestReleaseURL
	releaseListBackup := githubReleaseListURL
	defer func() {
		githubLatestReleaseURL = latestBackup
		githubReleaseListURL = releaseListBackup
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/releases") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"tag_name":     "v1.3.0",
				"name":         "v1.3.0",
				"body":         "feat: newest",
				"html_url":     "https://example.com/v1.3.0",
				"published_at": "2026-03-30T00:00:00Z",
			},
			{
				"tag_name":     "v1.2.0",
				"name":         "v1.2.0",
				"body":         "fix: middle",
				"html_url":     "https://example.com/v1.2.0",
				"published_at": "2026-03-29T00:00:00Z",
			},
			{
				"tag_name":     "v1.1.0",
				"name":         "v1.1.0",
				"body":         "fix: oldest",
				"html_url":     "https://example.com/v1.1.0",
				"published_at": "2026-03-28T00:00:00Z",
			},
		})
	}))
	defer server.Close()

	githubLatestReleaseURL = server.URL + "/releases/latest"
	githubReleaseListURL = server.URL + "/releases"

	baseDir := t.TempDir()
	manager, err := NewManager(Options{BaseDir: baseDir, WorkspaceRoot: baseDir})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	cfg.Image = "eceasy/cli-proxy-api:v1.0.0"
	cfg.HostPort = 39081
	if err = manager.saveState(cfg, ReleaseInfo{CurrentVersion: "v1.0.0"}, ""); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	info, err := manager.CheckUpdate(context.Background(), "")
	if err != nil {
		t.Fatalf("CheckUpdate failed: %v", err)
	}
	if info.LatestVersion != "v1.3.0" {
		t.Fatalf("latest version = %q", info.LatestVersion)
	}
	if info.BehindCount != 3 {
		t.Fatalf("behind count = %d", info.BehindCount)
	}
	if !strings.Contains(info.ReleaseNotes, "## v1.1.0") || !strings.Contains(info.ReleaseNotes, "## v1.3.0") {
		t.Fatalf("merged notes mismatch:\n%s", info.ReleaseNotes)
	}
}

func TestCheckUpdateFallsBackToSavedStateWhenGitHubRateLimited(t *testing.T) {
	latestBackup := githubLatestReleaseURL
	releaseListBackup := githubReleaseListURL
	defer func() {
		githubLatestReleaseURL = latestBackup
		githubReleaseListURL = releaseListBackup
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/releases"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
		case r.URL.Path == "/v0/management/config":
			w.Header().Set("X-CPA-VERSION", "v1.3.0")
			w.Header().Set("X-CPA-COMMIT", "abc1234")
			w.Header().Set("X-CPA-BUILD-DATE", "2026-03-30T12:00:00Z")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	githubLatestReleaseURL = server.URL + "/releases/latest"
	githubReleaseListURL = server.URL + "/releases"

	baseDir := t.TempDir()
	manager, err := NewManager(Options{
		BaseDir:         baseDir,
		WorkspaceRoot:   baseDir,
		UpstreamBaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	cfg.Image = defaultImage
	cfg.APIKey = "test-api-key"
	if err = manager.saveState(cfg, ReleaseInfo{
		CurrentVersion:       "v1.0.0",
		LatestVersion:        "v1.3.0",
		HasUpdate:            true,
		BehindCount:          3,
		MissingVersions:      []string{"v1.1.0", "v1.2.0", "v1.3.0"},
		ReleaseTitle:         "v1.0.0 -> v1.3.0（共 3 个版本）",
		ReleaseNotes:         "## v1.1.0\nfix: keep config",
		UpdateRecommendation: "建议尽快更新。",
		PublishedAt:          "2026-03-30T00:00:00Z",
	}, "backup.tar.gz"); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	info, err := manager.CheckUpdate(context.Background(), "")
	if err != nil {
		t.Fatalf("CheckUpdate fallback failed: %v", err)
	}
	if info.CurrentVersion != "v1.3.0" {
		t.Fatalf("current version = %q", info.CurrentVersion)
	}
	if info.LatestVersion != "v1.3.0" {
		t.Fatalf("latest version = %q", info.LatestVersion)
	}
	if info.HasUpdate {
		t.Fatal("expected fallback to show latest after successful probe")
	}
	if info.BehindCount != 0 {
		t.Fatalf("behind count = %d", info.BehindCount)
	}
	if info.UpdateRecommendation != "当前已是最新版本，无需额外更新。" {
		t.Fatalf("unexpected recommendation = %q", info.UpdateRecommendation)
	}
	if info.ReleaseURL != "https://github.com/router-for-me/CLIProxyAPI/releases/tag/v1.3.0" {
		t.Fatalf("release url = %q", info.ReleaseURL)
	}

	state, err := manager.loadState()
	if err != nil {
		t.Fatalf("loadState failed: %v", err)
	}
	if state.CurrentVersion != "v1.3.0" {
		t.Fatalf("state current version = %q", state.CurrentVersion)
	}
	if state.Release.CurrentVersion != "v1.3.0" {
		t.Fatalf("state release current version = %q", state.Release.CurrentVersion)
	}
	if state.Release.LatestVersion != "v1.3.0" {
		t.Fatalf("state release latest version = %q", state.Release.LatestVersion)
	}
	if state.Release.ReleaseURL != "https://github.com/router-for-me/CLIProxyAPI/releases/tag/v1.3.0" {
		t.Fatalf("state release url = %q", state.Release.ReleaseURL)
	}
}

func TestFallbackReleaseFromStateUsesCurrentVersionWhenCacheIsOlder(t *testing.T) {
	t.Parallel()

	info, ok := fallbackReleaseFromState(RuntimeState{
		Config: DeployConfig{
			Image: defaultImage,
		},
		CurrentVersion: "v6.9.3",
		Release: ReleaseInfo{
			CurrentVersion:       "v6.9.3",
			LatestVersion:        "v6.9.3",
			HasUpdate:            false,
			UpdateRecommendation: "当前已是最新版本，无需额外更新。",
			ReleaseURL:           "https://github.com/router-for-me/CLIProxyAPI/releases/tag/v6.9.3",
		},
	}, "v6.9.6")
	if !ok {
		t.Fatal("expected fallback release")
	}
	if info.CurrentVersion != "v6.9.6" {
		t.Fatalf("current version = %q", info.CurrentVersion)
	}
	if info.LatestVersion != "v6.9.6" {
		t.Fatalf("latest version = %q", info.LatestVersion)
	}
	if info.HasUpdate {
		t.Fatal("expected no update when current version is ahead of cached latest")
	}
	if info.ReleaseURL != "https://github.com/router-for-me/CLIProxyAPI/releases/tag/v6.9.6" {
		t.Fatalf("release url = %q", info.ReleaseURL)
	}
	if info.UpdateRecommendation != "当前已是最新版本，无需额外更新。" {
		t.Fatalf("unexpected recommendation = %q", info.UpdateRecommendation)
	}
	if info.ReleaseNotes != "" {
		t.Fatalf("expected stale release notes to be cleared, got %q", info.ReleaseNotes)
	}
}

func TestFetchGitHubReleasesRetriesTransientEOF(t *testing.T) {
	latestBackup := githubLatestReleaseURL
	releaseListBackup := githubReleaseListURL
	defer func() {
		githubLatestReleaseURL = latestBackup
		githubReleaseListURL = releaseListBackup
	}()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/releases") {
			http.NotFound(w, r)
			return
		}
		if attempts.Add(1) == 1 {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("response writer does not support hijacking")
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Fatalf("hijack failed: %v", err)
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"tag_name":     "v1.3.0",
				"name":         "v1.3.0",
				"body":         "feat: newest",
				"html_url":     "https://example.com/v1.3.0",
				"published_at": "2026-03-30T00:00:00Z",
			},
		})
	}))
	defer server.Close()

	githubLatestReleaseURL = server.URL + "/releases/latest"
	githubReleaseListURL = server.URL + "/releases"

	baseDir := t.TempDir()
	manager, err := NewManager(Options{BaseDir: baseDir, WorkspaceRoot: baseDir})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	releases, err := manager.releaseProvider.List(context.Background(), "")
	if err != nil {
		t.Fatalf("release provider list failed: %v", err)
	}
	if len(releases) != 1 || releases[0].Version != "v1.3.0" {
		t.Fatalf("unexpected releases: %#v", releases)
	}
	if attempts.Load() < 2 {
		t.Fatalf("expected retry, attempts = %d", attempts.Load())
	}
}

func TestShouldRetryGitHubError(t *testing.T) {
	t.Parallel()

	if !shouldRetryGitHubError(io.EOF) {
		t.Fatal("EOF should be retriable")
	}
	if !shouldRetryGitHubError(&net.DNSError{IsTimeout: true}) {
		t.Fatal("timeout network error should be retriable")
	}
	if shouldRetryGitHubError(context.Canceled) {
		t.Fatal("context canceled should not be retriable")
	}
}

func TestProbeCurrentVersionStopsRetryingWhenOnlyHashedSecretIsAvailable(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/config" {
			http.NotFound(w, r)
			return
		}
		attempts.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	baseDir := t.TempDir()
	manager, err := NewManager(Options{
		BaseDir:         baseDir,
		WorkspaceRoot:   baseDir,
		UpstreamBaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	cfg.Image = "eceasy/cli-proxy-api:v1.2.3"
	cfg.RequestRetry = 5
	cfg.ManagementSecret = "$2a$10$abcdefghijklmnopqrstuv1234567890abcdefghijklmn"
	cfg.ManagementSecretHashed = true
	if err = os.MkdirAll(filepath.Dir(cfg.ConfigFile), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err = writeInitialConfigFile(cfg); err != nil {
		t.Fatalf("writeInitialConfigFile failed: %v", err)
	}
	if err = manager.saveState(cfg, ReleaseInfo{CurrentVersion: "v1.2.3"}, ""); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	version, _, _ := manager.probeCurrentVersion(context.Background(), "")
	if version != "v1.2.3" {
		t.Fatalf("version = %q, want %q", version, "v1.2.3")
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1", attempts.Load())
	}
}
