package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersionPrefersExplicitVersion(t *testing.T) {
	originalVersion := version
	originalReadBuildInfo := readBuildInfo
	t.Cleanup(func() {
		version = originalVersion
		readBuildInfo = originalReadBuildInfo
	})

	version = "1.2.3"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "v9.9.9"},
		}, true
	}

	if got := resolveVersion(); got != "1.2.3" {
		t.Fatalf("expected version to prefer explicit value, got %q", got)
	}
}

func TestResolveVersionUsesBuildInfo(t *testing.T) {
	originalVersion := version
	originalReadBuildInfo := readBuildInfo
	t.Cleanup(func() {
		version = originalVersion
		readBuildInfo = originalReadBuildInfo
	})

	version = "dev"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "v0.8.0"},
		}, true
	}

	if got := resolveVersion(); got != "v0.8.0" {
		t.Fatalf("expected version from build info, got %q", got)
	}
}

func TestResolveVersionFallsBackToRevision(t *testing.T) {
	originalVersion := version
	originalReadBuildInfo := readBuildInfo
	t.Cleanup(func() {
		version = originalVersion
		readBuildInfo = originalReadBuildInfo
	})

	version = "dev"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "(devel)"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abcdef1234567890"},
			},
		}, true
	}

	if got := resolveVersion(); got != "dev+abcdef123456" {
		t.Fatalf("expected version from vcs revision, got %q", got)
	}
}
