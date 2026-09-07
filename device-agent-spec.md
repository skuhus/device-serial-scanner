# SKU Hus Serial Scanner Device Agent — Implementation Task Description

**Working name:** `device-serial-scanner` (single binary, one process per host)
**Status:** ready to start; Spike 0 must complete before M2 is designed in detail
**Layer:** transport shim. Sits below `devices` / `terminals`. Not a ledger participant.

---

## 1. Purpose

Give network access to devices physically attached to host computers. Phase 1 covers
wired barcode scanners: a scan made at a host must be consumable by components running
elsewhere, and the host must be able to receive commands back (beep, enable/disable,
config assert).

### Non-goals (enforce these in review)

- **No domain logic.** The agent does not know what a SKU is, what a project is beyond an
  opaque identifier, or what a scan means. It moves bytes and adds an envelope.
- **Not a ledger participant.** A scan event is an *input*, not a movement. Turning
  `station 3 saw payload X at T` into a stock movement happens server-side in
  `operations`/`terminals`.
- **Not an offline-sync client.** Handheld terminals do offline sync; this agent does not.
  See §6.
- **No printer support in phase 1.** Design for it (§12), do not build it.

---

## 2. Terminology

| Term | Meaning |
|---|---|
| **host** | the physical computer the device is plugged into |
| **station** | logical identity of a scanning position; 1 host may serve N stations |
| **project** | the isolation boundary (SKU Hus `Project`, not "tenant") |
| **site** | physical location grouping stations; routing convenience only |
| **agent** | one `skuhus-dev-serial-scanner` process, owning 1..N devices on one host |

Station identity comes from **config**, never from hostname inference. Hostname is a
logged attribute, not an identity.

---

## 3. Platform targets

| OS | Arch | Notes |
|---|---|---|
| Linux | amd64 | primary |
| Linux | arm64 | Raspberry Pi 4/5, 64-bit Pi OS |
| Linux | armv7 | older Pi / Zero 2 — include unless fleet is confirmed 64-bit |
| macOS | arm64, amd64 | secondary; `launchd`, not `systemd` |

`CGO_ENABLED=0`, fully static. No runtime dependency on Python, glibc version, or a
package manager.

---

## 4. Device layer

### 4.1 Interface mode

**USB-CDC (virtual COM) only in v1.** Every scanner in the fleet must be configured into
CDC mode before deployment. HID keyboard mode is explicitly out of scope for v1 — leave a
`device.kind` field in config so an `hid` implementation can be added later behind
`EVIOCGRAB`, but do not build it now.

Rationale: HID mode requires scancode→character translation against an assumed keyboard
layout. With Swedish layouts on hosts and US-configured scanners, non-alphanumeric
payloads corrupt silently and intermittently.

### 4.2 Device resolution

- Config specifies a **stable path**, never `/dev/ttyACM0`. Accept `/dev/serial/by-id/…`
  or a udev-created symlink. Ship a sample udev rule in `packaging/udev/`.
- Support `by-path` binding for "the scanner in the left USB port is station A".
- macOS: `/dev/cu.*` only. Reject `/dev/tty.*` at config-validation time with an
  explanatory error — opening it blocks on carrier detect forever.

### 4.3 Environment hazards (document in the install guide, ship fixes in packaging)

- **ModemManager** opens `/dev/ttyACM*` on hotplug and sends AT commands.
  Ship a udev rule setting `ENV{ID_MM_DEVICE_IGNORE}="1"`.
- **brltty** claims some USB-serial chipsets. Install docs must say to check for it.
- Linux permissions: `dialout` group. Package postinstall must not silently fail here.

### 4.4 Read loop

- Read into a buffer; split on the configured terminator (`CR`, `LF`, `CRLF`, or custom
  byte sequence). **Never assume one read equals one scan.**
- Enforce `max_frame_bytes` (default 4096) and an inter-character timeout (default 200ms).
  Exceeding either discards the partial frame and logs at WARN — a stuck device must not
  grow the buffer without bound.
- **Payloads are bytes, not text.** GS1-128 and Data Matrix carry `0x1D` group separators.
  Carry raw bytes; derive a decoded-text field only when the payload is valid UTF-8, and
  mark it as absent otherwise.
- On `EIO` / `ENODEV` / persistent zero-length reads: close, mark device absent, reopen
  after jittered backoff (100ms → 30s cap). Do not spin.

### 4.5 Scanner configuration

- Keep a **config barcode sheet per scanner model** in `docs/scanners/`. Non-negotiable:
  a replacement scanner that suffixes `CRLF` instead of `CR` produces a bug nobody can
  reproduce.
- Where the model accepts serial config commands, the agent **asserts** the expected mode
  (terminator, no prefix, no AIM identifier) on every device open, and logs a mismatch.

---

## 5. Transport — MQTT

### 5.1 Broker

Target the existing `rabbitmq:4.1-management` with `rabbitmq_mqtt` enabled, unless Spike 0
says otherwise.

> **Spike 0 (blocking, do before M2):** verify against the actual broker that MQTT 5
> properties behave as needed — *Message Expiry Interval*, *Response Topic*,
> *Correlation Data*, *Last Will and Testament*, and retained-message behaviour under a
> multi-node cluster. Broker support for MQTT 5 properties varies, and RabbitMQ's retained
> store has had per-node rather than replicated semantics. If any of these do not hold,
> the fallback is a dedicated broker (Mosquitto for a single site, EMQX for multi-site)
> bridged to RabbitMQ, and this document needs revising before M2.

### 5.2 Protocol settings

- **MQTT 5.0.** Client: `github.com/eclipse/paho.golang` (`autopaho` for supervised
  reconnect). Do not use `paho.mqtt.golang` — it is 3.1.1 and lacks the properties above.
- **Clean Start = true, Session Expiry = 0.** Deliberate: queued stale commands are
  harmful (§6). A reconnecting agent starts fresh.
- Keepalive 30s. TCP keepalive on as well.
- Client ID = station id. Broker credentials are per-station (§8).

### 5.3 Topics

```
skuhus/<project>/<site>/<station>/scan        agent → broker   QoS 1
skuhus/<project>/<site>/<station>/status      agent → broker   QoS 1, retained, LWT
skuhus/<project>/<site>/<station>/heartbeat   agent → broker   QoS 0
skuhus/<project>/<site>/<station>/cmd         broker → agent   QoS 1
skuhus/<project>/<site>/<station>/cmd/result  agent → broker   QoS 1
```

`<project>`, `<site>`, `<station>` are `[a-z0-9-]+`. Validate at config load.

### 5.4 Scan event payload

```json
{
  "event_id":      "uuid-v4",
  "schema":        1,
  "project":       "acme",
  "site":          "vasby",
  "station":       "pack-03",
  "host":          "pi-vasby-07",
  "device_id":     "usb-Honeywell_1470g-if00",
  "raw_b64":       "MDEyMzQ1Njc4OQ==",
  "text":          "0123456789",
  "text_valid":    true,
  "symbology":     "T",
  "agent_ts":      "2026-09-06T09:14:22.481Z",
  "clock_synced":  true,
  "agent_version": "1.2.0",
  "seq":           41827
}
```

- `event_id` is the dedup key. QoS 1 is at-least-once; consumers dedupe on it.
- `symbology` only when the scanner reports it (AIM identifier); null if not transmitted. 
  config sets if the feature is enabled for the device. We don't translate symbologies here
  because those may vary between vendors. Also config should set length of the AIM prefix, default 1.
- **`project` in the payload is an untrusted hint.** The authoritative boundary is derived
  server-side from the agent's credentials, but this will be implemented later.
- `clock_synced` — Raspberry Pis have no RTC; `agent_ts` is fiction until NTP syncs.
  Read `adjtimex`/`timedatectl` state on Linux; server also stamps on ingest.
- `seq` is a per-boot monotonic counter, useful for spotting gaps.

Publish with **Message Expiry Interval = `scan_ttl_seconds`** (default 30). This is the
perishability mechanism (§6), enforced by the broker rather than by consumers.

### 5.5 Status / LWT

Retained on `…/status`, and registered as the Last Will:

```json
{ "state": "online|offline", "device_present": true, "agent_version": "1.2.0",
  "since": "2026-09-06T08:00:00Z", "reason": "startup|shutdown|will" }
```

LWT gives "station 3 went offline" for free. Publish `online` on connect,
`offline`/`shutdown` on clean exit, and republish on device present/absent transitions.

This must include agent_ts as well.

### 5.6 Heartbeat

Every 15s on `…/heartbeat`, QoS 0, message expiry 60s:
agent version, uptime, device present, last scan time, scan count, publish failure count,
buffer depth. "No heartbeat from `pack-03` in 5 minutes" is how a dead scanner is found
before an operator files a ticket.

Heartbeats must include agent_ts as well.

### 5.7 Command channel

Subscribe `…/cmd`. Commands carry *Correlation Data* and *Response Topic*; the agent
replies on `…/cmd/result` (or the supplied response topic) with the same correlation data.

v1 command set — build the dispatch, stub what has no hardware yet:

| Command | Effect |
|---|---|
| `beep` | good/bad-read feedback (params: pattern) |
| `enable` / `disable` | gate scan publishing when no session is bound to the station |
| `assert_config` | re-apply expected scanner serial config |
| `ping` | liveness with round-trip timing |

Unknown commands return an error result, never silence. Reject any command whose topic
does not match this agent's own station.

**Operator feedback matters.** On the BA92, the C-Media playback device is available for
the beep path; on other hosts, feedback may be scanner-side (many scanners accept a beep
command over the same serial link) or screen-side. Keep the feedback mechanism pluggable.

---

## 6. Delivery semantics — read this before writing the buffer

A scan is **perishable**. It has no meaning without the session currently bound to the
station. A scan buffered for ten minutes and replayed lands in a pick that already ended:
that is a corrupt input, not a late sync.

Therefore:

1. **Short TTL.** `scan_ttl_seconds` default 30, via MQTT Message Expiry Interval.
2. **Fail loudly at the edge.** If a scan cannot be confirmed published within
   `publish_timeout` (default 2s), the operator learns immediately — error beep, status
   change, station UI red. An operator who knows the scan did not land rescans; one who
   does not, does not.
3. **Bounded local buffer, for broker blips only.** Default 64 events. On full: **block
   the reader** (a human cannot scan faster than the buffer drains) and surface the
   degraded state. Do not drop oldest, do not grow.
4. **No local persistence of pending scans across restart.** A restart discards the
   buffer, deliberately.
5. **Local audit log** (§7) is the forensic record. It is not a replay source.

This is a deliberate departure from the project's "offline is the normal case" principle,
which applies to handheld terminals that own a session. Note that departure explicitly in
`DESIGN.md` so it does not get "fixed" later by someone applying the principle uniformly.

---

## 7. Logging and observability

- `log/slog`, JSON handler, to stdout. `systemd`/`launchd` captures it; the existing
  promtail/alloy → Loki path picks it up unchanged.
- Standard fields on every line: `station`, `project`, `site`, `host`, `agent_version`,
  `device_id`. Structured, never interpolated into the message string.
- Levels: DEBUG (frame-level), INFO (connect, device open, state change), WARN (retry,
  malformed frame, config mismatch), ERROR (publish failure, unrecoverable device error).
- **Decide payload logging explicitly.** Default: log `event_id` and payload *length*, not
  content, at INFO; full payload at DEBUG only. SKUs are probably not sensitive, but make
  it a choice rather than an accident.
- Separate append-only **audit log** file of published events (`event_id`, timestamp,
  publish outcome), rotated by size, retention configurable. This is what answers "did
  station 3 actually send that scan at 14:02".
- Optional `--metrics-addr` exposing Prometheus counters. Low priority — heartbeat covers
  most of it.

---

## 8. Security

- **TLS to the broker**, cert pinning or a pinned CA bundle. Refuse plaintext unless
  `insecure: true` is explicitly set (dev only, logged at WARN every reconnect).
- **Per-station credentials.** Never a shared fleet user. Broker ACLs restrict each
  station to publish only its own `scan`/`status`/`heartbeat`/`cmd/result` topics and
  subscribe only its own `cmd`.
- Credentials from file (mode 0600) or environment, never from CLI args (visible in `ps`).
- Longer term: x509 client certs, which turns credential distribution across N boxes into
  a provisioning step rather than a secret-handling problem. Not v1.
- Server-side ingest treats every agent-supplied identity field as a hint and validates
  against the credential. A misconfigured Pi publishing another project's topic must be
  rejected and alerted, not merely ignored.

---

## 9. Configuration

Precedence: CLI flags > environment (`SKUHUS_AGENT_*`) > config file > defaults.
File at `/etc/skuhus-agent/config.yaml` (Linux) or
`/usr/local/etc/skuhus-agent/config.yaml` (macOS).

```yaml
identity:
  project: acme
  site: vasby
  station: pack-03

broker:
  url: tls://mq.internal:8883
  credentials_file: /etc/skuhus-agent/credentials
  keepalive: 30s
  connect_backoff: { initial: 1s, max: 60s, jitter: 0.3 }

devices:
  - id: scanner-main
    kind: serial            # serial | hid (hid not implemented in v1)
    path: /dev/serial/by-id/usb-Honeywell_1470g-if00
    baud: 9600              # ignored for CDC, required for real RS-232
    terminator: "\r"
    max_frame_bytes: 4096
    inter_char_timeout: 200ms
    assert_config: true

delivery:
  scan_ttl: 30s
  publish_timeout: 2s
  buffer_size: 64

logging:
  level: info
  log_payloads: false
  audit_file: /opt/skuhus-dev-serial-agent/audit.log
  audit_max_size_mb: 64
  audit_keep: 7
```

Validate fully at startup and **exit non-zero with a specific message** on bad config.
Provide `skuhus-agent validate` and `skuhus-agent probe` (enumerate candidate devices,
open one, print decoded frames to stdout) — the latter is the field-diagnosis tool and
will save more time than any other feature.

---

## 10. Process structure

```
main
 └─ supervisor (context, signal handling: SIGTERM/SIGINT → drain then exit)
     ├─ transport   (autopaho connection manager; owns publish)
     ├─ device[i]   (open → read → frame → emit; restart on failure with backoff)
     ├─ dispatcher  (cmd subscription → device/feedback actions → result publish)
     └─ heartbeat   (ticker)
```

- Reader goroutine → bounded channel → single publisher goroutine. One writer per
  connection.
- Every goroutine takes a `context.Context` and exits on cancel. No goroutine leaks on
  device restart — test this.
- `autopaho` handles reconnect; do **not** hand-roll it, and do not assume a publish
  succeeded because the call returned.

### Repository layout

```
cmd/skuhus-agent/          main, flags, subcommands
internal/config/           load, validate, defaults
internal/device/           Device interface
internal/device/serial/    CDC / RS-232 implementation
internal/event/            envelope, encoding, ids
internal/transport/mqtt/   autopaho wiring, topics, LWT
internal/dispatch/         command handling
internal/feedback/         beep/LED abstraction
internal/logging/          slog setup, audit log
packaging/systemd/         unit file
packaging/udev/            stable symlinks, ModemManager ignore
packaging/launchd/         plist
docs/scanners/             per-model config barcode sheets
.github/workflows/         ci.yml, release.yml
.goreleaser.yaml
```

### Dependencies (keep this list short)

| Purpose | Module |
|---|---|
| serial | `go.bug.st/serial` |
| MQTT 5 | `github.com/eclipse/paho.golang` |
| UUID | `github.com/google/uuid` |
| config | `gopkg.in/yaml.v3` + stdlib flags |
| logging | stdlib `log/slog` |

Adding anything else needs a reason in the PR description.

---

## 11. Testing

- **PTY harness.** `socat -d -d pty,raw,echo=0 pty,raw,echo=0` gives a device pair; record
  real scans once (including a GS1-128 payload with `0x1D`) and replay them. Build this on
  day one — otherwise every change needs a scanner on the desk.
- Unit: framing (split reads, oversized frames, timeouts, terminator variants), envelope
  encoding, config validation.
- Integration in CI: Mosquitto as a service container, assert topic/payload/LWT/expiry
  behaviour end to end.
- Race detector on all test runs.
- Manual acceptance checklist (no CI): unplug mid-scan, broker down at startup, broker
  dropped mid-session, buffer saturation, wrong-terminator scanner, clock unsynced boot.

---

## 12. Forward compatibility — printers

Do not unify the abstraction yet. Different semantics:

| | scan | print job |
|---|---|---|
| durability | perishable, short TTL | durable, must survive restart |
| duplication | tolerable (dedup by id) | **not** tolerable |
| direction | device → network | network → device |
| errors | rare | routine: out of paper, head open, ribbon out |

Keep `Device` an interface and the envelope generic enough (`device_kind`, direction) that
a printer implementation slots in. Before writing one, check whether a CUPS raw queue or
a direct socket to port 9100 already covers the PD41.

---

## 13. CI/CD — GitHub Actions

### 13.1 `ci.yml` — on push and pull request

```
jobs:
  lint      golangci-lint, go vet, gofmt check, `go mod tidy` diff check
  test      go test -race -coverprofile ./...    (ubuntu-latest)
  itest     go test -tags integration ./...      with mosquitto service container
  build     matrix build only (no publish) for all target GOOS/GOARCH
```

- Cache modules and build cache via `actions/setup-go` with `cache: true`.
- Cross-compile checks are cheap; keep them in every PR so an arm-only breakage is caught
  before tagging.
- Enable Dependabot for `gomod` and `github-actions`.
- Pin all actions to a commit SHA, not a tag.

### 13.2 `release.yml` — on tag `v*`

Use **GoReleaser**. It covers cross-compilation, archives, checksums, changelog, nfpm
packaging and release upload in one config.

```yaml
permissions:
  contents: write     # release upload
  id-token: write     # cosign keyless (if signing)
  packages: write     # only if publishing a container image
```

Build matrix: `linux/amd64`, `linux/arm64`, `linux/arm` (v6, v7), `darwin/amd64`,
`darwin/arm64`. `CGO_ENABLED=0`.

Version injection:

```
-ldflags "-s -w -X main.version={{.Version}} -X main.commit={{.FullCommit}} -X main.date={{.Date}}"
```

`main.version` must feed the `agent_version` field in every event, heartbeat and log line.
That is how you answer "which stations are still on the old build".

Release artifacts:

- `tar.gz` per platform, containing the binary, sample config, systemd unit / launchd
  plist, udev rules, and README.
- `.deb` and `.rpm` via GoReleaser's built-in `nfpms` — installs the binary, a
  `skuhus-agent` system user, the udev rules (including the ModemManager ignore), the
  systemd unit, and `/etc/skuhus-agent/` at 0750. Postinstall must not enable the service
  automatically; config comes first.
- `checksums.txt`.
- SBOM (syft) and cosign keyless signatures — optional, but cheap to add now and awkward
  to retrofit once the fleet is deployed.

Notes:

- macOS binaries are unsigned and unnotarized. For a `launchd` daemon installed by an
  admin this is usually fine, but a binary downloaded through a browser will carry the
  quarantine attribute. Document `xattr -d com.apple.quarantine` in the macOS install
  steps, or plan notarization later if macOS hosts become common.
- Tag protection: releases only from `main`, tags matching `v[0-9]+.[0-9]+.[0-9]+`.
- Consider a `prerelease` path for `v*-rc*` tags so a station or two can run a candidate
  before fleet rollout.

### 13.3 Deployment (out of scope for v1, decide before fleet grows)

Rollout is currently manual: download the package, install, configure, enable. That is
acceptable for tens of hosts. Before it exceeds that, pick an approach — Ansible playbook
against an inventory, or a self-update check against the GitHub releases API. Do not build
self-update in v1.

---

## 14. Milestones

| # | Deliverable | Done when |
|---|---|---|
| **M0** | Spike 0: broker MQTT 5 property verification | expiry, LWT, response-topic, correlation-data, retained all confirmed against the real broker, or fallback chosen |
| **M1** | Serial read → stdout | `skuhus-agent probe` prints framed scans from a real scanner and from the PTY harness |
| **M2** | Publish path | scans on `…/scan` with envelope, TTL, QoS 1; `…/status` retained + LWT; heartbeat |
| **M3** | Command path | `ping`, `enable`/`disable`, `beep` (stub allowed) with correlation-data replies |
| **M4** | Logging + config | slog JSON reaching Loki, audit log, full config validation, `validate` subcommand |
| **M5** | CI/CD | `ci.yml` green on PRs; tagged release produces signed tarballs, deb, rpm for all targets |
| **M6** | Hardening | hotplug, backoff, bounded buffer with block-on-full, `assert_config`, manual acceptance checklist passed |

M0 gates M2. M1–M4 can overlap. M5 should land no later than M3 — CI is cheaper to add
before the codebase has shape than after.

---

## 15. Open questions to settle before M2

1. **Fleet size and topology.** Number of stations, number of sites, and whether a site
   can lose the WAN. This decides whether the single-broker assumption holds.
2. **Station↔session binding.** Who tells the agent that station `pack-03` currently has
   an active session? Is `enable`/`disable` driven by the terminal, or does the agent
   publish unconditionally and the server filter? This shapes the command channel.
3. **Feedback path per host type.** Scanner-side beep over serial, host audio, or station
   UI — probably all three eventually. Which one is v1?
4. **Multiple stations per host.** Supported by config, but is it a real deployment shape
   or theoretical? Affects whether one process owns N devices or N processes run per host.
5. **Payload sensitivity.** Confirms the `log_payloads` default and audit-log retention.
6. **Scanner models in the fleet.** Determines which config barcode sheets are needed and
   whether every model actually supports CDC mode.

