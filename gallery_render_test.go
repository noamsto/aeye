package main

import (
	"sort"
	"testing"
)

func TestFilesToDeletePlain(t *testing.T) {
	e := imageEntry{Path: "/shots/login.png", Source: "Read"}
	got := e.filesToDelete()
	if len(got) != 1 || got[0] != "/shots/login.png" {
		t.Fatalf("filesToDelete() = %v, want [/shots/login.png]", got)
	}
}

func TestFilesToDeleteD2Cluster(t *testing.T) {
	e := imageEntry{
		Path:   "/d/hash-dark.png",
		Vector: "/d/hash-dark.svg",
		Source: "d2",
	}
	got := e.filesToDelete()
	sort.Strings(got)
	want := []string{
		"/d/hash-dark.png", "/d/hash-dark.svg",
		"/d/hash-light.png", "/d/hash-light.svg",
	}
	if len(got) != len(want) {
		t.Fatalf("filesToDelete() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filesToDelete() = %v, want %v", got, want)
		}
	}
}

func TestFilesToDeleteD2NoVector(t *testing.T) {
	e := imageEntry{Path: "/d/hash-dark.png", Source: "d2"}
	got := e.filesToDelete()
	if len(got) != 2 {
		t.Fatalf("filesToDelete() = %v, want the two png variants only", got)
	}
}
