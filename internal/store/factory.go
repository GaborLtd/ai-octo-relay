package store

import (
	"fmt"

	"github.com/match/ai-octo-relay/internal/config"
)

func OpenStateStore(cfg config.StorageConfig) (StateStore, error) {
	switch cfg.Type {
	case "json":
		return NewJSONStore(cfg.Path)
	case "sqlite":
		return NewSQLiteStateStore(cfg.Path)
	default:
		return nil, fmt.Errorf("unsupported state store type: %s", cfg.Type)
	}
}

func OpenEventStore(cfg config.StorageConfig) (EventStore, error) {
	switch cfg.Type {
	case "", "none":
		return &NoopEventStore{}, nil
	case "jsonl":
		return NewJSONLEventStore(cfg.Path)
	case "sqlite":
		return NewSQLiteEventStore(cfg.Path)
	default:
		return nil, fmt.Errorf("unsupported event store type: %s", cfg.Type)
	}
}
