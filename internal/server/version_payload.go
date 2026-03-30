package server

import "github.com/Waasaabii/CLIProxyApi-OPS/internal/ops"

type versionPayloadAdapter interface {
	Build(version ops.ReleaseInfo) any
}

type versionPayloadFactory interface {
	Standard() versionPayloadAdapter
	Legacy() versionPayloadAdapter
}

type defaultVersionPayloadFactory struct{}

type standardVersionPayloadAdapter struct{}

type legacyVersionPayloadAdapter struct{}

func (defaultVersionPayloadFactory) Standard() versionPayloadAdapter {
	return standardVersionPayloadAdapter{}
}

func (defaultVersionPayloadFactory) Legacy() versionPayloadAdapter {
	return legacyVersionPayloadAdapter{}
}

func (standardVersionPayloadAdapter) Build(version ops.ReleaseInfo) any {
	return version
}

func (legacyVersionPayloadAdapter) Build(version ops.ReleaseInfo) any {
	return map[string]any{
		"current-version": version.CurrentVersion,
		"current_version": version.CurrentVersion,
		"latest-version":  version.LatestVersion,
		"latest_version":  version.LatestVersion,
		"latest":          version.LatestVersion,
		"has-update":      version.HasUpdate,
		"has_update":      version.HasUpdate,
		"behind-count":    version.BehindCount,
		"behind_count":    version.BehindCount,
		"release-url":     version.ReleaseURL,
		"release_url":     version.ReleaseURL,
		"release-title":   version.ReleaseTitle,
		"release_title":   version.ReleaseTitle,
		"published-at":    version.PublishedAt,
		"published_at":    version.PublishedAt,
	}
}
