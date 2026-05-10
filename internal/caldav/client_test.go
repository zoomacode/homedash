package caldav

import (
	"testing"

	"github.com/emersion/go-ical"
	"github.com/zoomacode/homedash/internal/state"
)

// TestNew_ReturnsClient is a smoke test confirming that New() doesn't panic or
// return nil. Full PollOnce / VTODO coverage requires real iCloud credentials
// or hand-crafted CalDAV multistatus XML fixtures — both are out of scope here
// and will be exercised during the Task 13 integration review.
func TestNew_ReturnsClient(t *testing.T) {
	c := New("u@icloud.com", "pw", []string{"Personal"}, "Dashboard", state.New())
	if c == nil {
		t.Fatal("New returned nil")
	}
}

// TestNew_Defaults verifies that the client is initialised with the expected
// iCloud endpoint and a non-nil HTTP client.
func TestNew_Defaults(t *testing.T) {
	c := New("user", "pass", nil, "", state.New())
	if c.endpoint != defaultEndpoint {
		t.Errorf("endpoint = %q, want %q", c.endpoint, defaultEndpoint)
	}
	if c.httpClient == nil {
		t.Error("httpClient is nil")
	}
}

// TestGetProp exercises the ical helper without touching the network.
func TestGetProp(t *testing.T) {
	comp := ical.NewComponent(ical.CompEvent)

	// Absent property returns empty string.
	if got := getProp(comp, ical.PropSummary); got != "" {
		t.Errorf("getProp on missing prop = %q, want empty", got)
	}

	// Present property returns its value.
	p := ical.NewProp(ical.PropSummary)
	p.Value = "Team standup"
	comp.Props.Set(p)
	if got := getProp(comp, ical.PropSummary); got != "Team standup" {
		t.Errorf("getProp = %q, want %q", got, "Team standup")
	}
}

// TestGetTime exercises the time-parsing helper.
func TestGetTime(t *testing.T) {
	comp := ical.NewComponent(ical.CompEvent)

	// Absent property returns false.
	if _, ok := getTime(comp, ical.PropDateTimeStart); ok {
		t.Error("getTime on missing prop returned ok=true")
	}

	// Valid UTC datetime parses correctly.
	p := ical.NewProp(ical.PropDateTimeStart)
	p.Value = "20260101T120000Z"
	comp.Props.Set(p)
	if _, ok := getTime(comp, ical.PropDateTimeStart); !ok {
		t.Error("getTime on valid datetime returned ok=false")
	}
}
