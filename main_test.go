package main

import (
	"encoding/json"
	"testing"
)

func TestVersionFromManifest(t *testing.T) {
	var m map[string]string
	if err := json.Unmarshal(releaseManifest, &m); err != nil {
		t.Fatalf("embedded release-please manifest must be valid JSON: %v", err)
	}
	want := m["."]
	if want == "" {
		t.Fatal("manifest must carry a root (\".\") version")
	}

	old := buildSuffix
	t.Cleanup(func() { buildSuffix = old })

	buildSuffix = ""
	if got := version(); got != want {
		t.Errorf("version() = %q, want %q", got, want)
	}
	buildSuffix = "deadbee"
	if got := version(); got != want+"-deadbee" {
		t.Errorf("version() with suffix = %q, want %q", got, want+"-deadbee")
	}
}
