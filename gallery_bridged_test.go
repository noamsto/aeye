package main

import "testing"

// TestBridgedPolicy pins the formula runGallery and the tick handler both use
// to decide the frame policy: AEYE_BRIDGED always forces it on, otherwise it
// follows relay detection.
func TestBridgedPolicy(t *testing.T) {
	tests := []struct {
		name     string
		envForce bool
		relayed  bool
		want     bool
	}{
		{"neither set", false, false, false},
		{"relayed only", false, true, true},
		{"env force only", true, false, true},
		{"both set", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envForce {
				t.Setenv("AEYE_BRIDGED", "1")
			} else {
				t.Setenv("AEYE_BRIDGED", "")
			}
			if got := bridgedPolicy(tt.relayed); got != tt.want {
				t.Fatalf("bridgedPolicy(%v) = %v, want %v", tt.relayed, got, tt.want)
			}
		})
	}
}

// TestTickFollowsRelayFlip is the #257 regression: a viewer opened locally and
// later watched through a lazytmux mirror (or the reverse) must not keep the
// frame policy it started with — the periodic tick is where this gets
// noticed, since re-running relayTermName per pan frame would put a tmux
// subprocess on the hot path.
func TestTickFollowsRelayFlip(t *testing.T) {
	t.Setenv("AEYE_BRIDGED", "")
	m, _ := newVisibilityModel(t)
	m.visible = true
	stubPaneVisible(t, true)

	stubRelayTermName(t, "", true)
	next, _ := m.Update(galleryTickMsg{})
	m = next.(galleryModel)
	if !m.bridged {
		t.Fatal("tick did not turn m.bridged on when relay detection flipped true")
	}

	stubRelayTermName(t, "", false)
	next, _ = m.Update(galleryTickMsg{})
	m = next.(galleryModel)
	if m.bridged {
		t.Fatal("tick did not turn m.bridged off when relay detection flipped false")
	}
}

// TestTickHonoursBridgedForceOn: AEYE_BRIDGED must win even while relay
// detection reports false, so a manually-launched bridge viewer that sets it
// explicitly is never downgraded by the tick.
func TestTickHonoursBridgedForceOn(t *testing.T) {
	t.Setenv("AEYE_BRIDGED", "1")
	m, _ := newVisibilityModel(t)
	m.visible = true
	stubPaneVisible(t, true)
	stubRelayTermName(t, "", false)

	next, _ := m.Update(galleryTickMsg{})
	m = next.(galleryModel)
	if !m.bridged {
		t.Fatal("tick dropped the AEYE_BRIDGED force-on when relay detection reported false")
	}
}

// stubRelayTermName replaces relayTermName for the duration of the test, so
// the relay edge can be driven without a tmux server.
func stubRelayTermName(t *testing.T, term string, relayed bool) {
	t.Helper()
	prev := relayTermName
	relayTermName = func() (string, bool) { return term, relayed }
	t.Cleanup(func() { relayTermName = prev })
}
