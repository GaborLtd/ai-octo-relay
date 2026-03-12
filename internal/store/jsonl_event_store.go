package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type JSONLEventStore struct {
	path string
	mu   sync.Mutex
}

var _ EventStore = (*JSONLEventStore)(nil)

func NewJSONLEventStore(path string) (*JSONLEventStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create event store dir: %w", err)
	}
	return &JSONLEventStore{path: path}, nil
}

func (s *JSONLEventStore) Append(event Event) error {
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append event log: %w", err)
	}
	return nil
}
