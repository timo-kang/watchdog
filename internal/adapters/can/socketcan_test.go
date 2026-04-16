package can

import "testing"

func TestParseSocketCANOutput(t *testing.T) {
	raw := []byte(`5: can0: <NOARP,UP,LOWER_UP,ECHO> mtu 16 qdisc pfifo_fast state UP mode DEFAULT group default qlen 10
    link/can promiscuity 0  allmulti 0 minmtu 0 maxmtu 0
    can state ERROR-ACTIVE (berr-counter tx 0 rx 0) restart-ms 0
          bitrate 1000000 sample-point 0.875
          tq 125 prop-seg 6 phase-seg1 7 phase-seg2 2 sjw 1 brp 1
          re-started bus-errors arbit-lost error-warn error-pass bus-off
          2          10         0          1          1          3
    RX:  bytes packets errors dropped  missed   mcast
         1024  32      4      0        0        0
    TX:  bytes packets errors dropped carrier collsns
         2048  64      5      0       0       0`)

	status, err := parseSocketCANOutput(raw)
	if err != nil {
		t.Fatalf("parseSocketCANOutput: %v", err)
	}

	if !status.LinkUp {
		t.Fatal("expected link up")
	}
	if status.Bitrate != 1000000 {
		t.Fatalf("bitrate = %d, want 1000000", status.Bitrate)
	}
	if status.State != "error-active" {
		t.Fatalf("state = %q, want error-active", status.State)
	}
	if status.RestartCount != 2 {
		t.Fatalf("restart_count = %d, want 2", status.RestartCount)
	}
	if status.BusOffCount != 3 {
		t.Fatalf("bus_off_count = %d, want 3", status.BusOffCount)
	}
	if status.RXErrors != 4 {
		t.Fatalf("rx_errors = %d, want 4", status.RXErrors)
	}
	if status.TXErrors != 5 {
		t.Fatalf("tx_errors = %d, want 5", status.TXErrors)
	}
	if status.OnlineNodesKnown {
		t.Fatal("expected online nodes to remain unknown for generic socketcan")
	}
}
