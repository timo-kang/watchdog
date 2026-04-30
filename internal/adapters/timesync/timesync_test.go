package timesync

import (
	"context"
	"testing"
	"time"

	"watchdog/internal/config"
)

func TestParseTimedatectlShow(t *testing.T) {
	raw := []byte(`Timezone=Asia/Seoul
LocalRTC=no
CanNTP=yes
NTP=yes
NTPSynchronized=yes
TimeUSec=Fri 2026-04-17 14:10:20 UTC
RTCTimeUSec=Fri 2026-04-17 14:10:00 UTC
`)

	state, err := parseTimedatectlShow(raw)
	if err != nil {
		t.Fatalf("parseTimedatectlShow: %v", err)
	}
	if state.timezone != "Asia/Seoul" {
		t.Fatalf("timezone = %q, want Asia/Seoul", state.timezone)
	}
	if state.localRTC {
		t.Fatal("localRTC = true, want false")
	}
	if !state.canNTP || !state.ntpEnabled || !state.ntpSynchronized {
		t.Fatalf("unexpected NTP state: %+v", state)
	}
	if got := state.systemTime.Sub(state.rtcTime).Seconds(); got != 20 {
		t.Fatalf("rtc delta = %.0f, want 20", got)
	}
}

func TestParseTimedatectlShowRejectsMalformedLine(t *testing.T) {
	_, err := parseTimedatectlShow([]byte("Timezone=UTC\nbad-line\n"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCollectTracksUnsynchronizedDurationAndReset(t *testing.T) {
	unsynchronized := []byte(`Timezone=UTC
LocalRTC=no
CanNTP=yes
NTP=yes
NTPSynchronized=no
TimeUSec=Fri 2026-04-17 14:10:20 UTC
RTCTimeUSec=Fri 2026-04-17 14:10:00 UTC
`)
	synchronized := []byte(`Timezone=UTC
LocalRTC=no
CanNTP=yes
NTP=yes
NTPSynchronized=yes
TimeUSec=Fri 2026-04-17 14:10:20 UTC
RTCTimeUSec=Fri 2026-04-17 14:10:00 UTC
`)

	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	collector := New(config.TimeSyncSourceConfig{
		SourceID:            "system-clock",
		RequireSynchronized: true,
		SyncGracePeriod:     10 * time.Minute,
	})
	collector.now = func() time.Time { return now }
	collector.show = func(context.Context) ([]byte, error) {
		return unsynchronized, nil
	}

	observations, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("first collect: %v", err)
	}
	if got := observations[0].Metrics["time.unsynchronized_for_s"]; got != 0 {
		t.Fatalf("first unsynchronized_for_s = %.0f, want 0", got)
	}

	now = now.Add(30 * time.Second)
	observations, err = collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("second collect: %v", err)
	}
	if got := observations[0].Metrics["time.unsynchronized_for_s"]; got != 30 {
		t.Fatalf("second unsynchronized_for_s = %.0f, want 30", got)
	}

	collector.show = func(context.Context) ([]byte, error) {
		return synchronized, nil
	}
	now = now.Add(10 * time.Second)
	observations, err = collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("synchronized collect: %v", err)
	}
	if _, ok := observations[0].Metrics["time.unsynchronized_for_s"]; ok {
		t.Fatal("did not expect unsynchronized_for_s after synchronization")
	}

	collector.show = func(context.Context) ([]byte, error) {
		return unsynchronized, nil
	}
	now = now.Add(5 * time.Second)
	observations, err = collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("reset collect: %v", err)
	}
	if got := observations[0].Metrics["time.unsynchronized_for_s"]; got != 0 {
		t.Fatalf("reset unsynchronized_for_s = %.0f, want 0", got)
	}
}
