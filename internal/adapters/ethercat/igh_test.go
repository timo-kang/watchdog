package ethercat

import "testing"

func TestParseSlavesOutput(t *testing.T) {
	raw := []byte(`0  0:0  PREOP  +  EK1100 EtherCAT Coupler
1  0:1  OP     +  EL2008 8 Ch. Dig. Output
2  0:2  SAFEOP E  EL3002 2 Ch. Ana. Input`)

	status, err := parseSlavesOutput(raw)
	if err != nil {
		t.Fatalf("parseSlavesOutput: %v", err)
	}

	if status.SlavesSeen != 3 {
		t.Fatalf("slaves_seen = %d, want 3", status.SlavesSeen)
	}
	if status.SlaveErrors != 1 {
		t.Fatalf("slave_errors = %d, want 1", status.SlaveErrors)
	}
	if status.MasterState != "preop" {
		t.Fatalf("master_state = %q, want preop", status.MasterState)
	}
}

func TestParseMasterOutput(t *testing.T) {
	raw := []byte(`Master0
Phase: OP
Link: UP
Slaves responding: 12
Working counter: 118
Expected working counter: 120`)

	status := parseMasterOutput(raw)
	if !status.LinkKnown || !status.LinkUp {
		t.Fatal("expected known up link")
	}
	if status.MasterState != "op" {
		t.Fatalf("master_state = %q, want op", status.MasterState)
	}
	if status.SlavesSeen != 12 {
		t.Fatalf("slaves_seen = %d, want 12", status.SlavesSeen)
	}
	if status.WorkingCounter != 118 {
		t.Fatalf("working_counter = %d, want 118", status.WorkingCounter)
	}
	if status.WorkingCounterExpected != 120 {
		t.Fatalf("working_counter_expected = %d, want 120", status.WorkingCounterExpected)
	}
}
