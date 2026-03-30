package ops

type ReleaseProviderFactory interface {
	Create(options Options) ReleaseProvider
}

type DeployExecutorFactory interface {
	Create(manager *Manager, options Options) DeployExecutor
}

type ManagerDependencies struct {
	ReleaseProviderFactory ReleaseProviderFactory
	DeployExecutorFactory  DeployExecutorFactory
}

func defaultManagerDependencies() ManagerDependencies {
	return ManagerDependencies{
		ReleaseProviderFactory: githubReleaseProviderFactory{},
		DeployExecutorFactory:  composeDeployExecutorFactory{},
	}
}
