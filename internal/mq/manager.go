package mq

import (
	"context"
	"fmt"
	"strings"
)

// Kind enumerates the supported Bus backends.
const (
	KindInproc = "inproc"
	KindPG     = "pg"
	KindMongo  = "mongo"
)

// Config selects and configures one Bus backend. Only the field matching
// Kind is consulted.
type Config struct {
	Kind   string
	Inproc InprocConfig
	PG     PGConfig
	Mongo  MongoConfig
}

// Manager bundles a constructed Bus with knowledge of which backend it is.
// The wrapper exists so callers can inject a single dependency rather than
// branching on Kind at every call site.
type Manager struct {
	Bus  Bus
	Kind string
}

// NewManager constructs a Bus per cfg.Kind. An empty Kind defaults to inproc.
func NewManager(ctx context.Context, cfg Config) (*Manager, error) {
	kind := strings.ToLower(strings.TrimSpace(cfg.Kind))
	if kind == "" {
		kind = KindInproc
	}
	switch kind {
	case KindInproc:
		return &Manager{Bus: NewInproc(cfg.Inproc), Kind: kind}, nil
	case KindPG:
		bus, err := NewPG(ctx, cfg.PG)
		if err != nil {
			return nil, fmt.Errorf("mq manager: pg: %w", err)
		}
		return &Manager{Bus: bus, Kind: kind}, nil
	case KindMongo:
		bus, err := NewMongo(ctx, cfg.Mongo)
		if err != nil {
			return nil, fmt.Errorf("mq manager: mongo: %w", err)
		}
		return &Manager{Bus: bus, Kind: kind}, nil
	default:
		return nil, fmt.Errorf("mq manager: unknown kind %q", cfg.Kind)
	}
}

// Close releases the underlying Bus.
func (m *Manager) Close() error {
	if m == nil || m.Bus == nil {
		return nil
	}
	return m.Bus.Close()
}
