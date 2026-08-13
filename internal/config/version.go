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

// PDFProducer is the "Encoding software" (Info.Producer) string stamped into
// every rendered PDF calendar: the application name and its module version from
// the build info, so a calendar records which build produced it.
var PDFProducer = func() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return "hebcal-api-go " + info.Main.Version
	}
	return "hebcal-api-go"
}()

// LibraryVersions is baked into every ETag so tags change when the
// application or the hebcal libraries are upgraded.
var LibraryVersions = func() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.Main.Path + "@" + info.Main.Version
	}
	return ""
}()
