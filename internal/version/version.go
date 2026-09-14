package version

import (
	_ "embed"
	"runtime"
	"strings"
)

//go:embed VERSION
var rawVersion string

// Version is the release semver. Edit VERSION to bump it.
var Version = strings.TrimSpace(rawVersion)

// Commit and Date are optional; release builds set them via -ldflags.
var (
	Commit = ""
	Date   = ""
)

func Short() string {
	if Version == "" {
		return "dev"
	}
	return Version
}

func String() string {
	s := "Hello Coder " + Short()
	if c := shortCommit(); c != "" {
		s += " (" + c
		if Date != "" {
			s += " " + Date
		}
		s += ")"
	}
	return s
}

func Info() map[string]any {
	m := map[string]any{
		"version": Short(),
		"go":      runtime.Version(),
	}
	if c := shortCommit(); c != "" {
		m["commit"] = c
	}
	if Date != "" {
		m["date"] = Date
	}
	return m
}

func shortCommit() string {
	c := strings.TrimSpace(Commit)
	if c == "" || c == "unknown" {
		return ""
	}
	if len(c) > 7 {
		return c[:7]
	}
	return c
}
