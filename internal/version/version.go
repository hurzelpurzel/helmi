// Package version holds the helmi version string.
// The value is overridden at build time via:
//
//	-ldflags "-X helmi/internal/version.Version=<version>"
package version

// Version is the helmi release version.
var Version = "2.0.0"
