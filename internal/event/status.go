package event

import "time"

// Status is the payload published retained on the status topic, and registered
// as the Last Will. Section 5.5.
//
// The broker publishes the will when the agent dies without disconnecting,
// which is what makes "station 3 went offline" free. Note the measurement in
// docs/spikes/m0-mqtt5.md: RabbitMQ delivers the will but does not retain it,
// so a consumer connecting after a station died reads the last retained status,
// which still says online. The heartbeat is what closes that gap.
type Status struct {
	// The identity block is the same four fields, with the same names, as the
	// scan envelope in section 5.4. The topic already carries the identity, but
	// a payload lifted out of its topic - into a log line, a bug report, a
	// database row - is anonymous without them, and correlating it with a scan
	// then means reconstructing the topic by hand.
	Project    string `json:"project"`
	Site       string `json:"site"`
	Station    string `json:"station"`
	InstanceID string `json:"instance_id"`
	// State is "online" or "offline".
	State string `json:"state"`
	// DeviceOpen says whether this agent currently holds a configured device
	// open. It is deliberately narrow: it is what the agent knows, not what the
	// hardware is doing. A scanner that is plugged in but held by another
	// process, or refused by permissions, reports false, which is the point -
	// "present" would report true and hide exactly that failure.
	DeviceOpen   bool   `json:"device_open"`
	AgentVersion string `json:"agent_version"`
	// Since is when this state began, not when the message was sent.
	Since string `json:"since"`
	// Reason is "startup", "shutdown", "will" or "device".
	Reason string `json:"reason"`
	// AgentTS is when this message was built. Section 5.5 asks for it
	// separately from Since so a retained message's age is visible.
	//
	// In a will it is neither: the broker sends a payload composed at CONNECT,
	// so a will's agent_ts is when the agent connected, not when it died. The
	// time of death is when the message arrives, which only the consumer knows.
	AgentTS string `json:"agent_ts"`
}

// Reasons for a status publish.
const (
	ReasonStartup  = "startup"
	ReasonShutdown = "shutdown"
	ReasonWill     = "will"
	// ReasonDevice is a republish caused by a device present or absent
	// transition, which section 5.5 asks for by name.
	ReasonDevice = "device"
)

// States a station can report.
const (
	StateOnline  = "online"
	StateOffline = "offline"
)

// NewStatus builds a status payload. now and since are formatted as the
// envelope's UTC millisecond format so every timestamp this agent emits parses
// the same way.
func NewStatus(id Identity, state, reason string, deviceOpen bool, since, now time.Time) Status {
	return Status{
		Project:      id.Project,
		Site:         id.Site,
		Station:      id.Station,
		InstanceID:   id.InstanceID,
		State:        state,
		DeviceOpen:   deviceOpen,
		AgentVersion: id.AgentVersion,
		Since:        since.UTC().Format(TimeFormat),
		Reason:       reason,
		AgentTS:      now.UTC().Format(TimeFormat),
	}
}
