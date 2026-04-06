package main

import "strings"

var (
	toolVersion   = "dev"
	toolCommit    = ""
	toolBuildDate = ""
)

func currentToolVersion() string {
	version := strings.TrimSpace(toolVersion)
	if version == "" {
		version = "dev"
	}
	return version
}
