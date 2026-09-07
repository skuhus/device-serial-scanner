package serial

import (
	"sync"
	"syscall"
	"testing"
	"time"

	goserial "go.bug.st/serial"
)

// flakyPort opens successfully, reports one read timeout, then fails. It is the
// shape a failing cable or a flapping USB hub produces: the port enumerates and
// can be opened, but the device does not stay up.
type flakyPort struct {
	mu    sync.Mutex
	reads int
}

func (p *flakyPort) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	if p.reads == 1 {
		// A read timeout: no bytes, no error. This is what the library returns
		// when the inter-character timeout expires on an idle device.
		return 0, nil
	}
	return 0, syscall.EIO
}

func (p *flakyPort) Write([]byte) (int, error)          { return 0, nil }
func (p *flakyPort) Drain() error                       { return nil }
func (p *flakyPort) ResetInputBuffer() error            { return nil }
func (p *flakyPort) ResetOutputBuffer() error           { return nil }
func (p *flakyPort) SetDTR(bool) error                  { return nil }
func (p *flakyPort) SetRTS(bool) error                  { return nil }
func (p *flakyPort) SetMode(*goserial.Mode) error       { return nil }
func (p *flakyPort) SetReadTimeout(time.Duration) error { return nil }
func (p *flakyPort) Close() error                       { return nil }
func (p *flakyPort) Break(time.Duration) error          { return nil }
func (p *flakyPort) GetModemStatusBits() (*goserial.ModemStatusBits, error) {
	return &goserial.ModemStatusBits{}, nil
}

// A device that opens and then immediately fails must back off, not reopen
// several times a second forever. Section 4.4 of the specification: do not spin.
func TestFlappingDeviceBacksOff(t *testing.T) {
	const (
		initial = 10 * time.Millisecond
		ceiling = 500 * time.Millisecond
		window  = 1 * time.Second
	)

	var mu sync.Mutex
	opens := 0
	opts := serialOpts("flapping", "/dev/fake", "\r")
	opts.BackoffInitial = initial
	opts.BackoffMax = ceiling
	opts.Open = func(string, *goserial.Mode) (goserial.Port, error) {
		mu.Lock()
		opens++
		mu.Unlock()
		return &flakyPort{}, nil
	}
	runDevice(t, opts, 1)

	time.Sleep(window)

	mu.Lock()
	got := opens
	mu.Unlock()

	// With backoff applied the schedule is 10, 20, 40, 80, 160, 320, 500...
	// which fits about seven opens into a second. Without it the device
	// reopens every 10ms, which is about a hundred.
	const limit = 20
	if got > limit {
		t.Errorf("device was reopened %d times in %s, want at most %d; the backoff is being reset on every failed session",
			got, window, limit)
	}
	if got < 2 {
		t.Errorf("device was reopened %d times, want the retry loop to be running", got)
	}
	t.Logf("%d reopens in %s", got, window)
}

// A session that stayed up must reset the backoff, so a device that has been
// working for a long time reconnects promptly rather than waiting the ceiling.
func TestStableSessionResetsBackoff(t *testing.T) {
	var mu sync.Mutex
	var gaps []time.Duration
	last := time.Now()

	opts := serialOpts("stable", "/dev/fake", "\r")
	opts.BackoffInitial = 10 * time.Millisecond
	opts.BackoffMax = 400 * time.Millisecond
	opts.StableAfter = 50 * time.Millisecond
	opts.Open = func(string, *goserial.Mode) (goserial.Port, error) {
		mu.Lock()
		gaps = append(gaps, time.Since(last))
		last = time.Now()
		mu.Unlock()
		return &blockingPort{hold: 120 * time.Millisecond}, nil
	}
	runDevice(t, opts, 1)

	time.Sleep(700 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(gaps) < 3 {
		t.Fatalf("got %d opens, want at least 3", len(gaps))
	}
	// Every session lasts longer than StableAfter, so the wait before each
	// reopen must stay near the initial backoff instead of doubling.
	for i, gap := range gaps[2:] {
		if gap > 200*time.Millisecond {
			t.Errorf("gap %d was %s; backoff grew even though every session was stable", i+2, gap)
		}
	}
}

// blockingPort stays readable for hold, then fails, so a session has a
// measurable duration.
type blockingPort struct {
	hold  time.Duration
	start time.Time
}

func (p *blockingPort) Read(b []byte) (int, error) {
	if p.start.IsZero() {
		p.start = time.Now()
	}
	if time.Since(p.start) < p.hold {
		time.Sleep(10 * time.Millisecond)
		return 0, nil
	}
	return 0, syscall.EIO
}

func (p *blockingPort) Write([]byte) (int, error)          { return 0, nil }
func (p *blockingPort) Drain() error                       { return nil }
func (p *blockingPort) ResetInputBuffer() error            { return nil }
func (p *blockingPort) ResetOutputBuffer() error           { return nil }
func (p *blockingPort) SetDTR(bool) error                  { return nil }
func (p *blockingPort) SetRTS(bool) error                  { return nil }
func (p *blockingPort) SetMode(*goserial.Mode) error       { return nil }
func (p *blockingPort) SetReadTimeout(time.Duration) error { return nil }
func (p *blockingPort) Close() error                       { return nil }
func (p *blockingPort) Break(time.Duration) error          { return nil }
func (p *blockingPort) GetModemStatusBits() (*goserial.ModemStatusBits, error) {
	return &goserial.ModemStatusBits{}, nil
}
