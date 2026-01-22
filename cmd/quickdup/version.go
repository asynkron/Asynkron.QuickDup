package main

import "runtime/debug"

var version = "dev"

var readBuildInfo = debug.ReadBuildInfo

func resolveVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	if info, ok := readBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
		if revision := buildInfoSetting(info, "vcs.revision"); revision != "" {
			return "dev+" + shortRevision(revision)
		}
	}
	return version
}

func buildInfoSetting(info *debug.BuildInfo, key string) string {
	for _, setting := range info.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}

func shortRevision(revision string) string {
	if len(revision) <= 12 {
		return revision
	}
	return revision[:12]
}
