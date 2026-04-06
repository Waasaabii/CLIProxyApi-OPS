package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestCurrentToolVersionFallsBackToDev(t *testing.T) {
	t.Parallel()

	originalVersion := toolVersion
	toolVersion = ""
	t.Cleanup(func() {
		toolVersion = originalVersion
	})

	if got := currentToolVersion(); got != "dev" {
		t.Fatalf("currentToolVersion = %q, want %q", got, "dev")
	}
}

func TestBuildManagerRejectsInvalidBoolFlag(t *testing.T) {
	t.Parallel()

	_, _, err := buildManager([]string{"--debug", "maybe"})
	if err == nil {
		t.Fatal("expected invalid bool flag to be rejected")
	}
	if !strings.Contains(err.Error(), "--debug") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildManagerRejectsInvalidHostPort(t *testing.T) {
	t.Parallel()

	_, _, err := buildManager([]string{"--host-port", "70000"})
	if err == nil {
		t.Fatal("expected invalid host port to be rejected")
	}
	if !strings.Contains(err.Error(), "--host-port") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildManagerRejectsInvalidRequestRetry(t *testing.T) {
	t.Parallel()

	_, _, err := buildManager([]string{"--request-retry", "-2"})
	if err == nil {
		t.Fatal("expected invalid request retry to be rejected")
	}
	if !strings.Contains(err.Error(), "--request-retry") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunGenerateSecretOutputsManagementSecret(t *testing.T) {
	output, err := captureStdout(func() error {
		return run([]string{"generate-secret"})
	})
	if err != nil {
		t.Fatalf("run generate-secret failed: %v", err)
	}
	value := strings.TrimSpace(output)
	if !strings.HasPrefix(value, "MGT-") {
		t.Fatalf("management secret = %q, want prefix %q", value, "MGT-")
	}
}

func TestRunGenerateSecretOutputsAPIKey(t *testing.T) {
	output, err := captureStdout(func() error {
		return run([]string{"generate-secret", "--kind", "api"})
	})
	if err != nil {
		t.Fatalf("run generate-secret --kind api failed: %v", err)
	}
	value := strings.TrimSpace(output)
	if !strings.HasPrefix(value, "sk-") {
		t.Fatalf("api key = %q, want prefix %q", value, "sk-")
	}
}

func TestRunGenerateSecretRejectsInvalidKind(t *testing.T) {
	err := run([]string{"generate-secret", "--kind", "invalid"})
	if err == nil {
		t.Fatal("expected invalid kind to be rejected")
	}
	if !strings.Contains(err.Error(), "--kind") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func captureStdout(fn func() error) (string, error) {
	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = originalStdout
	}()

	runErr := fn()
	_ = writer.Close()

	var buffer bytes.Buffer
	if _, err = io.Copy(&buffer, reader); err != nil {
		return "", err
	}
	_ = reader.Close()
	return buffer.String(), runErr
}
