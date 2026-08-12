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

// LibraryVersions is baked into every ETag so tags change when the
// application or the hebcal libraries are upgraded.
var LibraryVersions = func() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Path + "@" + info.Main.Version
	}
	return ""
}()
