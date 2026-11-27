// Package secret stores the local API token and service credentials.
package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"

	"github.com/hxx/hxxDiscourseDevKit/internal/logging"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
)

// File is the persisted secret document.
type File struct {
	SchemaVersion int               `json:"schema_version"`
	APIToken      string            `json:"api_token"`
	Entries       map[string]string `json:"entries"`
}

// Store provides thread-safe access to secrets.json.
type Store struct {
	mu   sync.Mutex
	path string
	data File
}

// Open loads or creates secrets.json. The file is written with mode 0600 and
// all loaded values are registered with the log redactor.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		token, err := RandomHex(32)
		if err != nil {
			return nil, err
		}
		s.data = File{SchemaVersion: 1, APIToken: token, Entries: map[string]string{}}
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, fmt.Errorf("read secrets: %w", err)
	default:
		if err := json.Unmarshal(raw, &s.data); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		if s.data.SchemaVersion != 1 {
			return nil, fmt.Errorf("unsupported secrets schema_version %d", s.data.SchemaVersion)
		}
		if s.data.APIToken == "" {
			token, err := RandomHex(32)
			if err != nil {
				return nil, err
			}
			s.data.APIToken = token
			if err := s.saveLocked(); err != nil {
				return nil, err
			}
		}
		if s.data.Entries == nil {
			s.data.Entries = map[string]string{}
		}
	}
	if err := protectFile(path); err != nil {
		return nil, fmt.Errorf("protect %s: %w", path, err)
	}
	logging.RegisterSecret(s.data.APIToken)
	for _, value := range s.data.Entries {
		logging.RegisterSecret(value)
	}
	return s, nil
}

// APIToken returns the local API bearer token.
func (s *Store) APIToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.APIToken
}

// Get returns a named secret.
func (s *Store) Get(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data.Entries[name]
	return v, ok
}

// Set stores a named secret and persists the file.
func (s *Store) Set(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Entries == nil {
		s.data.Entries = map[string]string{}
	}
	s.data.Entries[name] = value
	logging.RegisterSecret(value)
	return s.saveLocked()
}

// Delete removes named secrets and persists the file.
func (s *Store) Delete(names ...string) error {
	if len(names) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range names {
		delete(s.data.Entries, name)
	}
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if s.data.Entries == nil {
		s.data.Entries = map[string]string{}
	}
	if err := store.WriteJSONProtected(s.path, s.data, 0o600, protectFile); err != nil {
		return fmt.Errorf("persist secrets: %w", err)
	}
	return nil
}

// ProtectFile restricts a file containing credentials to the current user.
// It is used for generated service configuration files such as redis.conf.
func ProtectFile(path string) error {
	return protectFile(path)
}

// ReadToken reads only the API token without creating or modifying the file.
func ReadToken(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return "", fmt.Errorf("decode %s: %w", path, err)
	}
	if f.APIToken == "" {
		return "", fmt.Errorf("api_token missing from %s", path)
	}
	return f.APIToken, nil
}
