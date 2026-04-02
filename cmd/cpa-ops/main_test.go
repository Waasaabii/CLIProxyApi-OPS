package main

import (
	"strings"
	"testing"
)

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
