package event

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"
)

func testIdentity() Identity {
	return Identity{
		Project:      "acme",
		Site:         "vasby",
		Station:      "pack-03",
		InstanceID:   "pack-03",
		AgentVersion: "1.2.0",
	}
}

func fixedTime(t *testing.T) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339Nano, "2026-09-06T09:14:22.481Z")
	if err != nil {
		t.Fatalf("parse fixed time: %v", err)
	}
	return ts
}

// counterIDs makes event ids deterministic so a test can assert on the whole
// envelope.
func counterIDs() IDFunc {
	var n int
	return func() string {
		n++
		return "event-" + strconv.Itoa(n)
	}
}

// The envelope must match the published schema field for field. A consumer
// parses this, so a renamed or dropped key is a breaking change.
func TestScanEnvelopeMatchesSchema(t *testing.T) {
	builder := NewBuilder(testIdentity(), counterIDs())
	scan := builder.Scan([]byte("0123456789"), fixedTime(t), "usb-Honeywell_1470g-if00")

	encoded, err := json.Marshal(scan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := map[string]any{
		"event_id":      "event-1",
		"schema":        float64(1),
		"project":       "acme",
		"site":          "vasby",
		"station":       "pack-03",
		"instance_id":   "pack-03",
		"device_id":     "usb-Honeywell_1470g-if00",
		"raw_b64":       "MDEyMzQ1Njc4OQ==",
		"text":          "0123456789",
		"text_valid":    true,
		"symbology":     nil,
		"agent_ts":      "2026-09-06T09:14:22.481Z",
		"agent_version": "1.2.0",
		"seq":           float64(1),
	}
	if len(got) != len(want) {
		t.Errorf("envelope has %d fields %v, want %d", len(got), keys(got), len(want))
	}
	for k, w := range want {
		if g, present := got[k]; !present {
			t.Errorf("field %q is missing", k)
		} else if g != w {
			t.Errorf("field %q = %#v, want %#v", k, g, w)
		}
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The whole frame is carried. A Symbol scanner prefixes a code identifier that
// is one byte, or three when it starts with P, and telling those apart needs a
// per-vendor table. That table belongs upstream, so the agent strips nothing.
func TestScanCarriesTheWholeFrame(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"ean13 with code id", "A7393481008232"},
		{"gs1-128 with code id", "K97PAH932721210504"},
		{"code128 whose data begins with P00", "DAP00838418592059"},
		{"data matrix with three byte code id", "P007QFG3I113FHBTOK4PN4U07M7MEFDM149"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			builder := NewBuilder(testIdentity(), counterIDs())
			scan := builder.Scan([]byte(tc.raw), fixedTime(t), "scanner-main")

			decoded, err := base64.StdEncoding.DecodeString(scan.RawB64)
			if err != nil {
				t.Fatalf("decode raw_b64: %v", err)
			}
			if string(decoded) != tc.raw {
				t.Errorf("raw_b64 = %q, want the frame unmodified: %q", decoded, tc.raw)
			}
			if scan.Text == nil || *scan.Text != tc.raw {
				t.Errorf("text = %v, want %q", scan.Text, tc.raw)
			}
			if scan.Symbology != nil {
				t.Errorf("symbology = %q, want null; the agent does not parse code identifiers", *scan.Symbology)
			}
		})
	}
}

// A group separator inside a GS1 payload must survive, which is the whole
// reason payloads are carried as bytes.
func TestScanKeepsGroupSeparatorInGS1Payload(t *testing.T) {
	builder := NewBuilder(testIdentity(), counterIDs())
	raw := []byte("K4217524174\x1d09012002083496719")
	scan := builder.Scan(raw, fixedTime(t), "scanner-main")

	decoded, _ := base64.StdEncoding.DecodeString(scan.RawB64)
	if string(decoded) != string(raw) {
		t.Errorf("raw_b64 lost bytes: % x", decoded)
	}
	if !bytes.Contains(decoded, []byte{0x1d}) {
		t.Error("the group separator was lost")
	}
}

// A payload that is not valid UTF-8 must be reported as absent text, not as a
// lossy replacement-character string.
func TestScanNonUTF8PayloadHasNullText(t *testing.T) {
	builder := NewBuilder(testIdentity(), counterIDs())
	raw := []byte{'A', 'B', 0xff, 0xfe, 'C'}
	scan := builder.Scan(raw, fixedTime(t), "scanner-main")

	if scan.TextValid {
		t.Error("text_valid should be false for a payload that is not valid UTF-8")
	}
	if scan.Text != nil {
		t.Errorf("text = %q, want null", *scan.Text)
	}
	encoded, err := json.Marshal(scan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	json.Unmarshal(encoded, &got)
	if v, present := got["text"]; !present || v != nil {
		t.Errorf("text serialised as %#v, want an explicit null", v)
	}
	decoded, _ := base64.StdEncoding.DecodeString(scan.RawB64)
	if string(decoded) != string(raw) {
		t.Errorf("raw_b64 lost bytes: % x, want % x", decoded, raw)
	}
}

// A GS1 payload is valid UTF-8 even with 0x1D in it, so text is present and the
// separator survives the round trip.
func TestScanGS1PayloadKeepsGroupSeparatorInText(t *testing.T) {
	builder := NewBuilder(testIdentity(), counterIDs())
	raw := []byte("0104912345123459\x1d17250101")
	scan := builder.Scan(raw, fixedTime(t), "scanner-main")
	if !scan.TextValid || scan.Text == nil {
		t.Fatal("a GS1 payload is valid UTF-8 and should have text")
	}
	if *scan.Text != string(raw) {
		t.Errorf("text = %q, want %q", *scan.Text, raw)
	}
	encoded, _ := json.Marshal(scan)
	var round Scan
	if err := json.Unmarshal(encoded, &round); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if *round.Text != string(raw) {
		t.Errorf("after a JSON round trip text = %q, want %q", *round.Text, raw)
	}
}

func TestScanTimestampIsUTCWithMilliseconds(t *testing.T) {
	builder := NewBuilder(testIdentity(), counterIDs())
	local := time.Date(2026, 9, 6, 11, 14, 22, 481_000_000, time.FixedZone("CEST", 2*60*60))
	scan := builder.Scan([]byte("X"), local, "scanner-main")
	if scan.AgentTS != "2026-09-06T09:14:22.481Z" {
		t.Errorf("agent_ts = %q, want the UTC instant with milliseconds", scan.AgentTS)
	}
}

// seq is a per-boot counter used to spot gaps, so it must increase by one per
// scan and stay correct when several devices publish at once.
func TestScanSeqIsMonotonicAndConcurrencySafe(t *testing.T) {
	// The default UUID generator is used rather than the counter helper, which
	// is deliberately unsynchronised and would be the thing under test.
	builder := NewBuilder(testIdentity(), nil)
	const workers, each = 8, 100

	var wg sync.WaitGroup
	seen := make([][]uint64, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			seen[worker] = make([]uint64, 0, each)
			for i := 0; i < each; i++ {
				scan := builder.Scan([]byte("X"), time.Now(), "scanner-main")
				seen[worker] = append(seen[worker], scan.Seq)
			}
		}(worker)
	}
	wg.Wait()

	issued := make(map[uint64]bool, workers*each)
	for _, perWorker := range seen {
		for _, seq := range perWorker {
			if issued[seq] {
				t.Fatalf("seq %d was issued twice", seq)
			}
			issued[seq] = true
		}
	}
	if len(issued) != workers*each {
		t.Errorf("got %d distinct seq values, want %d", len(issued), workers*each)
	}
	if builder.Seq() != uint64(workers*each) {
		t.Errorf("final seq = %d, want %d", builder.Seq(), workers*each)
	}
}

// Event ids are the dedup key, so two scans of the same payload must differ.
func TestScanEventIDsAreDistinct(t *testing.T) {
	builder := NewBuilder(testIdentity(), nil)
	first := builder.Scan([]byte("SAME"), fixedTime(t), "scanner-main")
	second := builder.Scan([]byte("SAME"), fixedTime(t), "scanner-main")
	if first.EventID == second.EventID {
		t.Errorf("both scans got event id %q", first.EventID)
	}
	if len(first.EventID) != 36 {
		t.Errorf("event id %q is not a UUID", first.EventID)
	}
}
