package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func newTestLogger(t *testing.T, level string) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	log, err := New(Options{
		Level: level, Out: &buf,
		Project: "acme", Site: "vasby", Station: "pack-03",
		Host: "pi-vasby-07", AgentVersion: "1.2.0",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return log, &buf
}

func decodeLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("log line is not JSON: %v\n%s", err, line)
	}
	return got
}

// Every line carries the station identity, so a Loki query can select one
// station out of the fleet without parsing message text.
func TestLoggerAttachesIdentityToEveryLine(t *testing.T) {
	log, buf := newTestLogger(t, "info")
	log.Info("device open")
	log.With("device_id", "scanner-main").Warn("discarded partial frame", "reason", "oversize")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), buf.String())
	}
	for i, line := range lines {
		got := decodeLine(t, line)
		for k, want := range map[string]any{
			"project": "acme", "site": "vasby", "station": "pack-03",
			"host": "pi-vasby-07", "agent_version": "1.2.0",
		} {
			if got[k] != want {
				t.Errorf("line %d field %q = %#v, want %#v", i, k, got[k], want)
			}
		}
	}

	second := decodeLine(t, lines[1])
	if second["device_id"] != "scanner-main" {
		t.Errorf("device_id = %#v, want scanner-main", second["device_id"])
	}
	// The reason is a structured field, not interpolated into the message.
	if second["msg"] != "discarded partial frame" {
		t.Errorf("msg = %#v, want the bare message", second["msg"])
	}
	if second["reason"] != "oversize" {
		t.Errorf("reason = %#v, want oversize", second["reason"])
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	log, buf := newTestLogger(t, "warn")
	log.Debug("frame bytes")
	log.Info("device open")
	log.Warn("retrying")
	log.Error("publish failed")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (warn and error):\n%s", len(lines), buf.String())
	}
	if got := decodeLine(t, lines[0])["level"]; got != "WARN" {
		t.Errorf("first line level = %#v, want WARN", got)
	}
}

// An unknown level is a configuration error, not a reason to quietly pick INFO
// and log at the wrong verbosity on a station nobody is watching.
func TestNewRejectsUnknownLevel(t *testing.T) {
	var buf bytes.Buffer
	if _, err := New(Options{Level: "verbose", Out: &buf}); err == nil {
		t.Fatal("an unknown level should be rejected")
	}
}

func TestNewRequiresOutput(t *testing.T) {
	if _, err := New(Options{Level: "info"}); err == nil {
		t.Fatal("a nil writer should be rejected")
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"":      slog.LevelInfo,
		"WARN":  slog.LevelWarn,
		" warn": slog.LevelWarn,
		"error": slog.LevelError,
	}
	for in, want := range cases {
		got, err := ParseLevel(in)
		if err != nil {
			t.Errorf("ParseLevel(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := ParseLevel("trace"); err == nil {
		t.Error("ParseLevel(trace) should fail")
	}
}
