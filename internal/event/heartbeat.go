package event

import "time"

// Heartbeat is the payload published on the heartbeat topic every 15 seconds.
// Section 5.6.
//
// It carries the counters that answer "is this station working" without needing
// a scan to happen: an operator's shift can be quiet, but a heartbeat is not.
// Its absence is what finds a dead station before a ticket does, and after the
// will-retention measurement in docs/spikes/m0-mqtt5.md it is the only signal
// that does so reliably.
type Heartbeat struct {
	// Identity as in the scan envelope and the status payload, for the same
	// reason: a heartbeat is most often read after being copied somewhere its
	// topic did not follow it to.
	Project      string `json:"project"`
	Site         string `json:"site"`
	Station      string `json:"station"`
	InstanceID   string `json:"instance_id"`
	AgentVersion string `json:"agent_version"`
	// UptimeSeconds counts from process start, so a restart loop is visible as
	// a counter that keeps returning to zero.
	UptimeSeconds int64 `json:"uptime_s"`
	// DeviceOpen has the same narrow meaning as in the status payload: this
	// agent holds the device open, not that the hardware is attached.
	DeviceOpen bool `json:"device_open"`
	// LastScanTS is null until this process has published a scan. It does not
	// survive a restart.
	LastScanTS *string `json:"last_scan_ts"`
	ScanCount  uint64  `json:"scan_count"`
	// PublishFailures counts scans the broker did not acknowledge within
	// publish_timeout. A station scanning normally with a rising failure count
	// is losing data, and nothing else in the stream says so.
	PublishFailures uint64 `json:"publish_failures"`
	// BufferDepth is how many framed scans are waiting for the publisher. It is
	// bounded by delivery.buffer_size; at that bound the reader blocks.
	BufferDepth int    `json:"buffer_depth"`
	AgentTS     string `json:"agent_ts"`
}

// HeartbeatState is the counter set a heartbeat reports.
type HeartbeatState struct {
	DeviceOpen      bool
	LastScan        time.Time
	ScanCount       uint64
	PublishFailures uint64
	BufferDepth     int
}

// NewHeartbeat builds a heartbeat payload for a process started at start.
func NewHeartbeat(id Identity, start, now time.Time, state HeartbeatState) Heartbeat {
	beat := Heartbeat{
		Project:         id.Project,
		Site:            id.Site,
		Station:         id.Station,
		InstanceID:      id.InstanceID,
		AgentVersion:    id.AgentVersion,
		UptimeSeconds:   int64(now.Sub(start).Seconds()),
		DeviceOpen:      state.DeviceOpen,
		ScanCount:       state.ScanCount,
		PublishFailures: state.PublishFailures,
		BufferDepth:     state.BufferDepth,
		AgentTS:         now.UTC().Format(TimeFormat),
	}
	if !state.LastScan.IsZero() {
		ts := state.LastScan.UTC().Format(TimeFormat)
		beat.LastScanTS = &ts
	}
	return beat
}
