// Package mqtt is the transport: it owns the broker connection, the topic
// names, and the MQTT 5 properties each kind of message is published with.
//
// It knows nothing about scanners. What it publishes is bytes plus the
// delivery semantics section 5 specifies for that topic, which is what keeps
// the perishability rule of section 6 in one place rather than spread across
// call sites.
package mqtt

import "fmt"

// Topics are the five topics one station uses. Section 5.3.
//
// The segments are validated at config load to match [a-z0-9-]+, because they
// are interpolated into topic names here without escaping and a "/" in a
// station id would silently publish to somebody else's topic.
type Topics struct {
	base string
}

// NewTopics builds the topic set for one station identity.
func NewTopics(project, site, station string) Topics {
	return Topics{base: fmt.Sprintf("skuhus/%s/%s/%s", project, site, station)}
}

// Base is the station prefix, without a trailing slash.
func (topics Topics) Base() string { return topics.base }

// Scan carries scan events, QoS 1, with a message expiry interval.
func (topics Topics) Scan() string { return topics.base + "/scan" }

// Status carries the retained online and offline state, and is the will topic.
func (topics Topics) Status() string { return topics.base + "/status" }

// Heartbeat carries liveness counters, QoS 0.
func (topics Topics) Heartbeat() string { return topics.base + "/heartbeat" }

// Command is the inbound command topic. Subscribing to it is M3.
func (topics Topics) Command() string { return topics.base + "/cmd" }

// CommandResult is where command results are published when the command did
// not name a response topic of its own. M3.
func (topics Topics) CommandResult() string { return topics.base + "/cmd/result" }
