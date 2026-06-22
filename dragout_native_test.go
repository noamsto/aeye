package main

import (
	"encoding/base64"
	"image"
	"strings"
	"testing"
)

func TestParseDragDA(t *testing.T) {
	for _, c := range []struct {
		name string
		resp string
		want bool
	}{
		{"osc72 before DA1", "\x1b]72;t=q\x1b\\\x1b[?62;4c", true},
		{"DA1 only", "\x1b[?62;4c", false},
		{"DA1 before osc72", "\x1b[?62c\x1b]72;t=q\x1b\\", false},
		{"garbage", "hello", false},
		{"empty", "", false},
	} {
		if got := parseDragDA(c.resp); got != c.want {
			t.Errorf("%s: parseDragDA(%q) = %v, want %v", c.name, c.resp, got, c.want)
		}
	}
}

func TestFileURI(t *testing.T) {
	if got, want := fileURI("/tmp/a b.png"), "file:///tmp/a%20b.png"; got != want {
		t.Errorf("fileURI = %q, want %q", got, want)
	}
}

func TestDragSequences(t *testing.T) {
	if dragArmSeq() != "\x1b]72;t=o:x=1\x1b\\" {
		t.Errorf("dragArmSeq = %q", dragArmSeq())
	}
	if dragOfferSeq() != "\x1b]72;t=o:o=3;text/uri-list\x1b\\" {
		t.Errorf("dragOfferSeq = %q", dragOfferSeq())
	}
	if dragInitiateSeq() != "\x1b]72;t=P:x=-1\x1b\\" {
		t.Errorf("dragInitiateSeq = %q", dragInitiateSeq())
	}
	b64 := base64.StdEncoding.EncodeToString([]byte("file:///x/a.png\r\n"))
	if want := "\x1b]72;t=p:x=0:m=0;" + b64 + "\x1b\\"; dragDataSeq("file:///x/a.png") != want {
		t.Errorf("dragDataSeq = %q, want %q", dragDataSeq("file:///x/a.png"), want)
	}
}

func TestDragIconFrames(t *testing.T) {
	frames := dragIconFrames(image.NewRGBA(image.Rect(0, 0, 10, 6)))
	if len(frames) == 0 {
		t.Fatal("expected at least one icon frame")
	}
	if !strings.Contains(frames[0], "t=p:x=-1:y=100:X=10:Y=6:m=0") {
		t.Errorf("first frame missing PNG/size metadata: %q", frames[0])
	}
	last := frames[len(frames)-1]
	if !strings.HasPrefix(frames[0], "\x1b]72;") || !strings.HasSuffix(last, "\x1b\\") {
		t.Errorf("frame framing wrong: %q … %q", frames[0], last)
	}
	if dragIconFrames(nil) != nil {
		t.Error("nil image should yield nil frames")
	}
}

func TestDragEventClassification(t *testing.T) {
	gesture := "72;t=o:x=5:y=3:X=10:Y=20"
	if !isDragGesture(gesture) {
		t.Errorf("isDragGesture(%q) = false, want true", gesture)
	}
	// Our own outbound frames must not be misread as a gesture.
	for _, ours := range []string{"72;t=o:x=1", "72;t=o:o=3;text/uri-list"} {
		if isDragGesture(ours) {
			t.Errorf("isDragGesture(%q) = true, want false (outbound frame)", ours)
		}
	}
	if !isDragFinished("72;t=e:x=4:y=0") {
		t.Error("t=e:x=4 should be finished")
	}
	if isDragFinished("72;t=e:x=1:y=0") {
		t.Error("t=e:x=1 (accepted) is not finished")
	}
	if !dragWasCancelled("72;t=e:x=4:y=1") {
		t.Error("t=e:x=4:y=1 should be cancelled")
	}
	if dragWasCancelled("72;t=e:x=4:y=0") {
		t.Error("t=e:x=4:y=0 is a success, not cancelled")
	}
	if !isDragError("72;t=E;ENOSPC") {
		t.Error("t=E without OK is an error")
	}
	if isDragError("72;t=E;OK") {
		t.Error("t=E;OK is the initiate ack, not an error")
	}
}

func TestOscInt(t *testing.T) {
	p := "72;t=o:x=51:y=25:X=519:Y=500"
	for _, c := range []struct {
		key  string
		want int
	}{{"x=", 51}, {"y=", 25}, {"X=", 519}, {"Y=", 500}} {
		if v, ok := oscInt(p, c.key); !ok || v != c.want {
			t.Errorf("oscInt(%q) = %d,%v want %d", c.key, v, ok, c.want)
		}
	}
	if _, ok := oscInt(p, "z="); ok {
		t.Error("absent key should return ok=false")
	}
}
