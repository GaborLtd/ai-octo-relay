package store

type ScopeState struct {
	Project       string `json:"project"`
	Agent         string `json:"agent"`
	Quiet         *bool  `json:"quiet,omitempty"`
	SessionActive bool   `json:"session_active,omitempty"`
}

type NativeSessionState struct {
	Agent     string `json:"agent"`
	NativeID  string `json:"native_id"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Project   string `json:"project,omitempty"`
	ThreadKey string `json:"thread_key,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
}

type State struct {
	Channels map[string]ScopeState         `json:"channels"`
	Threads  map[string]ScopeState         `json:"threads"`
	Sessions map[string]NativeSessionState `json:"sessions"`
}

type StateStore interface {
	GetChannel(channelID string) ScopeState
	SetChannel(channelID string, state ScopeState) error
	ClearChannel(channelID string) error
	GetThread(threadKey string) ScopeState
	SetThread(threadKey string, state ScopeState) error
	ClearThread(threadKey string) error
	GetSession(sessionKey string) NativeSessionState
	SetSession(sessionKey string, state NativeSessionState) error
	ClearSession(sessionKey string) error
}

type Event struct {
	Type       string         `json:"type"`
	Timestamp  string         `json:"timestamp"`
	ChannelID  string         `json:"channel_id,omitempty"`
	ThreadKey  string         `json:"thread_key,omitempty"`
	SessionKey string         `json:"session_key,omitempty"`
	Project    string         `json:"project,omitempty"`
	Agent      string         `json:"agent,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type EventStore interface {
	Append(event Event) error
}
