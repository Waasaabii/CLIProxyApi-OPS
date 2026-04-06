package main

import (
	"bufio"
	"errors"
	"strings"
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

func TestIsGenerateShortcut(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"/gen", "gen", "G"} {
		if !isGenerateShortcut(value) {
			t.Fatalf("expected %q to be treated as generate shortcut", value)
		}
	}
	if isGenerateShortcut("1") {
		t.Fatal("did not expect \"1\" to be treated as generate shortcut")
	}
}

func TestPromptInteractiveSecretValueManual(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader("manual-secret\n"))
	value, changed, err := promptInteractiveSecretValue(reader, "管理密钥", "old-secret", "2", func() (string, error) {
		return "MGT-generated", nil
	})
	if err != nil {
		t.Fatalf("promptInteractiveSecretValue failed: %v", err)
	}
	if !changed {
		t.Fatal("expected changed to be true")
	}
	if value != "manual-secret" {
		t.Fatalf("value = %q, want %q", value, "manual-secret")
	}
}

func TestPromptInteractiveSecretValueCancel(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader(""))
	value, changed, err := promptInteractiveSecretValue(reader, "管理密钥", "old-secret", "0", func() (string, error) {
		return "MGT-generated", nil
	})
	if err != nil {
		t.Fatalf("promptInteractiveSecretValue failed: %v", err)
	}
	if changed {
		t.Fatal("expected changed to be false")
	}
	if value != "" {
		t.Fatalf("value = %q, want empty", value)
	}
}

func TestPromptInteractiveSecretValueGenerate(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader(""))
	value, changed, err := promptInteractiveSecretValue(reader, "管理密钥", "old-secret", "1", func() (string, error) {
		return "MGT-auto-generated", nil
	})
	if err != nil {
		t.Fatalf("promptInteractiveSecretValue failed: %v", err)
	}
	if !changed {
		t.Fatal("expected changed to be true")
	}
	if value != "MGT-auto-generated" {
		t.Fatalf("value = %q, want %q", value, "MGT-auto-generated")
	}
}

func TestPromptOptionalGeneratedValueShortcut(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader("/gen\n"))
	value, err := promptOptionalGeneratedValue(reader, "CPA API Key", "", func() (string, error) {
		return "sk-auto-generated", nil
	})
	if err != nil {
		t.Fatalf("promptOptionalGeneratedValue failed: %v", err)
	}
	if value != "sk-auto-generated" {
		t.Fatalf("value = %q, want %q", value, "sk-auto-generated")
	}
}
