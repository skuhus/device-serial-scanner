// Package device defines the transport-independent view of an attached device.
//
// The interface is kept generic on purpose. A printer is not a scanner - it is
// network-to-device, its jobs are durable rather than perishable, and its
// errors (out of paper, head open) are routine rather than exceptional - so the
// abstraction is not unified yet. Keeping Device an interface is what allows one
// to be added without reworking the supervisor.
package device

import (
	"context"
	"time"
)

// Direction says which way data flows for a device kind. Scanners are Inbound.
// It exists so a future outbound device does not have to redefine the model.
type Direction string

const (
	// Inbound is device to network.
	Inbound Direction = "inbound"
	// Outbound is network to device.
	Outbound Direction = "outbound"
)

// Frame is one complete terminator-delimited unit read from a device.
//
// Raw holds bytes, not text. GS1-128 and Data Matrix payloads carry 0x1D group
// separators, and treating them as a string corrupts them.
type Frame struct {
	DeviceID string
	Raw      []byte
	At       time.Time
}

// Device is one attached device owned by the agent.
type Device interface {
	// ID is the configured device identifier.
	ID() string
	// Kind is the configured transport kind, for example "serial".
	Kind() string
	// Path is the configured device path.
	Path() string
	// Direction is the data flow direction.
	Direction() Direction
	// Run reads until ctx is cancelled, sending each complete frame to sink.
	//
	// Run owns reopening the device: a disconnect is an expected condition and
	// is retried with jittered backoff rather than returned. It returns only
	// when ctx is cancelled, or on an error that reopening cannot fix.
	//
	// Run blocks when sink is full. That is the intended backpressure: a human
	// cannot scan faster than the publisher drains, so blocking the reader
	// surfaces a stalled broker instead of hiding it behind a growing queue.
	Run(ctx context.Context, sink chan<- Frame) error
}
