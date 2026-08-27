// Package buildinfo carries build-time facts, set from main at startup.
package buildinfo

// Version is the release version ("dev" for local builds). main sets it from
// its ldflag-injected value before the CLI runs.
var Version = "dev"

// IsDev reports whether this is a local (non-release) build. Endpoint overrides
// like RC_BASE_URL are honored only in dev, so a shipped binary always talks to
// the production RevenueCat endpoints.
func IsDev() bool { return Version == "dev" }
