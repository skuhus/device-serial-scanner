package mqtt

import "testing"

func TestTopicsMatchSection53(t *testing.T) {
	topics := NewTopics("acme", "vasby", "pack-03")
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"base", topics.Base(), "skuhus/acme/vasby/pack-03"},
		{"scan", topics.Scan(), "skuhus/acme/vasby/pack-03/scan"},
		{"status", topics.Status(), "skuhus/acme/vasby/pack-03/status"},
		{"heartbeat", topics.Heartbeat(), "skuhus/acme/vasby/pack-03/heartbeat"},
		{"cmd", topics.Command(), "skuhus/acme/vasby/pack-03/cmd"},
		{"cmd result", topics.CommandResult(), "skuhus/acme/vasby/pack-03/cmd/result"},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
