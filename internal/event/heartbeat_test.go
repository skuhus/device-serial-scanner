package event

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHeartbeatFields(t *testing.T) {
	start := time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC)
	now := start.Add(125 * time.Second)
	lastScan := start.Add(60 * time.Second)

	id := Identity{Project: "acme", Site: "vasby", Station: "pack-03", InstanceID: "pack-03", AgentVersion: "1.2.0"}
	h := NewHeartbeat(id, start, now, HeartbeatState{
		DeviceOpen:      true,
		LastScan:        lastScan,
		ScanCount:       7,
		PublishFailures: 2,
		BufferDepth:     3,
	})
	body, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := map[string]any{
		"project":          "acme",
		"site":             "vasby",
		"station":          "pack-03",
		"instance_id":      "pack-03",
		"agent_version":    "1.2.0",
		"uptime_s":         float64(125),
		"device_open":      true,
		"last_scan_ts":     "2026-09-06T08:01:00.000Z",
		"scan_count":       float64(7),
		"publish_failures": float64(2),
		"buffer_depth":     float64(3),
		"agent_ts":         "2026-09-06T08:02:05.000Z",
	}
	if len(got) != len(want) {
		t.Errorf("heartbeat has %d fields, want %d: %s", len(got), len(want), body)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

// A station that has not scanned yet reports null rather than the zero time,
// which would read as a scan in 1970.
func TestHeartbeatNullLastScanBeforeAnyScan(t *testing.T) {
	start := time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC)
	h := NewHeartbeat(Identity{AgentVersion: "1.2.0"}, start, start.Add(time.Second), HeartbeatState{})
	if h.LastScanTS != nil {
		t.Fatalf("last_scan_ts = %v, want nil", *h.LastScanTS)
	}
	body, _ := json.Marshal(h)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := got["last_scan_ts"]; !ok || v != nil {
		t.Errorf("last_scan_ts = %v, want a present null field", v)
	}
}
