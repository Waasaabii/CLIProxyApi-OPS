package ops

import "time"

const (
	defaultContainerPort         = 8317
	defaultImage                 = "eceasy/cli-proxy-api:latest"
	defaultContainerName         = "cpa"
	defaultBindHost              = "127.0.0.1"
	defaultHostPort              = 8317
	defaultRequestRetry          = 3
	defaultGitHubRequestRetries  = 3
	defaultAuthDir               = "/data/auths"
	defaultGitHubLatestRelease   = "https://api.github.com/repos/router-for-me/CLIProxyAPI/releases/latest"
	defaultGitHubReleaseList     = "https://api.github.com/repos/router-for-me/CLIProxyAPI/releases"
	defaultGitHubReleasePageBase = "https://github.com/router-for-me/CLIProxyAPI/releases"
	authStorageKey               = "cli-proxy-auth"
)

type Options struct {
	BaseDir         string
	WorkspaceRoot   string
	UpstreamBaseURL string
	Overrides       OverrideConfig
}

type OverrideConfig struct {
	Image                  string
	ImageExplicit          bool
	ContainerName          string
	BindHost               string
	HostPort               int
	APIKey                 string
	ManagementSecret       string
	AllowRemoteManagement  *bool
	DisableControlPanel    *bool
	Debug                  *bool
	UsageStatisticsEnabled *bool
	RequestRetry           int
}

type DeployConfig struct {
	BaseDir                 string `json:"baseDir"`
	DataDir                 string `json:"dataDir"`
	ComposeFile             string `json:"composeFile"`
	ConfigFile              string `json:"configFile"`
	EnvFile                 string `json:"envFile"`
	StateFile               string `json:"stateFile"`
	BackupsDir              string `json:"backupsDir"`
	OperationLogFile        string `json:"operationLogFile"`
	Image                   string `json:"image"`
	ContainerName           string `json:"containerName"`
	BindHost                string `json:"bindHost"`
	HostPort                int    `json:"hostPort"`
	ContainerPort           int    `json:"containerPort"`
	APIKey                  string `json:"apiKey"`
	ManagementSecret        string `json:"managementSecret"`
	ManagementSecretHashed  bool   `json:"managementSecretHashed"`
	AllowRemoteManagement   bool   `json:"allowRemoteManagement"`
	DisableControlPanel     bool   `json:"disableControlPanel"`
	Debug                   bool   `json:"debug"`
	UsageStatisticsEnabled  bool   `json:"usageStatisticsEnabled"`
	RequestRetry            int    `json:"requestRetry"`
	AuthDir                 string `json:"authDir"`
	UpstreamBaseURLOverride string `json:"upstreamBaseURLOverride"`
}

type Snapshot struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"createdAt"`
}

type UninstallOptions struct {
	DryRun       bool `json:"dryRun"`
	PurgeData    bool `json:"purgeData"`
	PurgeBackups bool `json:"purgeBackups"`
}

type UninstallResult struct {
	Removed []string `json:"removed"`
	Kept    []string `json:"kept"`
	DryRun  bool     `json:"dryRun"`
}

type Status struct {
	ContainerName string `json:"containerName"`
	State         string `json:"state"`
	Image         string `json:"image"`
	Ports         string `json:"ports"`
}

type ReleaseInfo struct {
	CurrentVersion            string   `json:"currentVersion,omitempty"`
	LatestVersion             string   `json:"latestVersion,omitempty"`
	HasUpdate                 bool     `json:"hasUpdate"`
	BehindCount               int      `json:"behindCount,omitempty"`
	MissingVersions           []string `json:"missingVersions,omitempty"`
	ReleaseTitle              string   `json:"releaseTitle,omitempty"`
	ReleaseNotes              string   `json:"releaseNotes,omitempty"`
	OriginalReleaseNotes      string   `json:"originalReleaseNotes,omitempty"`
	ReleaseNotesLocale        string   `json:"releaseNotesLocale,omitempty"`
	ReleaseNotesModel         string   `json:"releaseNotesModel,omitempty"`
	UpdateRecommendationLevel string   `json:"updateRecommendationLevel,omitempty"`
	UpdateRecommendation      string   `json:"updateRecommendation,omitempty"`
	UpdateSummary             string   `json:"updateSummary,omitempty"`
	ReleaseURL                string   `json:"releaseUrl,omitempty"`
	PublishedAt               string   `json:"publishedAt,omitempty"`
}

type Info struct {
	Config     DeployConfig `json:"config"`
	Status     Status       `json:"status"`
	Version    ReleaseInfo  `json:"version"`
	LastBackup string       `json:"lastBackup"`
}

type RuntimeState struct {
	Config           DeployConfig `json:"config"`
	Release          ReleaseInfo  `json:"release,omitempty"`
	CurrentVersion   string       `json:"currentVersion,omitempty"`
	CurrentCommit    string       `json:"currentCommit,omitempty"`
	CurrentBuildDate string       `json:"currentBuildDate,omitempty"`
	LastBackup       string       `json:"lastBackup,omitempty"`
	UpdatedAt        time.Time    `json:"updatedAt"`
}

type ConsoleLogger struct{}

type Logger interface {
	Printf(format string, args ...any)
}
