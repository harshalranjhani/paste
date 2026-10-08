package pbin

import (
	"runtime/debug"
	"strconv"
	"strings"
)

func buildVersion(version string) string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func newerRelease(latest, current string) bool {
	current, _, _ = strings.Cut(current, "+")
	current, _, prerelease := strings.Cut(current, "-")
	installed := strings.Split(strings.TrimPrefix(current, "v"), ".")
	available := strings.Split(strings.TrimPrefix(latest, "v"), ".")
	if len(installed) != 3 {
		return true // Development builds have no release version to compare.
	}
	for i := range available {
		a, err := strconv.ParseUint(available[i], 10, 64)
		if err != nil {
			return true
		}
		b, err := strconv.ParseUint(installed[i], 10, 64)
		if err != nil {
			return true
		}
		if a != b {
			return a > b
		}
	}
	return prerelease
}
