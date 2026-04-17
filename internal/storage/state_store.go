package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"shikimal-sync/internal/fsx"
	"shikimal-sync/internal/model"
)

var ErrStateNotFound = errors.New("state file does not exist")

type StateStore struct {
	path string
}

func NewStateStore(path string) *StateStore {
	return &StateStore{path: path}
}

func (s *StateStore) Load() (*model.Snapshot, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrStateNotFound
		}
		return nil, fmt.Errorf("read state file: %w", err)
	}

	var snapshot model.Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("parse state file: %w", err)
	}
	if snapshot.Entries == nil {
		snapshot.Entries = map[string]model.Entry{}
	}
	return &snapshot, nil
}

func (s *StateStore) Save(snapshot *model.Snapshot) error {
	return fsx.WriteJSONAtomic(s.path, snapshot)
}
