package logging

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func openAudit(t *testing.T, maxSizeMB, keep int) (*Audit, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.log")
	audit, err := OpenAudit(path, maxSizeMB, keep)
	if err != nil {
		t.Fatalf("OpenAudit: %v", err)
	}
	t.Cleanup(func() { audit.Close() })
	return audit, path
}

func readRecords(t *testing.T, path string) []AuditRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var out []AuditRecord
	scanner := bufio.NewScanner(f)
	// One test writes a record larger than the default 64 KB token limit.
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for scanner.Scan() {
		var record AuditRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("record is not JSON: %v\n%s", err, scanner.Text())
		}
		out = append(out, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return out
}

// The audit log answers "did station 3 send that scan", so it records the event
// id, the outcome and the payload length, and never the payload.
func TestAuditAppendsRecords(t *testing.T) {
	audit, path := openAudit(t, 1, 3)
	records := []AuditRecord{
		{EventID: "e1", Station: "pack-03", DeviceID: "scanner-main", Outcome: OutcomePublished, Bytes: 10, Seq: 1},
		{EventID: "e2", Station: "pack-03", DeviceID: "scanner-main", Outcome: OutcomeFailed, Bytes: 12, Seq: 2, Detail: "timeout"},
	}
	for _, record := range records {
		if err := audit.Append(record); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got := readRecords(t, path)
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	if got[0].EventID != "e1" || got[0].Outcome != OutcomePublished || got[0].Bytes != 10 {
		t.Errorf("record 0 = %+v", got[0])
	}
	if got[1].Outcome != OutcomeFailed || got[1].Detail != "timeout" {
		t.Errorf("record 1 = %+v", got[1])
	}
	for i, record := range got {
		if record.TS == "" {
			t.Errorf("record %d has no timestamp", i)
		}
	}
}

// Appending to an existing file must not truncate it: the audit log survives
// agent restarts.
func TestAuditAppendsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	first, err := OpenAudit(path, 1, 3)
	if err != nil {
		t.Fatalf("OpenAudit: %v", err)
	}
	if err := first.Append(AuditRecord{EventID: "before-restart", Outcome: OutcomePublished}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	first.Close()

	second, err := OpenAudit(path, 1, 3)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()
	if err := second.Append(AuditRecord{EventID: "after-restart", Outcome: OutcomePublished}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got := readRecords(t, path)
	if len(got) != 2 || got[0].EventID != "before-restart" || got[1].EventID != "after-restart" {
		t.Fatalf("records = %+v, want both entries in order", got)
	}
}

func TestAuditRotatesAtSizeLimitAndKeepsHistory(t *testing.T) {
	const keep = 3
	audit, path := openAudit(t, 1, keep)

	// Each record is padded so that a megabyte is reached quickly.
	pad := strings.Repeat("x", 4096)
	for i := 0; i < 1200; i++ {
		if err := audit.Append(AuditRecord{
			EventID: fmt.Sprintf("e%04d", i), Outcome: OutcomePublished, Detail: pad,
		}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	for i := 1; i <= keep; i++ {
		rotated := fmt.Sprintf("%s.%d", path, i)
		info, err := os.Stat(rotated)
		if err != nil {
			t.Fatalf("expected rotated file %s: %v", rotated, err)
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", rotated)
		}
	}
	if _, err := os.Stat(fmt.Sprintf("%s.%d", path, keep+1)); !os.IsNotExist(err) {
		t.Errorf("file beyond the retention limit still exists: %v", err)
	}

	// The newest rotated file must hold older events than the live file: the
	// history must be ordered, not shuffled by the rename chain.
	live := readRecords(t, path)
	previous := readRecords(t, path+".1")
	if len(live) == 0 || len(previous) == 0 {
		t.Fatalf("live=%d previous=%d records, want both populated", len(live), len(previous))
	}
	if previous[len(previous)-1].EventID >= live[0].EventID {
		t.Errorf("audit.log.1 ends at %s but audit.log starts at %s; rotation ordering is wrong",
			previous[len(previous)-1].EventID, live[0].EventID)
	}
}

// keep: 0 means rotate and discard, which must not leave a growing file behind.
func TestAuditKeepZeroDiscardsHistory(t *testing.T) {
	audit, path := openAudit(t, 1, 0)
	pad := strings.Repeat("x", 4096)
	for i := 0; i < 600; i++ {
		if err := audit.Append(AuditRecord{EventID: fmt.Sprintf("e%04d", i), Detail: pad}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Errorf("keep 0 should leave no rotated files: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat live file: %v", err)
	}
	if info.Size() > 1<<20 {
		t.Errorf("live file is %d bytes, want at most 1 MB", info.Size())
	}
}

// A single record larger than the limit must still be written rather than
// looping on rotation.
func TestAuditWritesRecordLargerThanLimit(t *testing.T) {
	audit, path := openAudit(t, 1, 1)
	if err := audit.Append(AuditRecord{EventID: "small"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	huge := strings.Repeat("y", 2<<20)
	if err := audit.Append(AuditRecord{EventID: "huge", Detail: huge}); err != nil {
		t.Fatalf("Append huge: %v", err)
	}
	got := readRecords(t, path)
	if len(got) != 1 || got[0].EventID != "huge" {
		t.Fatalf("records = %d, want the oversized record alone after rotation", len(got))
	}
}

func TestAuditConcurrentAppends(t *testing.T) {
	audit, path := openAudit(t, 8, 2)
	const writers, each = 8, 200

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := audit.Append(AuditRecord{
					EventID: fmt.Sprintf("w%d-e%d", w, i), Outcome: OutcomePublished,
				}); err != nil {
					t.Errorf("Append: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	if err := audit.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	got := readRecords(t, path)
	if len(got) != writers*each {
		t.Errorf("got %d records, want %d; concurrent writes were interleaved", len(got), writers*each)
	}
}

func TestOpenAuditRejectsBadArguments(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name            string
		path            string
		maxSizeMB, keep int
	}{
		{"empty path", "", 1, 1},
		{"zero size", filepath.Join(dir, "a.log"), 0, 1},
		{"negative keep", filepath.Join(dir, "a.log"), 1, -1},
		{"missing directory", filepath.Join(dir, "nope", "a.log"), 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := OpenAudit(tc.path, tc.maxSizeMB, tc.keep); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestAuditAppendAfterCloseFails(t *testing.T) {
	audit, _ := openAudit(t, 1, 1)
	if err := audit.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := audit.Append(AuditRecord{EventID: "e"}); err == nil {
		t.Error("appending to a closed audit log should fail")
	}
}
