package ops

import (
	"context"
	"testing"
)

type stubReleaseProviderFactory struct {
	provider ReleaseProvider
}

type stubDeployExecutorFactory struct {
	executor DeployExecutor
}

type stubReleaseProvider struct{}

type stubDeployExecutor struct{}

func (f stubReleaseProviderFactory) Create(options Options) ReleaseProvider {
	return f.provider
}

func (f stubDeployExecutorFactory) Create(manager *Manager, options Options) DeployExecutor {
	return f.executor
}

func (stubReleaseProvider) List(ctx context.Context, currentVersion string) ([]githubRelease, error) {
	return nil, nil
}

func (stubReleaseProvider) Latest(ctx context.Context) (githubRelease, error) {
	return githubRelease{}, nil
}

func (stubDeployExecutor) Pull(ctx context.Context, cfg DeployConfig, logger Logger) error {
	return nil
}

func (stubDeployExecutor) Up(ctx context.Context, cfg DeployConfig, logger Logger) error {
	return nil
}

func (stubDeployExecutor) Down(ctx context.Context, cfg DeployConfig, logger Logger) error {
	return nil
}

func (stubDeployExecutor) RemoveContainer(ctx context.Context, cfg DeployConfig, logger Logger, containerName string) error {
	return nil
}

func (stubDeployExecutor) InspectContainer(ctx context.Context, cfg DeployConfig, containerName string) (string, error) {
	return "", nil
}

func (stubDeployExecutor) HasLocalImage(ctx context.Context, cfg DeployConfig, image string) bool {
	return false
}

func TestNewManagerUsesInjectedFactories(t *testing.T) {
	t.Parallel()

	releaseProvider := &stubReleaseProvider{}
	deployExecutor := &stubDeployExecutor{}
	baseDir := t.TempDir()

	manager, err := newManagerWithDependencies(Options{
		BaseDir:       baseDir,
		WorkspaceRoot: baseDir,
	}, ManagerDependencies{
		ReleaseProviderFactory: stubReleaseProviderFactory{provider: releaseProvider},
		DeployExecutorFactory:  stubDeployExecutorFactory{executor: deployExecutor},
	})
	if err != nil {
		t.Fatalf("newManagerWithDependencies failed: %v", err)
	}

	if manager.releaseProvider != releaseProvider {
		t.Fatal("release provider factory was not used")
	}
	if manager.deployExecutor != deployExecutor {
		t.Fatal("deploy executor factory was not used")
	}
}
