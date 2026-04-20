package timesync

import "testing"

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
