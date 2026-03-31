package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveUpdateImage(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	manager, err := NewManager(Options{
		BaseDir:       baseDir,
		WorkspaceRoot: baseDir,
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if got := manager.resolveUpdateImage(DeployConfig{Image: "eceasy/cli-proxy-api:v6.9.3"}); got != defaultImage {
		t.Fatalf("resolveUpdateImage() = %q, want %q", got, defaultImage)
	}

	explicitBaseDir := t.TempDir()
	explicitManager, err := NewManager(Options{
		BaseDir:       explicitBaseDir,
		WorkspaceRoot: explicitBaseDir,
		Overrides: OverrideConfig{
			ImageExplicit: true,
		},
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if got := explicitManager.resolveUpdateImage(DeployConfig{Image: "eceasy/cli-proxy-api:v6.9.3"}); got != "eceasy/cli-proxy-api:v6.9.3" {
		t.Fatalf("resolveUpdateImage() with explicit image = %q", got)
	}
}

func TestUpdateWithoutExplicitImageResetsPinnedVersionToLatest(t *testing.T) {
	latestBackup := githubLatestReleaseURL
	releaseListBackup := githubReleaseListURL
	defer func() {
		githubLatestReleaseURL = latestBackup
		githubReleaseListURL = releaseListBackup
	}()

	baseDir := t.TempDir()
	lastCompose := filepath.Join(baseDir, "last-compose.yml")
	installFakeDocker(t, baseDir, lastCompose)

	server := newReleaseAndManagementServer(t)
	defer server.Close()
	githubLatestReleaseURL = server.URL + "/releases/latest"
	githubReleaseListURL = server.URL + "/releases"

	manager, err := NewManager(Options{
		BaseDir:         baseDir,
		WorkspaceRoot:   baseDir,
		UpstreamBaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	cfg.Image = "eceasy/cli-proxy-api:v6.9.3"
	cfg.ContainerName = "cpa-update-test"
	cfg.HostPort = 28431
	cfg.APIKey = "test-api-key"
	cfg.ManagementSecret = "test-management-secret"

	if err = seedExistingDeployment(cfg); err != nil {
		t.Fatalf("seedExistingDeployment failed: %v", err)
	}
	if err = manager.saveState(cfg, ReleaseInfo{CurrentVersion: "v6.9.3"}, ""); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	if err = manager.Update(context.Background(), nil, ""); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	updatedCfg, err := manager.CurrentConfig()
	if err != nil {
		t.Fatalf("CurrentConfig failed: %v", err)
	}
	if updatedCfg.Image != defaultImage {
		t.Fatalf("updated image = %q, want %q", updatedCfg.Image, defaultImage)
	}

	envData, err := os.ReadFile(updatedCfg.EnvFile)
	if err != nil {
		t.Fatalf("ReadFile env failed: %v", err)
	}
	if !strings.Contains(string(envData), "CPA_IMAGE='eceasy/cli-proxy-api:latest'") {
		t.Fatalf("env image not reset to latest:\n%s", string(envData))
	}

	composeData, err := os.ReadFile(updatedCfg.ComposeFile)
	if err != nil {
		t.Fatalf("ReadFile compose failed: %v", err)
	}
	if !strings.Contains(string(composeData), "image: eceasy/cli-proxy-api:latest") {
		t.Fatalf("compose image not reset to latest:\n%s", string(composeData))
	}

	lastComposeData, err := os.ReadFile(lastCompose)
	if err != nil {
		t.Fatalf("ReadFile last compose failed: %v", err)
	}
	if !strings.Contains(string(lastComposeData), "image: eceasy/cli-proxy-api:latest") {
		t.Fatalf("docker compose did not receive latest image config:\n%s", string(lastComposeData))
	}
}

func TestBackupHandlesGrowingOperationLog(t *testing.T) {
	baseDir := t.TempDir()
	manager, err := NewManager(Options{BaseDir: baseDir, WorkspaceRoot: baseDir})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	cfg.Image = "eceasy/cli-proxy-api:v6.9.3"
	cfg.APIKey = "test-api-key"
	cfg.ManagementSecret = "test-management-secret"
	if err = seedExistingDeployment(cfg); err != nil {
		t.Fatalf("seedExistingDeployment failed: %v", err)
	}
	if err = manager.saveState(cfg, ReleaseInfo{CurrentVersion: "v6.9.3"}, ""); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	initialLog := bytes.Repeat([]byte("0123456789abcdef\n"), 512*1024)
	if err = os.WriteFile(cfg.OperationLogFile, initialLog, 0o644); err != nil {
		t.Fatalf("WriteFile operation log failed: %v", err)
	}

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		payload := bytes.Repeat([]byte("grow-log-entry\n"), 512)
		for {
			select {
			case <-stopCh:
				return
			default:
			}

			file, openErr := os.OpenFile(cfg.OperationLogFile, os.O_APPEND|os.O_WRONLY, 0o644)
			if openErr == nil {
				_, _ = file.Write(payload)
				_ = file.Close()
			}
			time.Sleep(1 * time.Millisecond)
		}
	}()

	_, err = manager.Backup(context.Background(), nil)
	close(stopCh)
	<-doneCh
	if err != nil {
		t.Fatalf("Backup failed while operation log was growing: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(cfg.BackupsDir, "*.tar.gz"))
	if err != nil {
		t.Fatalf("Glob backups failed: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected backup archive to be created")
	}
}

func TestBackupPreservesReleaseMetadata(t *testing.T) {
	baseDir := t.TempDir()
	manager, err := NewManager(Options{BaseDir: baseDir, WorkspaceRoot: baseDir})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	cfg.Image = "eceasy/cli-proxy-api:v6.9.3"
	cfg.APIKey = "test-api-key"
	cfg.ManagementSecret = "test-management-secret"
	if err = seedExistingDeployment(cfg); err != nil {
		t.Fatalf("seedExistingDeployment failed: %v", err)
	}
	if err = manager.saveState(cfg, ReleaseInfo{
		CurrentVersion: "v6.9.3",
		LatestVersion:  "v6.9.6",
		HasUpdate:      true,
		BehindCount:    3,
		MissingVersions: []string{
			"v6.9.4",
			"v6.9.5",
			"v6.9.6",
		},
		ReleaseURL: "https://example.com/v6.9.6",
	}, ""); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	snapshot, err := manager.Backup(context.Background(), nil)
	if err != nil {
		t.Fatalf("Backup failed: %v", err)
	}

	state, err := manager.loadState()
	if err != nil {
		t.Fatalf("loadState failed: %v", err)
	}
	if state.LastBackup != snapshot.Name {
		t.Fatalf("last backup = %q, want %q", state.LastBackup, snapshot.Name)
	}
	if state.Release.LatestVersion != "v6.9.6" {
		t.Fatalf("release latest version = %q", state.Release.LatestVersion)
	}
	if !state.Release.HasUpdate {
		t.Fatal("expected cached update metadata to be preserved")
	}
	if state.Release.ReleaseURL != "https://example.com/v6.9.6" {
		t.Fatalf("release url = %q", state.Release.ReleaseURL)
	}
}

func TestUpdateUsesLocalImageWhenPullFails(t *testing.T) {
	latestBackup := githubLatestReleaseURL
	releaseListBackup := githubReleaseListURL
	defer func() {
		githubLatestReleaseURL = latestBackup
		githubReleaseListURL = releaseListBackup
	}()

	baseDir := t.TempDir()
	lastCompose := filepath.Join(baseDir, "last-compose.yml")
	installFakeDockerWithLocalImageFallback(t, baseDir, lastCompose, defaultImage)

	server := newReleaseAndManagementServer(t)
	defer server.Close()
	githubLatestReleaseURL = server.URL + "/releases/latest"
	githubReleaseListURL = server.URL + "/releases"

	manager, err := NewManager(Options{
		BaseDir:         baseDir,
		WorkspaceRoot:   baseDir,
		UpstreamBaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := defaultDeployConfig(baseDir)
	cfg.Image = "eceasy/cli-proxy-api:v6.9.3"
	cfg.ContainerName = "cpa-update-local-image"
	cfg.HostPort = 28432
	cfg.APIKey = "test-api-key"
	cfg.ManagementSecret = "test-management-secret"

	if err = seedExistingDeployment(cfg); err != nil {
		t.Fatalf("seedExistingDeployment failed: %v", err)
	}
	if err = manager.saveState(cfg, ReleaseInfo{
		CurrentVersion: "v6.9.3",
		LatestVersion:  "v6.9.6",
		HasUpdate:      true,
		BehindCount:    3,
	}, ""); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	if err = manager.Update(context.Background(), nil, ""); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	updatedCfg, err := manager.CurrentConfig()
	if err != nil {
		t.Fatalf("CurrentConfig failed: %v", err)
	}
	if updatedCfg.Image != defaultImage {
		t.Fatalf("updated image = %q, want %q", updatedCfg.Image, defaultImage)
	}

	lastComposeData, err := os.ReadFile(lastCompose)
	if err != nil {
		t.Fatalf("ReadFile last compose failed: %v", err)
	}
	if !strings.Contains(string(lastComposeData), "image: eceasy/cli-proxy-api:latest") {
		t.Fatalf("docker compose did not continue with local image config:\n%s", string(lastComposeData))
	}
}

func TestInstallRetriesPullUntilSuccess(t *testing.T) {
	latestBackup := githubLatestReleaseURL
	releaseListBackup := githubReleaseListURL
	retryDelayBackup := composePullRetryDelay
	defer func() {
		githubLatestReleaseURL = latestBackup
		githubReleaseListURL = releaseListBackup
		composePullRetryDelay = retryDelayBackup
	}()

	composePullRetryDelay = time.Millisecond

	baseDir := t.TempDir()
	pullCountFile := filepath.Join(baseDir, "pull-count.txt")
	installFakeDockerWithTransientPullFailures(t, baseDir, pullCountFile, 2)

	server := newReleaseAndManagementServer(t)
	defer server.Close()
	githubLatestReleaseURL = server.URL + "/releases/latest"
	githubReleaseListURL = server.URL + "/releases"

	manager, err := NewManager(Options{
		BaseDir:         baseDir,
		WorkspaceRoot:   baseDir,
		UpstreamBaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	currentCfg, err := manager.CurrentConfig()
	if err != nil {
		t.Fatalf("CurrentConfig failed: %v", err)
	}
	if currentCfg.RequestRetry != defaultRequestRetry {
		t.Fatalf("request retry = %d, want %d", currentCfg.RequestRetry, defaultRequestRetry)
	}

	if err = manager.Install(context.Background(), nil); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	countRaw, err := os.ReadFile(pullCountFile)
	if err != nil {
		t.Fatalf("read pull count failed: %v", err)
	}
	if strings.TrimSpace(string(countRaw)) != "3" {
		logContent, _ := os.ReadFile(filepath.Join(baseDir, "cpa-operation.log"))
		t.Fatalf("unexpected pull count: %q\nlog:\n%s", string(countRaw), string(logContent))
	}
}

func installFakeDocker(t *testing.T, baseDir, lastCompose string) {
	t.Helper()

	binDir := filepath.Join(baseDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	script := `#!/bin/sh
set -eu
if [ "$#" -ge 2 ] && [ "$1" = "compose" ] && [ "$2" = "version" ]; then
  exit 0
fi
if [ "$#" -ge 1 ] && [ "$1" = "compose" ]; then
  compose_file=""
  prev=""
  for arg in "$@"; do
    if [ "$prev" = "-f" ]; then
      compose_file="$arg"
      prev=""
      continue
    fi
    prev="$arg"
  done
  if [ -n "$compose_file" ]; then
    cp "$compose_file" "$FAKE_DOCKER_LAST_COMPOSE"
  fi
  exit 0
fi
if [ "$#" -ge 2 ] && [ "$1" = "image" ] && [ "$2" = "inspect" ]; then
  exit 1
fi
exit 0
`
	dockerPath := filepath.Join(binDir, "docker")
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile docker failed: %v", err)
	}

	t.Setenv("FAKE_DOCKER_LAST_COMPOSE", lastCompose)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func installFakeDockerWithLocalImageFallback(t *testing.T, baseDir, lastCompose, localImage string) {
	t.Helper()

	binDir := filepath.Join(baseDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	script := `#!/bin/sh
set -eu
if [ "$#" -ge 2 ] && [ "$1" = "compose" ] && [ "$2" = "version" ]; then
  exit 0
fi
if [ "$#" -ge 4 ] && [ "$1" = "compose" ]; then
  compose_file=""
  prev=""
  for arg in "$@"; do
    if [ "$prev" = "-f" ]; then
      compose_file="$arg"
      prev=""
      continue
    fi
    prev="$arg"
  done
  if [ -n "$compose_file" ]; then
    cp "$compose_file" "$FAKE_DOCKER_LAST_COMPOSE"
  fi
  last_arg=""
  for arg in "$@"; do
    last_arg="$arg"
  done
  if [ "$last_arg" = "pull" ]; then
    echo "pull failed" >&2
    exit 1
  fi
  exit 0
fi
if [ "$#" -ge 4 ] && [ "$1" = "image" ] && [ "$2" = "inspect" ]; then
  if [ "$3" = "$FAKE_DOCKER_LOCAL_IMAGE" ]; then
    echo "sha256:test"
    exit 0
  fi
  exit 1
fi
exit 0
`
	dockerPath := filepath.Join(binDir, "docker")
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile docker failed: %v", err)
	}

	t.Setenv("FAKE_DOCKER_LAST_COMPOSE", lastCompose)
	t.Setenv("FAKE_DOCKER_LOCAL_IMAGE", localImage)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func installFakeDockerWithTransientPullFailures(t *testing.T, baseDir, pullCountFile string, failCount int) {
	t.Helper()

	binDir := filepath.Join(baseDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	script := `#!/bin/sh
set -eu
if [ "$#" -ge 2 ] && [ "$1" = "compose" ] && [ "$2" = "version" ]; then
  exit 0
fi
if [ "$#" -ge 1 ] && [ "$1" = "compose" ]; then
  last_arg=""
  for arg in "$@"; do
    last_arg="$arg"
  done
  if [ "$last_arg" = "pull" ]; then
    count=0
    if [ -f "$FAKE_DOCKER_PULL_COUNT_FILE" ]; then
      count=$(cat "$FAKE_DOCKER_PULL_COUNT_FILE")
    fi
    count=$((count + 1))
    printf '%s' "$count" >"$FAKE_DOCKER_PULL_COUNT_FILE"
    if [ "$count" -le "$FAKE_DOCKER_PULL_FAIL_COUNT" ]; then
      echo "temporary pull failure" >&2
      exit 1
    fi
    exit 0
  fi
  exit 0
fi
if [ "$#" -ge 2 ] && [ "$1" = "image" ] && [ "$2" = "inspect" ]; then
  exit 1
fi
exit 0
`
	dockerPath := filepath.Join(binDir, "docker")
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile docker failed: %v", err)
	}

	t.Setenv("FAKE_DOCKER_PULL_COUNT_FILE", pullCountFile)
	t.Setenv("FAKE_DOCKER_PULL_FAIL_COUNT", fmt.Sprintf("%d", failCount))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func newReleaseAndManagementServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/config":
			w.Header().Set("X-CPA-VERSION", "v6.9.6")
			w.Header().Set("X-CPA-COMMIT", "6570692")
			w.Header().Set("X-CPA-BUILD-DATE", "2026-03-29T14:21:11Z")
			w.WriteHeader(http.StatusOK)
		case "/releases":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"tag_name":     "v6.9.6",
					"name":         "v6.9.6",
					"body":         "fix: latest",
					"html_url":     "https://example.com/v6.9.6",
					"published_at": "2026-03-29T14:26:17Z",
				},
				{
					"tag_name":     "v6.9.5",
					"name":         "v6.9.5",
					"body":         "fix: previous",
					"html_url":     "https://example.com/v6.9.5",
					"published_at": "2026-03-29T04:36:29Z",
				},
			})
		case "/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name":     "v6.9.6",
				"name":         "v6.9.6",
				"body":         "fix: latest",
				"html_url":     "https://example.com/v6.9.6",
				"published_at": "2026-03-29T14:26:17Z",
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func seedExistingDeployment(cfg DeployConfig) error {
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "auths"), 0o755); err != nil {
		return err
	}
	if err := writeInitialConfigFile(cfg); err != nil {
		return err
	}
	if err := writeEnvFileAtomic(cfg); err != nil {
		return err
	}
	if err := writeComposeFileAtomic(cfg); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cfg.DataDir, "auths", "keep.txt"), []byte("ok"), 0o644)
}
