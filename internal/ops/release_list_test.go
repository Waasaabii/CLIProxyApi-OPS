package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListReleasesReturnsSummariesWithLimit(t *testing.T) {
	t.Parallel()

	latestBackup := githubLatestReleaseURL
	releaseListBackup := githubReleaseListURL
	defer func() {
		githubLatestReleaseURL = latestBackup
		githubReleaseListURL = releaseListBackup
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"tag_name":     "v6.9.6",
					"name":         "v6.9.6",
					"html_url":     "https://example.com/v6.9.6",
					"published_at": "2026-03-31T00:00:00Z",
				},
				{
					"tag_name":     "v6.9.5",
					"name":         "v6.9.5",
					"html_url":     "https://example.com/v6.9.5",
					"published_at": "2026-03-30T00:00:00Z",
				},
			})
		case "/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name":     "v6.9.6",
				"name":         "v6.9.6",
				"html_url":     "https://example.com/v6.9.6",
				"published_at": "2026-03-31T00:00:00Z",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	githubLatestReleaseURL = server.URL + "/releases/latest"
	githubReleaseListURL = server.URL + "/releases"

	baseDir := t.TempDir()
	manager, err := NewManager(Options{BaseDir: baseDir, WorkspaceRoot: baseDir})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	releases, err := manager.ListReleases(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListReleases failed: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("len(releases) = %d, want 1", len(releases))
	}
	if releases[0].Version != "v6.9.6" {
		t.Fatalf("version = %q, want %q", releases[0].Version, "v6.9.6")
	}
}
