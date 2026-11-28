// Package build holds what the build stamps into every binary.
package build

// Version is the version of the app, from the git tag: set by the Makefile
// and windows\build.ps1, "dev" in a plain go build.
var Version = "dev"

// Channel is "release" in what the release workflow builds from a tag: such
// a copy looks for the next release and updates itself. Empty in any other
// build, which has no release to follow.
var Channel = ""

// Released reports whether this copy came from the release workflow.
func Released() bool { return Channel == "release" }
