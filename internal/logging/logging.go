// Package logging sets up the structured log and the separate audit log.
//
// The structured log is JSON on stdout. systemd and launchd capture it and the
// existing promtail/alloy path ships it to Loki unchanged, so nothing here
// writes log files.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Options configures the process logger.
type Options struct {
	Level string
	// Out defaults to os.Stdout when nil.
	Out io.Writer
	// Identity fields are attached to every line. They are structured
	// attributes, never interpolated into the message.
	Project string
	Site    string
	Station string
	// Host is the machine name. It is a log attribute rather than an identity:
	// which box this is matters when reading logs, and never identifies the
	// station. Instance names the agent process, and is what the payloads and
	// the broker connection carry.
	Host         string
	Instance     string
	AgentVersion string
	// AddSource includes the emitting file and line. Useful at DEBUG.
	AddSource bool
}

// New builds the process logger. It returns an error for an unknown level
// rather than quietly falling back to INFO.
func New(opts Options) (*slog.Logger, error) {
	level, err := ParseLevel(opts.Level)
	if err != nil {
		return nil, err
	}
	out := opts.Out
	if out == nil {
		return nil, fmt.Errorf("logging: output writer is required")
	}
	handler := slog.NewJSONHandler(out, &slog.HandlerOptions{
		Level:     level,
		AddSource: opts.AddSource,
	})
	return slog.New(handler).With(
		"project", opts.Project,
		"site", opts.Site,
		"station", opts.Station,
		"host", opts.Host,
		"instance_id", opts.Instance,
		"agent_version", opts.AgentVersion,
	), nil
}

// ParseLevel maps a configuration string to a slog level.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("logging: unknown level %q, expected debug, info, warn or error", s)
}
