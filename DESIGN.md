# Design notes

Decisions taken while building the device layer, the configuration layer and the
logging layer. Section references are to `device-agent-spec.md`.

## Scans are perishable: this agent does not do offline sync

The project principle that "offline is the normal case" does **not** apply here,
and this is deliberate rather than an oversight. That principle belongs to
handheld terminals, which own a session and can reconcile later. This agent owns
no session.

A scan has no meaning without the session currently bound to the station. A scan
buffered for ten minutes and replayed lands in a pick that already ended, which
is a corrupt input rather than a late sync (spec section 6).

The consequences, encoded in `delivery` and validated at startup:

- `scan_ttl` defaults to 30s and will be applied as the MQTT Message Expiry
  Interval, so the broker discards a stale scan rather than a consumer having to.
- `publish_timeout` defaults to 2s and is validated to be no longer than
  `scan_ttl`. Telling an operator a scan failed after the broker had already
  expired it is worse than useless.
- `buffer_size` defaults to 64 and is a bound, not a target. On full the reader
  blocks; a human cannot scan faster than the publisher drains, so blocking
  surfaces a stalled broker instead of hiding it behind a growing queue. The
  device layer already does this: `Device.Run` blocks on the sink channel.
- Nothing pending is persisted across a restart, by design.

The audit log is the forensic record, not a replay source.

Do not "fix" any of the above by applying the offline-first principle uniformly.

## Framing

### `max_frame_bytes` is the payload size, excluding the terminator

The spec does not say which. Payload was chosen so the number means the same
thing whatever the terminator is: switching a device from CR to CRLF does not
change how long a barcode may be.

The framer tolerates `len(terminator) - 1` bytes past the limit before declaring
a frame over size, so a maximum-length payload whose terminator has only partly
arrived is not rejected one read early.

### Any discard resynchronises to the next terminator

Resuming mid-frame after a discard emits the tail of a broken frame as if it
were a short barcode. That is silent corruption: a plausible-looking payload
that no barcode ever carried. Dropping the remainder is a visible loss the
operator can act on, which is the same reasoning as section 6 - an operator who
knows the scan did not land rescans.

Resynchronisation ends at the next terminator, or when the device falls silent
for one inter-character timeout.

### Choosing `inter_char_timeout`

The timeout ends resynchronisation as well as starting it, and that has a limit
worth stating. If a device stalls mid-frame for longer than one timeout period,
falls silent, and then sends the rest of the frame, the tail is treated as a new
frame and can be published as a short payload. The information needed to tell
that tail from a genuine new scan does not exist at this layer.

The mitigation is the timeout value. At 9600 baud a byte takes about a
millisecond and a scanner sends a scan as one continuous burst, so the 200ms
default is roughly 200 byte-times of slack. Set it above any gap that occurs
inside a real burst on the hardware in question, and it will not fire mid-frame.

The alternative - resynchronising until a terminator arrives, however long that
takes - was rejected because it makes the wrong-terminator failure quiet. A
scanner sending LF where the config says CR would emit one warning and then
discard every subsequent scan at DEBUG. Ending resynchronisation on silence
produces one warning per scan, which is the loud failure section 11's manual
acceptance checklist expects.

### A CR/CRLF mismatch is the one wrong terminator that is not loud

Measured on a Symbol 05e0:1701, not reasoned about. When the device sends CRLF
and the config says CR, the frame splits correctly on the CR and an orphaned
`0x0A` is left buffered. Scanning slowly, the inter-character timeout discards
it and nothing is lost. Scanning faster than that timeout, the stray byte is
still buffered when the next scan arrives and is **prepended to it**, producing a
frame that is emitted with `text_valid: true` and is indistinguishable from a
genuine barcode. The same capture also lost a scan to a resync discard caused by
the previous stray byte.

So this mismatch corrupts under load and looks fine at a desk, which is the
failure section 4.5 describes and the reason a per-model config sheet is not
optional. The full capture is in `docs/scanners/symbol-05e0-1701.md`.

A cheap defence exists and is **not implemented**: a frame whose first byte
belongs to a common terminator that is not part of the configured one is almost
certainly this mismatch. Whether such a frame should be dropped or published
with a warning is an operational decision rather than a technical one, so it is
recorded here rather than chosen unilaterally.

### The backoff resets on session duration, not on a successful open

Section 4.4 says not to spin. Resetting the reopen backoff whenever the port
opened and read at least once does spin: a failing cable lets the port
enumerate, open, and return one read timeout before it drops, so the backoff
returns to its initial value on every cycle. Measured against an injected
failing port, that produced 87 reopens per second, indefinitely.

The backoff now resets only when a session lasted at least `StableAfter`, which
defaults to the backoff ceiling of 30s. A device that has been up for half a
minute counts as healthy and reconnects promptly; one that flaps backs off to
the ceiling. The same measurement produces 7 reopens per second and climbing.

`StableAfter` is an internal option rather than a config key: the specification
fixes the configuration schema in section 9, and this is a tuning constant
rather than a per-station decision.

## Envelope

### `device_id` carries the configured device id

Section 5.4 shows `"device_id": "usb-Honeywell_1470g-if00"`, which is a by-id
path basename, while section 9 gives the device a config field `id:
scanner-main`. These disagree.

The configured id is used, for the same reason section 2 gives for station
identity: identity comes from configuration, not from inference. Naming a device
after its by-id basename in the config reproduces the spec's example exactly, and
`config.sample.yaml` says so. **Open question** - confirm which the ingest side
expects before M2 fixes it in consumers.

### `device_kind` and `direction` are not in the envelope yet

Section 12 asks for an envelope generic enough that a printer slots in, but
section 5.4 gives an exact payload that does not contain those fields. The exact
payload wins, because it is what consumers are being written against.
Genericity is carried by the `Device` interface and by `device.Direction`
instead, which is where a printer actually needs it. Adding the two fields is a
`schema: 2` change.

### The agent does not ask whether the clock is synchronised

`agent_ts` is the host's local clock in UTC, and nothing in the envelope vouches
for it. An earlier version queried `adjtimex` on Linux and published a
`clock_synced` flag; that is gone, along with the `internal/clock` package.

Knowing whether a clock is disciplined is not this agent's job. It is a
transport shim (section 1), the ingest side has its own clock to compare
against, and a flag that could only ever be answered on Linux invited consumers
to trust a timestamp because one platform said so. Hosts without an RTC still
boot with a fictional wall clock; that is a fleet provisioning problem, and the
station's silence between heartbeats says more about it than a boolean did.

### The whole frame is the payload

`raw_b64` is the complete frame with only the terminator removed. Nothing is
stripped, and `symbology` is always null.

The alternative was to strip a code identifier and report it. Real hardware
ruled that out. A Symbol 05e0:1701 prefixes every scan with a Symbol Code
Character whose length is not constant: one byte for the 1D symbologies, three
for the 2D ones, distinguished only by whether the first byte is `P`. A fixed
`aim_prefix_len` cannot express that, and a substring search is worse than
useless: `DAP00838418592059` is a Code 128 whose data begins `AP00`, so matching
on `P00` anywhere would mangle it.

Delimiting the identifier therefore needs a per-vendor table, and that table is
domain knowledge a transport shim must not carry (section 1). Upstream already
holds per-device definitions and can decode the identifier along with everything
else, from a payload that has not been altered on the way.

The `symbology` field stays in the envelope, always null, because section 5.4
fixes the payload shape and consumers are written against it. Section 5.4 already
allows null for "not transmitted". Removing the field entirely is a `schema: 2`
change if it is ever wanted.

The vendor table for the scanner in hand is recorded in
`docs/scanners/symbol-05e0-1701.md`, for the upstream side rather than for this
agent.

## Publishing

### The broker connection outlives the run context

Section 10 shuts down on SIGTERM by draining and exiting, and section 5.5 ends
that sequence with a retained `offline` status followed by a clean DISCONNECT.
Dialling the connection with the same context that the signal cancels breaks
both: measured against the development broker, the offline status failed with
"no connection available" and the broker delivered the will instead, reporting a
crash where there had been an orderly stop.

The connection therefore has its own context, cancelled after the supervisor
returns. The shutdown order is: devices stop, the buffer drains, the offline
status goes out, DISCONNECT, and only then is the connection context cancelled.

### The shutdown drain is bounded at 5 seconds

A frame already in the buffer gets its full `publish_timeout`, but the drain as
a whole stops after `DefaultDrainTimeout`. Without a bound, a full buffer
against an unresponsive broker holds the process open for `buffer_size` times
`publish_timeout`, which at the defaults is over two minutes spent delivering
scans whose sessions ended (section 6). What is left is recorded in the audit
log as `dropped`, not discarded silently.

Like `StableAfter` in the device layer, this is an internal constant rather than
a config key: section 9 fixes the schema, and this is a tuning value.

### The audit log is flushed per record

`Append` calls `Sync` on every line rather than leaving the write in the page
cache. A packing station loses power without warning, and a forensic record that
stops several scans before the lights went out cannot answer the question it
exists for.

The cost is one fsync per scan, which is affordable at the rate a human scans
and would not be at machine rates. That is the assumption to revisit if this
agent ever carries a device that emits continuously.

An audit write that fails is logged and does not stop publishing. A station that
cannot write its record is degraded; a station that stops scanning because a
disk is full is out of service, and the second is worse for the people using it.

### Shutdown does not wait on the network without a bound

Disconnecting writes a DISCONNECT packet, which means writing to a socket that
may be attached to a network that has gone away. The disconnect is therefore
given `publish_timeout` rather than an unbounded context: by that point the
agent has published everything it had, and an unbounded wait turns "the WAN
dropped" into "the service will not stop".

Measured against a frozen broker - the container paused, so the socket stays
established and nothing is refused - a SIGTERM took two seconds: the offline
status timed out at its own bound, and the disconnect returned.

### A failed publish is not retried

Retrying is what the delivery semantics of section 6 rule out. By the time a
retry lands the scan is stale, and the operator who sees no confirmation
rescans. The failure is counted, logged, and recorded in the audit log; the
heartbeat carries the count so a station that is scanning but not delivering is
visible without reading logs.

`autopaho`'s publish queue is left nil for the same reason. With a queue, a
publish made while disconnected is accepted and sent on reconnection, which is
precisely the offline replay this agent must not do.

### `device_open`, not `device_present`

The flag says one thing only: this agent currently holds the device open. It is
set from the serial layer's presence callback, which fires true after a
successful open and false when the open fails or the session ends.

"Present" reads as a statement about the hardware, and would be wrong in the
case that matters most. A scanner that is plugged in and enumerated but held by
another process, or refused by permissions, is present and unusable; reporting
it as present hides exactly the failure an operator is looking for. What the
agent knows is whether it has the port, so that is what the field says.

### `instance_id`, not `host`

Section 5.4 carries `host`, and a hostname is the wrong identifier for it: it is
not unique across a fleet, it changes under DHCP, and it cannot tell apart two
agents on one machine.

`instance_id` is `identity.instance`, defaulting to `identity.station`, which is
what section 5.2 fixes the MQTT client id to. It is settable because a client id
must be unique per broker connection: two processes sharing one disconnect each
other in a loop. So the same value names the agent in every payload and names
its connection on the broker, and `rabbitmqctl list_mqtt_connections` maps
straight onto the messages.

The machine name is not lost. It stays a log attribute, where "which box is
this" is the question being asked, and out of the payloads, where it never
identified anything.

**Reaching an agent from a message.** The topic is
`skuhus/<project>/<site>/<station>/<leaf>`, and all three payloads carry those
three segments, so any message says how to address the station that sent it.
That is per station, not per instance: two instances on one station share the
status, heartbeat and command topics. Measured, not assumed - a second instance
shutting down published a retained `offline` for a station whose first instance
was still running and scanning. Putting the instance into the topic tree is a
change to section 5.3 and is not made here.

### Status and heartbeat carry the identity block, which the specification omits

Sections 5.5 and 5.6 give payloads with no station in them; only the topic says
who sent one. That holds right up until a payload is copied somewhere its topic
did not follow it to - a log line, a bug report, a paste in a chat - and then it
is anonymous, and correlating it with a scan means reconstructing the topic by
hand.

Both now carry `project`, `site`, `station` and `host`, the same four field
names the scan envelope of section 5.4 uses, so one parse path reads all three.
It matters most in the will, which is the one message a consumer reads when the
agent can no longer speak for itself.

A will's `agent_ts` is worth knowing about while reading one: the broker sends a
payload composed at CONNECT, so that timestamp is when the agent connected, not
when it died.

### `device` is a fourth status reason

Section 5.5 lists `startup|shutdown|will`, and in the same paragraph asks for a
republish when the device appears or disappears. That republish has no reason
in the list, so it uses `device`. Consumers switching on the field need to
tolerate it.

A device transition arriving before the first broker connection publishes
nothing: it is the normal startup order, and the status published on connect
carries the current presence anyway.

### Credentials cannot travel in the broker URL

`broker.url` carrying userinfo is a validation error, not a supported way to
authenticate. A URL is visible in the process list, in every log line that names
the broker, and in a config file pasted into a ticket, which is the whole reason
section 8 puts credentials in a mode 0600 file. The error message does not quote
the URL back, because that would put the password into the output of whoever ran
`validate`.

As a second line, `Broker.RedactedURL` is what the logs and the `validate`
summary print.

### The credentials file is key=value

The specification names `broker.credentials_file` but not its format. It is
`username=` and `password=` lines, with `#` comments, rather than two bare
lines, so that a file edited by hand cannot silently swap the two. The password
is taken verbatim after the first `=`; a password may legitimately end in a
space, and trimming one produces an authentication failure that reads as a
broker fault. `SKUHUS_AGENT_MQTT_USERNAME` and `SKUHUS_AGENT_MQTT_PASSWORD` override the file,
per the precedence in section 9.

They name MQTT rather than the broker because that is what they authenticate.
The configuration section stays `broker:`, which section 9 fixes; the four other
`SKUHUS_AGENT_BROKER_*` variables are unchanged, so the environment currently
mixes both names.

### The dependency list grew by two, transitively

Section 10 asks for the dependency list to stay short. `paho.golang`'s
`autopaho` package, which section 5.2 mandates, brings `gorilla/websocket` and
`golang.org/x/net`. Neither is imported by this agent; both arrive because
autopaho supports WebSocket transports. Removing them means not using autopaho.

## Configuration

### Loading is strict in both directions

An unknown key in the YAML file and an unrecognised `SKUHUS_AGENT_*` variable
are both fatal. A misspelled setting that is silently ignored leaves a station
running a value the operator believes they changed, and the fleet then disagrees
with its own configuration management.

Devices are not settable from the environment. A list does not map onto flat
variables without inventing an indexing scheme.

### Validation reports every problem, not the first

A misconfigured station is fixed in one pass rather than one restart per typo.

### Checks that exist because of a specific failure

- `terminator` containing a backslash is rejected with the YAML quoting
  explained. `terminator: '\r'` in single quotes is the two characters backslash
  and r, and produces a device that frames nothing and logs a timeout per scan.
- `/dev/tty.*` is rejected wherever it appears, not only on macOS, and names the
  `/dev/cu.*` twin. A config written for a Mac should fail validation on the CI
  box too.
- `/dev/ttyACM<n>` and friends warn rather than fail. They work; they move
  between reboots and replug order.
- A `credentials_file` readable by group or other is rejected. Credentials
  everyone on the host can read are not per-station credentials.
- `publish_timeout` longer than `scan_ttl` is rejected, per section 6.

### `assert_config` defaults to false

The spec's sample sets it true, but honouring it needs a per-model command set
and the fleet's scanner models are open question 6. Enabling it logs at WARN on
every device open that the configuration was **not** asserted, rather than
passing quietly. Defaulting to true would make every station emit that warning
forever. The sample config ships it false with the reason attached.

## What is not built

This unit covers M1, M2, and the configuration and logging foundation. Not
present:

- Command dispatch and the feedback abstraction (M3). The `cmd` and `cmd/result`
  topic names exist in the transport; nothing subscribes to them.
- `packaging/` - systemd unit, launchd plist, udev rules including the
  ModemManager ignore - and the GitHub Actions workflows (M5).

M0, the broker MQTT 5 property spike, is written and run. It does not block M2
any longer, but it does block deployment: the broker at 10.9.21.23 is RabbitMQ
3.10.25 with no MQTT 5 support, so the agent as built cannot connect to it. M2
was developed against the local 4.3.5 in `dev/rabbitmq/` instead. Against a local 4.1.8 the properties
hold except for two, both on the retained status topic of section 5.5: a will is
delivered but not retained, and the retained store does not replicate across
cluster nodes. `docs/spikes/m0-mqtt5.md` has the measurements, and
`spike/brokerinfo` is what identifies a broker before the property spike runs.

`dev/rabbitmq/` holds a local RabbitMQ 4.3.5 with per-station credentials, so M2
can be written against a broker that behaves as section 5 describes while the
question of what the fleet runs is settled separately. Its definitions file is
the whole configuration: users, permissions and topic permissions are imported
on every boot, and the volume holds everything else.

## Naming

Identifiers say what they hold. Receivers are `agent`, `client`, `framer`,
`presence` rather than `a`, `c`, `f`, `p`, and locals are named for their
contents rather than for their type's first letter. This costs a few characters
per line and pays for itself the first time someone reads a function they did
not write.

The exceptions are `err`, `ok`, `ctx` and `t *testing.T`, which are read as
punctuation rather than as names.

Renaming has one hazard worth recording, because it happened here: a mechanical
rename collided with an existing variable in `Validate`, turning
`problems, warning := validateBroker(...)` followed by
`append(problems, problems...)` into a function that discarded every problem
found so far. It compiled. The tests caught it, which is the argument for having
tests that assert on rejections and not only on acceptances.

## Test harness

`internal/device/serial` creates its pseudo-terminal pair through `/dev/ptmx`
directly rather than shelling out to `socat`, so the tests need no external
process and no scanner on the desk. The device under test is opened with the
real `go.bug.st/serial` library on the slave path, so the port setup, the read
timeout and the disconnect behaviour are all exercised rather than mocked.

The captures in `internal/device/serial/testdata/` are synthesised, not recorded
from hardware. They cover the shapes the framer must survive; they are not
evidence about any specific scanner model. Replace them with real captures per
model as the fleet is confirmed - see the README in that directory.

Real USB unplug returns `EIO` from `read(2)`, which the code classifies but
which no pseudo-terminal can produce. That path is covered by injecting an
opener that fails; the manual acceptance checklist in section 11 still has to
cover a real unplug.

`symbol-05e0-1701-crlf.bin` holds real payloads from a real scanner and is
replayed alongside the synthesised captures. No GS1-128 payload has been
captured from hardware yet, so the 0x1D case - the one a text-based
implementation destroys - is still covered only by a synthesised file.

Three defects reached this codebase that the pseudo-terminal harness could not
have found, and all three came from one session with a real scanner: the modem
lines were never asserted, the read loop did not observe its context, and
`log_payloads` was wired to nothing so a misconfigured device could only be
diagnosed as a byte count. Hardware time is worth more than it looks.
