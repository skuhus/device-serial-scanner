package logging

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Outcome is the disposition of an event in the audit log.
type Outcome string

const (
	// OutcomePublished means the broker acknowledged the publish.
	OutcomePublished Outcome = "published"
	// OutcomeFailed means the publish was not confirmed within the timeout.
	OutcomeFailed Outcome = "failed"
	// OutcomeDropped means the event never reached the publisher.
	OutcomeDropped Outcome = "dropped"
)

// AuditRecord is one line of the audit log.
//
// The audit log answers "did station 3 actually send that scan at 14:02". It is
// a forensic record, not a replay source: a scan is perishable and replaying
// one after the fact lands it in a session that has already ended.
type AuditRecord struct {
	TS       string  `json:"ts"`
	EventID  string  `json:"event_id"`
	Station  string  `json:"station"`
	DeviceID string  `json:"device_id"`
	Outcome  Outcome `json:"outcome"`
	// Bytes is the payload length. The payload itself is deliberately absent.
	Bytes  int    `json:"bytes"`
	Seq    uint64 `json:"seq"`
	Detail string `json:"detail,omitempty"`
}

// Audit is an append-only JSON-lines file rotated by size.
type Audit struct {
	path     string
	maxBytes int64
	keep     int

	mu   sync.Mutex
	file *os.File
	size int64
}

// OpenAudit opens or creates the audit file.
//
// maxSizeMB is the size at which the file rotates; keep is how many rotated
// files are retained. keep of zero means rotate and discard the previous file.
// The parent directory must already exist: creating it here would paper over a
// packaging step that failed.
func OpenAudit(path string, maxSizeMB, keep int) (*Audit, error) {
	if path == "" {
		return nil, fmt.Errorf("audit: path is required")
	}
	if maxSizeMB < 1 {
		return nil, fmt.Errorf("audit: max size must be at least 1 MB, got %d", maxSizeMB)
	}
	if keep < 0 {
		return nil, fmt.Errorf("audit: keep must not be negative, got %d", keep)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("audit: directory %s: %w", filepath.Dir(path), err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("audit: %s is not audit directory", filepath.Dir(path))
	}

	audit := &Audit{path: path, maxBytes: int64(maxSizeMB) * 1024 * 1024, keep: keep}
	if err := audit.openFile(); err != nil {
		return nil, err
	}
	return audit, nil
}

func (audit *Audit) openFile() error {
	file, err := os.OpenFile(audit.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return fmt.Errorf("audit: open %s: %w", audit.path, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("audit: stat %s: %w", audit.path, err)
	}
	audit.file, audit.size = file, info.Size()
	return nil
}

// Append writes one record, rotating first if the record would take the file
// past its size limit. TS is filled in when empty.
func (audit *Audit) Append(record AuditRecord) error {
	if record.TS == "" {
		record.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("audit: encode record: %w", err)
	}
	line = append(line, '\n')

	audit.mu.Lock()
	defer audit.mu.Unlock()
	if audit.file == nil {
		return fmt.Errorf("audit: %s is closed", audit.path)
	}
	if audit.size > 0 && audit.size+int64(len(line)) > audit.maxBytes {
		if err := audit.rotate(); err != nil {
			return err
		}
	}
	written, err := audit.file.Write(line)
	audit.size += int64(written)
	if err != nil {
		return fmt.Errorf("audit: write %s: %w", audit.path, err)
	}
	// Flushed per record rather than left to the page cache. A packing station
	// loses power without warning, and a forensic record that ends several
	// scans before the lights went out cannot answer the question it exists
	// for. One fsync per scan is affordable at the rate a human scans; it would
	// not be at machine rates, and that is the assumption to revisit if this
	// agent ever carries something other than a scanner.
	if err := audit.file.Sync(); err != nil {
		return fmt.Errorf("audit: flush %s: %w", audit.path, err)
	}
	return nil
}

// rotate renames the current file to .1, shifting existing rotated files up and
// discarding anything past keep. The caller holds the mutex.
func (audit *Audit) rotate() error {
	if err := audit.file.Close(); err != nil {
		return fmt.Errorf("audit: close %s before rotation: %w", audit.path, err)
	}
	audit.file = nil

	if audit.keep == 0 {
		if err := os.Remove(audit.path); err != nil {
			return fmt.Errorf("audit: discard %s: %w", audit.path, err)
		}
		return audit.openFile()
	}

	if err := os.Remove(audit.rotatedPath(audit.keep)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("audit: remove oldest rotated file: %w", err)
	}
	for i := audit.keep - 1; i >= 1; i-- {
		from, to := audit.rotatedPath(i), audit.rotatedPath(i+1)
		if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("audit: rotate %s to %s: %w", from, to, err)
		}
	}
	if err := os.Rename(audit.path, audit.rotatedPath(1)); err != nil {
		return fmt.Errorf("audit: rotate %s: %w", audit.path, err)
	}
	return audit.openFile()
}

func (audit *Audit) rotatedPath(index int) string { return fmt.Sprintf("%s.%d", audit.path, index) }

// Sync flushes the file to disk.
func (audit *Audit) Sync() error {
	audit.mu.Lock()
	defer audit.mu.Unlock()
	if audit.file == nil {
		return nil
	}
	return audit.file.Sync()
}

// Close flushes and closes the audit file.
func (audit *Audit) Close() error {
	audit.mu.Lock()
	defer audit.mu.Unlock()
	if audit.file == nil {
		return nil
	}
	syncErr := audit.file.Sync()
	closeErr := audit.file.Close()
	audit.file = nil
	return errors.Join(syncErr, closeErr)
}
