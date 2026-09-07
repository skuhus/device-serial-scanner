package event

import "github.com/google/uuid"

// defaultID returns a UUIDv4. It is the dedup key: MQTT QoS 1 is at-least-once,
// so a consumer that has already seen this id must drop the redelivery.
func defaultID() string { return uuid.NewString() }
