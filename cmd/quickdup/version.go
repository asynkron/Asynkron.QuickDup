package main

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

const developmentVersion = "dev"

// version is overridden by GoReleaser through -ldflags "-X main.version=<tag>".
var version = developmentVersion

var readBuildInfo = debug.ReadBuildInfo

func writeVersion(w io.Writer) {
	_, _ = fmt.Fprintf(w, "quickdup %s\n", resolveVersion())
}

func resolveVersion() string {
	if explicit := strings.TrimSpace(version); explicit != "" && explicit != developmentVersion {
		return explicit
	}

	info, ok := readBuildInfo()
	if !ok || info == nil {
		return developmentVersion
	}
	if moduleVersion := strings.TrimSpace(info.Main.Version); moduleVersion != "" && moduleVersion != "(devel)" {
		return moduleVersion
	}
	if revision := strings.TrimSpace(buildInfoSetting(info, "vcs.revision")); revision != "" {
		return developmentVersion + "+" + shortRevision(revision)
	}

	return developmentVersion
}

func buildInfoSetting(info *debug.BuildInfo, key string) string {
	if info == nil {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}

func shortRevision(revision string) string {
	const length = 12
	if len(revision) <= length {
		return revision
	}
	return revision[:length]
}
