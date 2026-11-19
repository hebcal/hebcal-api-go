package config

import "runtime/debug"

// APIVersion identifies the version in service in API responses.
var APIVersion = func() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" {
			return info.Main.Version
		}
	}
	return "unknown"
}()
