package agent

import "sync"

// Presence tracks, per device, whether the device is currently open.
//
// It exists as its own type because of an ordering problem: the serial devices
// need their presence callback at construction, and the supervisor needs the
// devices at construction. The caller builds this first and hands it to both.
type Presence struct {
	mu      sync.Mutex
	state   map[string]bool
	changed chan struct{}
}

// NewPresence starts with every named device absent, which is true until one
// has been opened.
func NewPresence(deviceIDs ...string) *Presence {
	presence := &Presence{
		state: make(map[string]bool, len(deviceIDs)),
		// Capacity one, and sends are dropped when full: a transition that
		// arrives while one is already pending needs no second wake-up,
		// because the reader publishes current state rather than a history.
		changed: make(chan struct{}, 1),
	}
	for _, id := range deviceIDs {
		presence.state[id] = false
	}
	return presence
}

// Set records a transition. It is safe to call from a device's read loop and
// never blocks, which is what that callback requires.
func (presence *Presence) Set(deviceID string, present bool) {
	presence.mu.Lock()
	was, known := presence.state[deviceID]
	presence.state[deviceID] = present
	presence.mu.Unlock()

	if known && was == present {
		return
	}
	select {
	case presence.changed <- struct{}{}:
	default:
	}
}

// Any reports whether at least one device is open. It is what the status and
// heartbeat payloads carry as device_open: with one device configured, which is
// the common case, it is that device.
func (presence *Presence) Any() bool {
	presence.mu.Lock()
	defer presence.mu.Unlock()
	for _, present := range presence.state {
		if present {
			return true
		}
	}
	return false
}

// Changed fires after a transition.
func (presence *Presence) Changed() <-chan struct{} { return presence.changed }
