// Package event builds the envelope that wraps a device payload.
//
// The envelope adds identity, timing and provenance. It does not interpret the
// payload: the agent does not know what a SKU is, and a scan is an input rather
// than a stock movement.
package event

import (
	"encoding/base64"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Schema is the envelope version. Consumers switch on it.
const Schema = 1

// TimeFormat is RFC 3339 with milliseconds, always in UTC.
const TimeFormat = "2006-01-02T15:04:05.000Z07:00"

// Scan is the payload published on the scan topic.
//
// Field order matches the specification so a diff against it stays readable.
type Scan struct {
	EventID string `json:"event_id"`
	Schema  int    `json:"schema"`
	// Project is an untrusted hint. The authoritative isolation boundary is
	// derived server-side from the agent's broker credentials.
	Project string `json:"project"`
	Site    string `json:"site"`
	Station string `json:"station"`
	// InstanceID names the agent process that sent this. It is the MQTT client
	// id, so it is also how a connection on the broker maps back to a station,
	// and it defaults to the station id. The topic to reach that agent is
	// skuhus/<project>/<site>/<station>, which the three fields above give.
	//
	// It replaces the specification's "host": a hostname is not unique across
	// a fleet, changes under DHCP, and says nothing about which of two agents
	// on one machine sent the message.
	InstanceID string `json:"instance_id"`
	DeviceID   string `json:"device_id"`
	// RawB64 is the complete frame, terminator excluded. Nothing is stripped:
	// any code identifier the scanner prefixes is part of the payload, and
	// decoding it is the upstream side's job using its per-device definitions.
	RawB64 string `json:"raw_b64"`
	// Text is present only when the payload is valid UTF-8. It is null
	// otherwise rather than a lossy best effort.
	Text      *string `json:"text"`
	TextValid bool    `json:"text_valid"`
	// Symbology is always null. The agent does not parse code identifiers: they
	// are vendor-specific, variable length, and indistinguishable from payload
	// without a per-model table, which is domain knowledge a transport shim
	// must not carry. The field is kept so the envelope still matches the
	// documented schema.
	Symbology *string `json:"symbology"`
	// AgentTS is the host's local clock, in UTC, and nothing here vouches for
	// it. A host without an RTC boots with a fictional wall clock; deciding
	// whether to trust this timestamp, or to stamp its own on ingest, belongs
	// to the receiving side, which has its own clock to compare against.
	AgentTS      string `json:"agent_ts"`
	AgentVersion string `json:"agent_version"`
	// Seq is a per-boot monotonic counter, for spotting gaps in a station's
	// stream. It does not survive a restart and is not a dedup key.
	Seq uint64 `json:"seq"`
}

// Identity is the fixed part of every envelope this agent emits.
type Identity struct {
	Project      string
	Site         string
	Station      string
	InstanceID   string
	AgentVersion string
}

// IDFunc returns a fresh event id. It is a field so tests can make output
// deterministic.
type IDFunc func() string

// Builder produces envelopes for one agent process.
type Builder struct {
	identity Identity
	newID    IDFunc
	seq      atomic.Uint64
}

// NewBuilder creates a builder. newID may be nil, in which case UUIDv4 is used.
func NewBuilder(identity Identity, newID IDFunc) *Builder {
	if newID == nil {
		newID = defaultID
	}
	return &Builder{identity: identity, newID: newID}
}

// Scan wraps one frame. The frame is carried whole.
func (builder *Builder) Scan(raw []byte, at time.Time, deviceID string) Scan {
	payload := raw

	scan := Scan{
		EventID:      builder.newID(),
		Schema:       Schema,
		Project:      builder.identity.Project,
		Site:         builder.identity.Site,
		Station:      builder.identity.Station,
		InstanceID:   builder.identity.InstanceID,
		DeviceID:     deviceID,
		RawB64:       base64.StdEncoding.EncodeToString(payload),
		Symbology:    nil,
		AgentTS:      at.UTC().Format(TimeFormat),
		AgentVersion: builder.identity.AgentVersion,
		Seq:          builder.seq.Add(1),
	}
	if utf8.Valid(payload) {
		text := string(payload)
		scan.Text, scan.TextValid = &text, true
	}
	return scan
}

// Seq is the counter value most recently assigned.
func (builder *Builder) Seq() uint64 { return builder.seq.Load() }
