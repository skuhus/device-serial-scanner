package event

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStatusFieldsAndFormat(t *testing.T) {
	since := time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC)
	now := since.Add(90 * time.Second)

	id := Identity{Project: "acme", Site: "vasby", Station: "pack-03", InstanceID: "pack-03", AgentVersion: "1.2.0"}
	s := NewStatus(id, StateOnline, ReasonStartup, true, since, now)
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]any{
		"project":       "acme",
		"site":          "vasby",
		"station":       "pack-03",
		"instance_id":   "pack-03",
		"state":         "online",
		"device_open":   true,
		"agent_version": "1.2.0",
		"since":         "2026-09-06T08:00:00.000Z",
		"reason":        "startup",
		"agent_ts":      "2026-09-06T08:01:30.000Z",
	}
	if len(got) != len(want) {
		t.Errorf("status has %d fields, want %d: %s", len(got), len(want), body)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

// The will is built before any device is open, so it must be able to say the
// station is offline with no device present.
func TestStatusOfflineWill(t *testing.T) {
	now := time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC)
	s := NewStatus(Identity{Station: "pack-03", AgentVersion: "1.2.0"}, StateOffline, ReasonWill, false, now, now)
	if s.State != "offline" || s.Reason != "will" || s.DeviceOpen {
		t.Errorf("got %+v, want an offline will with no device present", s)
	}
	// The will is what a consumer reads when the agent cannot speak for itself,
	// so it above all has to say which station it came from.
	if s.Station != "pack-03" {
		t.Errorf("station = %q, want pack-03", s.Station)
	}
}

// Local times must not reach the wire: the ingest side compares timestamps
// across stations in different timezones.
func TestStatusTimestampsAreUTC(t *testing.T) {
	zone := time.FixedZone("UTC+5", 5*3600)
	at := time.Date(2026, 9, 6, 13, 0, 0, 0, zone)
	s := NewStatus(Identity{AgentVersion: "1.2.0"}, StateOnline, ReasonDevice, true, at, at)
	if s.AgentTS != "2026-09-06T08:00:00.000Z" {
		t.Errorf("agent_ts = %q, want the UTC rendering", s.AgentTS)
	}
	if s.Since != "2026-09-06T08:00:00.000Z" {
		t.Errorf("since = %q, want the UTC rendering", s.Since)
	}
}
