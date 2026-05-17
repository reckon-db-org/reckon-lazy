// Package profiles persists a list of named cluster endpoints to
// $XDG_CONFIG_HOME/lazyreckon/profiles.toml so the user picks from
// a remembered set at startup instead of typing host:port every
// time. lazyreckon is an observation platform — the file stores
// endpoints in plain text; secrets (when the gateway gets auth) go
// in the OS keyring, not this file.
package profiles

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Profile is one saved cluster endpoint.
type Profile struct {
	Name     string    `toml:"name"`
	Endpoint string    `toml:"endpoint"`
	LastUsed time.Time `toml:"last_used,omitempty"`
}

// Store is the on-disk profile list. Load/Save are atomic at the
// file level via a temp + rename. Concurrent writers are not
// supported — lazyreckon is a single-user TUI; we'd add locking if
// that ever changes.
type Store struct {
	Profiles []Profile `toml:"profile"`

	path string
}

var (
	// ErrNotFound is returned when a profile name doesn't exist.
	ErrNotFound = errors.New("profile not found")
	// ErrDuplicate is returned when adding a profile whose name
	// already exists.
	ErrDuplicate = errors.New("profile name already exists")
)

// DefaultPath — $XDG_CONFIG_HOME/lazyreckon/profiles.toml, falling
// back to $HOME/.config/lazyreckon/profiles.toml.
func DefaultPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "lazyreckon", "profiles.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config dir: %w", err)
	}
	return filepath.Join(home, ".config", "lazyreckon", "profiles.toml"), nil
}

// Load reads the profile list from path. A missing file is not an
// error — the returned Store is empty but ready to Save against the
// path. Bad TOML is an error.
func Load(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// Path returns the path the store will Save to.
func (s *Store) Path() string { return s.path }

// Save writes the store atomically: marshal → write temp → rename.
// Creates the containing directory if missing.
func (s *Store) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".profiles-*.toml")
	if err != nil {
		return fmt.Errorf("temp: %w", err)
	}
	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(s); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("rename %s -> %s: %w", tmp.Name(), s.path, err)
	}
	return nil
}

// Find returns the profile by name, or ErrNotFound.
func (s *Store) Find(name string) (Profile, error) {
	for _, p := range s.Profiles {
		if p.Name == name {
			return p, nil
		}
	}
	return Profile{}, ErrNotFound
}

// Add appends a new profile. Returns ErrDuplicate if a profile with
// the same name already exists.
func (s *Store) Add(p Profile) error {
	if _, err := s.Find(p.Name); err == nil {
		return ErrDuplicate
	}
	s.Profiles = append(s.Profiles, p)
	return nil
}

// Delete removes the named profile. Returns ErrNotFound if missing.
func (s *Store) Delete(name string) error {
	for i, p := range s.Profiles {
		if p.Name == name {
			s.Profiles = append(s.Profiles[:i], s.Profiles[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// Rename changes the name of an existing profile.
func (s *Store) Rename(oldName, newName string) error {
	if oldName == newName {
		return nil
	}
	if _, err := s.Find(newName); err == nil {
		return ErrDuplicate
	}
	for i, p := range s.Profiles {
		if p.Name == oldName {
			s.Profiles[i].Name = newName
			return nil
		}
	}
	return ErrNotFound
}

// Touch updates LastUsed on the named profile to now. Used at
// connect-time so the next startup sorts the most recent profile
// to the top.
func (s *Store) Touch(name string) error {
	for i, p := range s.Profiles {
		if p.Name == name {
			s.Profiles[i].LastUsed = time.Now()
			return nil
		}
	}
	return ErrNotFound
}

// SortByRecency reorders Profiles so the most-recently-used comes
// first. Profiles with a zero LastUsed sort last (alphabetical
// tiebreak).
func (s *Store) SortByRecency() {
	sort.SliceStable(s.Profiles, func(i, j int) bool {
		li, lj := s.Profiles[i].LastUsed, s.Profiles[j].LastUsed
		switch {
		case !li.IsZero() && !lj.IsZero():
			return li.After(lj)
		case !li.IsZero():
			return true
		case !lj.IsZero():
			return false
		default:
			return strings.ToLower(s.Profiles[i].Name) <
				strings.ToLower(s.Profiles[j].Name)
		}
	})
}

// ValidateName returns nil if name is OK to use, an error otherwise.
// Rules: non-empty, no leading/trailing whitespace, no newlines,
// length <= 64.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("name is empty")
	}
	if strings.TrimSpace(name) != name {
		return errors.New("name has leading or trailing whitespace")
	}
	if strings.ContainsAny(name, "\n\r\t") {
		return errors.New("name contains a newline or tab")
	}
	if len(name) > 64 {
		return errors.New("name longer than 64 chars")
	}
	return nil
}

// ValidateEndpoint returns nil if ep parses as host:port-ish.
// Doesn't try to dial — just sanity. host required, port optional
// (defaults to 50051 in display).
func ValidateEndpoint(ep string) error {
	ep = strings.TrimSpace(ep)
	if ep == "" {
		return errors.New("endpoint is empty")
	}
	// Allow plain host (no port) — the caller can default the
	// port at dial time. But reject obviously broken shapes.
	if strings.ContainsAny(ep, " \t\n\r") {
		return errors.New("endpoint contains whitespace")
	}
	return nil
}
