package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Warning is a non-fatal configuration problem. Warnings do not stop the agent;
// they are logged at WARN and printed by the validate subcommand.
type Warning struct {
	Field   string
	Message string
}

func (warning Warning) String() string { return warning.Field + ": " + warning.Message }

var (
	// topicSegment is the character set the MQTT topic segments allow.
	topicSegment = regexp.MustCompile(`^[a-z0-9-]+$`)
	// deviceID is deliberately wider: operators name devices after the by-id
	// path, which carries mixed case and underscores.
	deviceID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	// MQTT 5 allows more, but a client id is read by people at least as often
	// as by brokers.
	instanceID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	// unstableDevPath matches kernel-assigned names that move between reboots
	// and replug order.
	unstableDevPath = regexp.MustCompile(`^/dev/tty(ACM|USB|S|AMA)[0-9]+$`)

	tlsSchemes       = map[string]bool{"tls": true, "ssl": true, "mqtts": true, "wss": true}
	plaintextSchemes = map[string]bool{"tcp": true, "mqtt": true, "ws": true}
)

// Validate checks the whole configuration and reports every problem it finds.
//
// env is consulted only to decide whether broker credentials are supplied
// without a credentials file; pass nil when that does not apply.
func Validate(cfg *Config, env map[string]string) ([]Warning, error) {
	var problems []error
	var warnings []Warning

	problems = append(problems, validateIdentity(cfg.Identity)...)

	brokerProblems, brokerWarnings := validateBroker(cfg.Broker, env)
	problems, warnings = append(problems, brokerProblems...), append(warnings, brokerWarnings...)

	deviceProblems, deviceWarnings := validateDevices(cfg.Devices)
	problems, warnings = append(problems, deviceProblems...), append(warnings, deviceWarnings...)
	problems = append(problems, validateDelivery(cfg.Delivery)...)
	problems = append(problems, validateLogging(cfg.Logging)...)

	return warnings, errors.Join(problems...)
}

func validateIdentity(identity Identity) []error {
	var problems []error
	for _, field := range []struct{ name, value string }{
		{"identity.project", identity.Project},
		{"identity.site", identity.Site},
		{"identity.station", identity.Station},
	} {
		switch {
		case field.value == "":
			problems = append(problems, fmt.Errorf("%s is required; station identity comes from config, never from the hostname", field.name))
		case !topicSegment.MatchString(field.value):
			problems = append(problems, fmt.Errorf("%s %q must match [a-z0-9-]+; it is used verbatim as an MQTT topic segment", field.name, field.value))
		}
	}
	// The instance is not a topic segment, so it is not held to [a-z0-9-]+. It
	// is an MQTT client id: the broker sees it, an operator greps for it, and
	// characters that need quoting in a shell or a log query make that worse.
	switch {
	case identity.Instance == "":
		// Empty means it takes the station id, which is checked above. Load
		// fills that in before validating, so this only happens when Validate
		// is called on a configuration assembled by hand.
	case !instanceID.MatchString(identity.Instance):
		problems = append(problems, fmt.Errorf(
			"identity.instance %q must match [A-Za-z0-9._-]+ and be at most 64 characters; it is the MQTT client id", identity.Instance))
	}
	return problems
}

func validateBroker(broker Broker, env map[string]string) ([]error, []Warning) {
	var problems []error
	var warnings []Warning

	if broker.URL == "" {
		problems = append(problems, errors.New("broker.url is required"))
	} else if parsed, err := url.Parse(broker.URL); err != nil {
		problems = append(problems, fmt.Errorf("broker.url %q is not a valid URL: %w", broker.URL, err))
	} else {
		// Checked separately from the scheme, because a URL can be wrong in
		// both ways at once and an operator should hear about both.
		if parsed.User != nil {
			// Section 8 keeps credentials in a mode 0600 file or in the
			// environment. In a URL they reach the process list, every log line
			// that reports the broker, and any config file attached to a
			// ticket.
			problems = append(problems, errors.New(
				"broker.url carries credentials; move them to broker.credentials_file or to "+
					EnvMQTTUsername+" and "+EnvMQTTPassword))
		}
		switch {
		case parsed.Host == "":
			problems = append(problems, fmt.Errorf("broker.url %q has no host", broker.URL))
		case tlsSchemes[parsed.Scheme]:
			if broker.Insecure {
				warnings = append(warnings, Warning{"broker.insecure",
					"set with a TLS scheme; certificate verification will be skipped"})
			}
		case plaintextSchemes[parsed.Scheme]:
			if !broker.Insecure {
				problems = append(problems, fmt.Errorf(
					"broker.url %q is plaintext; set broker.insecure: true to allow it (development only)", broker.URL))
			} else {
				warnings = append(warnings, Warning{"broker.url",
					"plaintext connection to the broker, allowed only because broker.insecure is set"})
			}
		default:
			problems = append(problems, fmt.Errorf(
				"broker.url scheme %q is not supported; use tls, ssl, mqtts, wss, or tcp/mqtt/ws with broker.insecure", parsed.Scheme))
		}
	}

	_, hasEnvPassword := env[EnvMQTTPassword]
	switch {
	case broker.CredentialsFile == "" && !hasEnvPassword:
		problems = append(problems, fmt.Errorf(
			"broker.credentials_file is required, or set %s; credentials are never accepted as CLI arguments because ps exposes them", EnvMQTTPassword))
	case broker.CredentialsFile != "":
		problems = append(problems, checkSecretFile("broker.credentials_file", broker.CredentialsFile)...)
	}

	if broker.CAFile != "" {
		if _, err := os.Stat(broker.CAFile); err != nil {
			problems = append(problems, fmt.Errorf("broker.ca_file %s: %w", broker.CAFile, err))
		}
	}

	if broker.Keepalive <= 0 {
		problems = append(problems, fmt.Errorf("broker.keepalive must be positive, got %s", broker.Keepalive))
	}
	if broker.ConnectBackoff.Initial <= 0 {
		problems = append(problems, fmt.Errorf("broker.connect_backoff.initial must be positive, got %s", broker.ConnectBackoff.Initial))
	}
	if broker.ConnectBackoff.Max < broker.ConnectBackoff.Initial {
		problems = append(problems, fmt.Errorf("broker.connect_backoff.max (%s) must be at least initial (%s)",
			broker.ConnectBackoff.Max, broker.ConnectBackoff.Initial))
	}
	if broker.ConnectBackoff.Jitter < 0 || broker.ConnectBackoff.Jitter > 1 {
		problems = append(problems, fmt.Errorf("broker.connect_backoff.jitter must be between 0 and 1, got %v", broker.ConnectBackoff.Jitter))
	}
	return problems, warnings
}

// checkSecretFile requires a regular file that no other user can read.
func checkSecretFile(field, path string) []error {
	info, err := os.Stat(path)
	if err != nil {
		return []error{fmt.Errorf("%s %s: %w", field, path, err)}
	}
	if !info.Mode().IsRegular() {
		return []error{fmt.Errorf("%s %s is not a regular file", field, path)}
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return []error{fmt.Errorf("%s %s has mode %04o; it must not be readable by group or other (chmod 0600)", field, path, perm)}
	}
	return nil
}

func validateDevices(devices []Device) ([]error, []Warning) {
	var problems []error
	var warnings []Warning

	if len(devices) == 0 {
		return []error{errors.New("devices must contain at least one entry")}, nil
	}

	seen := make(map[string]int, len(devices))
	seenPaths := make(map[string]int, len(devices))
	for i, deviceCfg := range devices {
		where := fmt.Sprintf("devices[%d]", i)
		switch {
		case deviceCfg.ID == "":
			problems = append(problems, fmt.Errorf("%s.id is required", where))
		case !deviceID.MatchString(deviceCfg.ID):
			problems = append(problems, fmt.Errorf("%s.id %q must match [A-Za-z0-9._-]+", where, deviceCfg.ID))
		default:
			if prev, dup := seen[deviceCfg.ID]; dup {
				problems = append(problems, fmt.Errorf("%s.id %q duplicates devices[%d]", where, deviceCfg.ID, prev))
			}
			seen[deviceCfg.ID] = i
			where = "devices." + deviceCfg.ID
		}

		switch deviceCfg.Kind {
		case KindSerial:
		case KindHID:
			problems = append(problems, fmt.Errorf(
				"%s.kind hid is not implemented in v1; configure the scanner into USB-CDC mode instead", where))
		case "":
			problems = append(problems, fmt.Errorf("%s.kind is required", where))
		default:
			problems = append(problems, fmt.Errorf("%s.kind %q is unknown; expected serial", where, deviceCfg.Kind))
		}

		pathProblems, pathWarnings := validateDevicePath(where, deviceCfg.Path)
		problems, warnings = append(problems, pathProblems...), append(warnings, pathWarnings...)
		if deviceCfg.Path != "" {
			// The second device to open the same port gets EBUSY, because the
			// library takes exclusive access. Catching it here names the
			// mistake instead of leaving one station permanently absent.
			if prev, dup := seenPaths[deviceCfg.Path]; dup {
				problems = append(problems, fmt.Errorf(
					"%s.path %q is already used by devices[%d]; two devices cannot share one port", where, deviceCfg.Path, prev))
			}
			seenPaths[deviceCfg.Path] = i
		}

		if deviceCfg.Baud <= 0 {
			problems = append(problems, fmt.Errorf("%s.baud must be positive, got %d", where, deviceCfg.Baud))
		}

		problems = append(problems, validateTerminator(where, deviceCfg.Terminator)...)

		if deviceCfg.MaxFrameBytes < 1 {
			problems = append(problems, fmt.Errorf(
				"%s.max_frame_bytes is the largest accepted payload excluding the terminator and must be at least 1, got %d", where, deviceCfg.MaxFrameBytes))
		}
		if deviceCfg.MaxFrameBytes > 1<<20 {
			problems = append(problems, fmt.Errorf(
				"%s.max_frame_bytes %d exceeds 1048576; the limit exists to bound a stuck device", where, deviceCfg.MaxFrameBytes))
		}
		if deviceCfg.InterCharTimeout <= 0 {
			problems = append(problems, fmt.Errorf("%s.inter_char_timeout must be positive, got %s", where, deviceCfg.InterCharTimeout))
		}
		if deviceCfg.AssertConfig {
			warnings = append(warnings, Warning{where + ".assert_config",
				"requested, but no per-model scanner profile is implemented yet; the agent will log that it could not assert the configuration"})
		}
	}
	return problems, warnings
}

func validateDevicePath(where, path string) ([]error, []Warning) {
	switch {
	case path == "":
		return []error{fmt.Errorf("%s.path is required", where)}, nil
	case !filepath.IsAbs(path):
		return []error{fmt.Errorf("%s.path %q must be absolute", where, path)}, nil
	case strings.HasPrefix(path, "/dev/tty."):
		return []error{fmt.Errorf(
			"%s.path %q is a macOS callin device; opening it blocks on carrier detect forever. Use the matching /dev/cu.* callout device", where, path)}, nil
	case unstableDevPath.MatchString(path):
		return nil, []Warning{{where + ".path", fmt.Sprintf(
			"%q is a kernel-assigned name that changes with reboot and replug order; bind to /dev/serial/by-id/... , a by-path entry, or a udev symlink", path)}}
	}
	return nil, nil
}

// validateTerminator rejects the single most likely YAML mistake: writing the
// terminator unquoted or single-quoted, which yields the two characters
// backslash and r rather than a carriage return.
func validateTerminator(where, term string) []error {
	if term == "" {
		return []error{fmt.Errorf("%s.terminator is required; state it per device rather than relying on a default", where)}
	}
	if strings.Contains(term, `\`) {
		return []error{fmt.Errorf(
			"%s.terminator %q contains a literal backslash; write it as a double-quoted YAML scalar so escapes are decoded, for example terminator: \"\\r\"", where, term)}
	}
	if len(term) > 8 {
		return []error{fmt.Errorf("%s.terminator is %d bytes; expected at most 8", where, len(term))}
	}
	return nil
}

func validateDelivery(deviceCfg Delivery) []error {
	var problems []error
	if deviceCfg.ScanTTL <= 0 {
		problems = append(problems, fmt.Errorf("delivery.scan_ttl must be positive, got %s", deviceCfg.ScanTTL))
	}
	if deviceCfg.PublishTimeout <= 0 {
		problems = append(problems, fmt.Errorf("delivery.publish_timeout must be positive, got %s", deviceCfg.PublishTimeout))
	}
	if deviceCfg.PublishTimeout > deviceCfg.ScanTTL {
		problems = append(problems, fmt.Errorf(
			"delivery.publish_timeout (%s) exceeds delivery.scan_ttl (%s); the operator would be told the scan failed after the broker had already expired it", deviceCfg.PublishTimeout, deviceCfg.ScanTTL))
	}
	if deviceCfg.BufferSize < 1 {
		problems = append(problems, fmt.Errorf("delivery.buffer_size must be at least 1, got %d", deviceCfg.BufferSize))
	}
	return problems
}

func validateLogging(logging Logging) []error {
	var problems []error
	switch strings.ToLower(logging.Level) {
	case "debug", "info", "warn", "error":
	default:
		problems = append(problems, fmt.Errorf("logging.level %q is unknown; expected debug, info, warn or error", logging.Level))
	}
	switch {
	case logging.AuditFile == "":
		problems = append(problems, errors.New("logging.audit_file is required; it is the forensic record of what each station published"))
	case !filepath.IsAbs(logging.AuditFile):
		problems = append(problems, fmt.Errorf("logging.audit_file %q must be absolute", logging.AuditFile))
	default:
		dir := filepath.Dir(logging.AuditFile)
		info, err := os.Stat(dir)
		if err != nil {
			problems = append(problems, fmt.Errorf("logging.audit_file directory %s: %w", dir, err))
		} else if !info.IsDir() {
			problems = append(problems, fmt.Errorf("logging.audit_file directory %s is not a directory", dir))
		}
	}
	if logging.AuditMaxSizeMB < 1 {
		problems = append(problems, fmt.Errorf("logging.audit_max_size_mb must be at least 1, got %d", logging.AuditMaxSizeMB))
	}
	if logging.AuditKeep < 0 {
		problems = append(problems, fmt.Errorf("logging.audit_keep must not be negative, got %d", logging.AuditKeep))
	}
	return problems
}

// ValidateDevice checks a single device entry. It exists so the probe
// subcommand, which assembles a device from flags rather than a file, rejects
// exactly the mistakes the validate subcommand rejects.
func ValidateDevice(deviceCfg Device) ([]Warning, error) {
	problems, warnings := validateDevices([]Device{deviceCfg})
	return warnings, errors.Join(problems...)
}
