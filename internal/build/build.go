// Package build holds what the build stamps into every binary.
package build

// Version is the version of the app, from the git tag: set by the Makefile
// and windows\build.ps1, "dev" in a plain go build.
var Version = "dev"
