package server

import (
	"strings"
	"testing"

	"github.com/Waasaabii/CLIProxyApi-OPS/internal/ops"
)

func TestInjectManagementScript(t *testing.T) {
	t.Parallel()

	body := []byte("<html><body><div>ok</div></BODY></html>")
	injected, changed := injectManagementScript(body)
	if !changed {
		t.Fatal("expected html injection")
	}
	content := string(injected)
	if !strings.Contains(content, `<script src="/ops/management.js"></script>`) {
		t.Fatalf("missing injected script: %s", content)
	}
	if strings.Count(content, "/ops/management.js") != 1 {
		t.Fatalf("script injected multiple times: %s", content)
	}
}

func TestLegacyLatestVersionPayload(t *testing.T) {
	t.Parallel()

	payload := defaultVersionPayloadFactory{}.Legacy().Build(ops.ReleaseInfo{
		CurrentVersion: "v6.9.3",
		LatestVersion:  "v6.9.6",
		HasUpdate:      true,
		BehindCount:    1,
		ReleaseURL:     "https://example.com/releases/v6.9.6",
		ReleaseTitle:   "v6.9.6",
		PublishedAt:    "2026-03-29T14:21:11Z",
	}).(map[string]any)

	if got := payload["latest-version"]; got != "v6.9.6" {
		t.Fatalf("unexpected latest-version: %#v", got)
	}
	if got := payload["latest_version"]; got != "v6.9.6" {
		t.Fatalf("unexpected latest_version: %#v", got)
	}
	if got := payload["latest"]; got != "v6.9.6" {
		t.Fatalf("unexpected latest: %#v", got)
	}
	if got := payload["current-version"]; got != "v6.9.3" {
		t.Fatalf("unexpected current-version: %#v", got)
	}
	if got := payload["has-update"]; got != true {
		t.Fatalf("unexpected has-update: %#v", got)
	}
	if got := payload["behind-count"]; got != 1 {
		t.Fatalf("unexpected behind-count: %#v", got)
	}
	if got := payload["release-url"]; got != "https://example.com/releases/v6.9.6" {
		t.Fatalf("unexpected release-url: %#v", got)
	}
}
