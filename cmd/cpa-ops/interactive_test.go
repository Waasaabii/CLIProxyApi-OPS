package main

import (
	"errors"
	"testing"

	"github.com/Waasaabii/CLIProxyApi-OPS/internal/ops"
)

func TestResolveReleaseVersionSelectionByIndex(t *testing.T) {
	t.Parallel()

	releases := []ops.ReleaseSummary{
		{Version: "v6.9.6"},
		{Version: "v6.9.5"},
	}

	version, err := resolveReleaseVersionSelection("2", releases)
	if err != nil {
		t.Fatalf("resolveReleaseVersionSelection failed: %v", err)
	}
	if version != "v6.9.5" {
		t.Fatalf("version = %q, want %q", version, "v6.9.5")
	}
}

func TestResolveReleaseVersionSelectionManual(t *testing.T) {
	t.Parallel()

	_, err := resolveReleaseVersionSelection("m", []ops.ReleaseSummary{{Version: "v6.9.6"}})
	if !errors.Is(err, errVersionSelectionManual) {
		t.Fatalf("expected manual selection error, got %v", err)
	}
}

func TestResolveReleaseVersionSelectionBack(t *testing.T) {
	t.Parallel()

	_, err := resolveReleaseVersionSelection("0", []ops.ReleaseSummary{{Version: "v6.9.6"}})
	if !errors.Is(err, errVersionSelectionBack) {
		t.Fatalf("expected back selection error, got %v", err)
	}
}

func TestResolveReleaseVersionSelectionAcceptsVersionText(t *testing.T) {
	t.Parallel()

	version, err := resolveReleaseVersionSelection("v6.9.3", nil)
	if err != nil {
		t.Fatalf("resolveReleaseVersionSelection failed: %v", err)
	}
	if version != "v6.9.3" {
		t.Fatalf("version = %q, want %q", version, "v6.9.3")
	}
}
