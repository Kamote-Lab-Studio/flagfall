// Package version holds build information, set at link time:
//
//	go build -ldflags "-X github.com/flagfall/flagfall/internal/version.Version=1.0.0 -X github.com/flagfall/flagfall/internal/version.Commit=abc123"
package version

var (
	// Version is the semantic version of this build.
	Version = "0.0.0-dev"
	// Commit is the git commit this build was made from.
	Commit = "unknown"
)

// String returns a human-readable version line.
func String() string {
	return "flagfall " + Version + " (" + Commit + ")"
}
