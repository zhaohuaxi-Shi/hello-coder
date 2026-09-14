package version

import (
	"regexp"
	"testing"
)

func TestVersionSemver(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Fatalf("VERSION must be semver X.Y.Z, got %q", Version)
	}
}
