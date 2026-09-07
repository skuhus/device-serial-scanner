package version

import (
	"strings"
	"testing"
)

// agent_version is how the fleet is asked which stations are on an old build,
// so a development binary must not report an empty version.
func TestSetIgnoresEmptyValues(t *testing.T) {
	t.Cleanup(func() { commit, date = "none", "unknown" })

	Set("", "")
	if Commit() != "none" || Date() != "unknown" {
		t.Errorf("empty values overwrote the placeholders: %s", String())
	}

	Set("abc123", "2026-09-06T00:00:00Z")
	if Commit() != "abc123" || Date() != "2026-09-06T00:00:00Z" {
		t.Errorf("Set did not take effect: %s", String())
	}

	Set("", "2026-09-07T00:00:00Z")
	if Commit() != "abc123" {
		t.Errorf("an empty commit overwrote %q", Commit())
	}
}

// The version comes from source, not from the linker, so no build can report a
// version the code does not carry.
func TestVersionComesFromTheConstant(t *testing.T) {
	if Version() != version {
		t.Errorf("Version() = %q, want the constant %q", Version(), version)
	}
	if version == "" {
		t.Fatal("the version constant is empty; it is the one place it is written down")
	}
	if !strings.Contains(String(), version) {
		t.Errorf("String() = %q, want it to carry the version", String())
	}
}
