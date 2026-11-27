package config

import (
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/store"
)

// Store provides thread-safe read/update access to devkit.json.
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

// Open loads the config, creating a default one on first run.
func Open(paths *Paths) (*Store, error) {
	s := &Store{path: paths.ConfigFile()}
	var cfg Config
	err := store.ReadJSON(s.path, &cfg)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		cfg = Default(paths)
		if err := store.WriteJSON(s.path, cfg, 0o600); err != nil {
			return nil, fmt.Errorf("write initial config: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("read config: %w", err)
	default:
		cfg.ApplyDefaults(paths)
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("invalid config %s: %w", s.path, err)
		}
	}
	s.cfg = cfg
	return s, nil
}

// Get returns a copy of the current configuration.
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Update applies fn to a copy, validates the result, and persists it.
func (s *Store) Update(fn func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.cfg
	if err := fn(&next); err != nil {
		return err
	}
	next.UpdatedAt = time.Now().UTC()
	if err := next.Validate(); err != nil {
		return err
	}
	if err := store.WriteJSON(s.path, next, 0o600); err != nil {
		return fmt.Errorf("persist config: %w", err)
	}
	s.cfg = next
	return nil
}
