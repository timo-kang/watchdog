package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
)

type StatusView struct {
	State  State        `json:"state"`
	Latest *AuditRecord `json:"latest,omitempty"`
}

func LoadStatus(statePath, latestPath string) (StatusView, error) {
	state, err := LoadState(statePath)
	if err != nil {
		return StatusView{}, err
	}

	var latest *AuditRecord
	if latestPath != "" {
		record, err := LoadLatestRecord(latestPath)
		if err != nil {
			return StatusView{}, err
		}
		latest = record
	}

	return StatusView{
		State:  state,
		Latest: latest,
	}, nil
}

func LoadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{SchemaVersion: 1}, nil
		}
		return State{}, fmt.Errorf("read supervisor state: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode supervisor state: %w", err)
	}
	if state.SchemaVersion == 0 {
		state.SchemaVersion = 1
	}
	return state, nil
}

func LoadLatestRecord(path string) (*AuditRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read latest supervisor record: %w", err)
	}

	var record AuditRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("decode latest supervisor record: %w", err)
	}
	return &record, nil
}
