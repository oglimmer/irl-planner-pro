// Package buildinfo holds build-time identification populated via -ldflags.
package buildinfo

// Populated at build time via:
//
//	-ldflags "-X irlplanner/internal/buildinfo.Version=...
//	          -X irlplanner/internal/buildinfo.Commit=...
//	          -X irlplanner/internal/buildinfo.Time=..."
var (
	Version = "dev"
	Commit  = "unknown"
	Time    = "unknown"
)
