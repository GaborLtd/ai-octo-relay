package store

type NoopEventStore struct{}

var _ EventStore = (*NoopEventStore)(nil)

func (s *NoopEventStore) Append(Event) error {
	return nil
}
