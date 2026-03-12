package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type ScopeState struct {
	Project string `json:"project"`
	Agent   string `json:"agent"`
	Quiet   *bool  `json:"quiet,omitempty"`
}

type State struct {
	Channels map[string]ScopeState `json:"channels"`
	Threads  map[string]ScopeState `json:"threads"`
}

type JSONStore struct {
	path  string
	mu    sync.RWMutex
	state State
}

func NewJSONStore(path string) (*JSONStore, error) {
	store := &JSONStore{
		path: path,
		state: State{
			Channels: map[string]ScopeState{},
			Threads:  map[string]ScopeState{},
		},
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *JSONStore) load() error {
	content, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s.saveLocked()
		}
		return fmt.Errorf("read state: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(content) == 0 {
		return nil
	}
	if err := json.Unmarshal(content, &s.state); err != nil {
		return fmt.Errorf("parse state: %w", err)
	}
	if s.state.Channels == nil {
		s.state.Channels = map[string]ScopeState{}
	}
	if s.state.Threads == nil {
		s.state.Threads = map[string]ScopeState{}
	}
	return nil
}

func (s *JSONStore) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channels := make(map[string]ScopeState, len(s.state.Channels))
	for key, value := range s.state.Channels {
		channels[key] = value
	}
	threads := make(map[string]ScopeState, len(s.state.Threads))
	for key, value := range s.state.Threads {
		threads[key] = value
	}
	return State{
		Channels: channels,
		Threads:  threads,
	}
}

func (s *JSONStore) GetChannel(channelID string) ScopeState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Channels[channelID]
}

func (s *JSONStore) SetChannel(channelID string, state ScopeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Channels[channelID] = state
	return s.saveLocked()
}

func (s *JSONStore) ClearChannel(channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Channels, channelID)
	return s.saveLocked()
}

func (s *JSONStore) GetThread(threadKey string) ScopeState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Threads[threadKey]
}

func (s *JSONStore) SetThread(threadKey string, state ScopeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Threads[threadKey] = state
	return s.saveLocked()
}

func (s *JSONStore) ClearThread(threadKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Threads, threadKey)
	return s.saveLocked()
}

func (s *JSONStore) saveLocked() error {
	content, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(s.path, content, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}
