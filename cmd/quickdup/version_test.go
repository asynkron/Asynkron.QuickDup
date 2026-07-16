package main

import (
	"bytes"
	"runtime/debug"
	"testing"
)

func TestResolveVersionPrefersInjectedVersion(t *testing.T) {
	withVersionState(t)
	version = "v1.2.3"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		t.Fatal("resolveVersion read build info for an injected version")
		return nil, false
	}

	if got := resolveVersion(); got != "v1.2.3" {
		t.Fatalf("resolveVersion() = %q, want %q", got, "v1.2.3")
	}
}

func TestResolveVersionUsesBuildMetadataFallbacks(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		info     *debug.BuildInfo
		infoOK   bool
		expected string
	}{
		{
			name:    "module version",
			version: developmentVersion,
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "v0.8.0"},
			},
			infoOK:   true,
			expected: "v0.8.0",
		},
		{
			name:    "vcs revision",
			version: developmentVersion,
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abcdef1234567890"},
				},
			},
			infoOK:   true,
			expected: "dev+abcdef123456",
		},
		{
			name:     "missing build info",
			version:  "",
			infoOK:   false,
			expected: developmentVersion,
		},
		{
			name:     "development build",
			version:  developmentVersion,
			info:     &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			infoOK:   true,
			expected: developmentVersion,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withVersionState(t)
			version = test.version
			readBuildInfo = func() (*debug.BuildInfo, bool) {
				return test.info, test.infoOK
			}

			if got := resolveVersion(); got != test.expected {
				t.Fatalf("resolveVersion() = %q, want %q", got, test.expected)
			}
		})
	}
}

func TestWriteVersionPrintsSingleLine(t *testing.T) {
	withVersionState(t)
	version = "v2.0.0"
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	var output bytes.Buffer
	writeVersion(&output)
	if got := output.String(); got != "quickdup v2.0.0\n" {
		t.Fatalf("writeVersion() output = %q, want %q", got, "quickdup v2.0.0\\n")
	}
}

func withVersionState(t *testing.T) {
	t.Helper()
	previousVersion := version
	previousReadBuildInfo := readBuildInfo
	t.Cleanup(func() {
		version = previousVersion
		readBuildInfo = previousReadBuildInfo
	})
}
